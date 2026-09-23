package gateway

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/google/uuid"
)

type Shortcut struct {
	Tags     []string `json:"tags"`
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Command  string   `json:"command"`
	TargetID string   `json:"targetId"`
}

func (web *Web) shortcutRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/shortcuts", web.protected(web.shortcutPage))
	mux.HandleFunc("POST /api/shortcuts", web.administrator(web.saveShortcut))
	mux.HandleFunc("PUT /api/shortcuts/{id}", web.administrator(web.saveShortcut))
	mux.HandleFunc("DELETE /api/shortcuts/{id}", web.administrator(web.deleteShortcut))
}
func (web *Web) shortcutPage(w http.ResponseWriter, r *http.Request) {
	p, ok := readPage(w, r)
	if !ok {
		return
	}
	uid := web.sessionID(r)
	// 以 EXISTS 匹配，避免多个标签或机器重复命令并影响分页。
	tagMatch := `EXISTS(SELECT 1 FROM shortcut_tags st JOIN json_each(t.config,'$.tags') tag ON tag.value=st.tag WHERE st.shortcut_id=s.id)`
	from := `FROM shortcuts s LEFT JOIN targets t ON t.id=s.target_id WHERE (?='' OR EXISTS(SELECT 1 FROM targets t WHERE ` + targetAccessSQL + ` AND (s.target_id='*' OR s.target_id=t.id OR (s.target_id='' AND ` + tagMatch + `))))`
	args := []any{uid, uid, uid}
	if target := r.URL.Query().Get("applicable_to"); target != "" {
		if !web.canAccess(r, target) {
			apiError(w, 403, "没有此机器的访问权限")
			return
		}
		from += ` AND EXISTS(SELECT 1 FROM targets t WHERE t.id=? AND (s.target_id='*' OR s.target_id=t.id OR (s.target_id='' AND ` + tagMatch + `)))`
		args = append(args, target)
	}
	if r.URL.Query().Get("scope") == "tags" {
		from += ` AND s.target_id=''`
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		from += ` AND EXISTS(SELECT 1 FROM shortcut_tags st WHERE st.shortcut_id=s.id AND st.tag=?)`
		args = append(args, tag)
	}
	if target := r.URL.Query().Get("target_id"); target != "" {
		from += ` AND s.target_id=?`
		args = append(args, target)
	}
	if q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); q != "" {
		from += ` AND instr(gateway_lower(s.name||' '||s.command||' '||COALESCE((SELECT group_concat(tag,' ') FROM shortcut_tags WHERE shortcut_id=s.id),'')||' '||CASE WHEN s.target_id='*' THEN '全部机器' ELSE COALESCE(json_extract(t.config,'$.name'),'')||' '||s.target_id END),?)>0`
		args = append(args, q)
	}
	sqlPage(w, r, web.store, p, from, `s.id,s.name,s.command,s.target_id,(SELECT json_group_array(tag) FROM (SELECT tag FROM shortcut_tags WHERE shortcut_id=s.id ORDER BY tag))`, "s.seq,s.id", args, func(rows *sql.Rows) (Shortcut, error) {
		var item Shortcut
		var tags string
		err := rows.Scan(&item.ID, &item.Name, &item.Command, &item.TargetID, &tags)
		if err == nil {
			err = json.Unmarshal([]byte(tags), &item.Tags)
		}
		return item, err
	})
}
func (web *Web) saveShortcut(w http.ResponseWriter, r *http.Request) {
	var item Shortcut
	if err := decodeJSON(w, r, &item); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	item.Name = strings.TrimSpace(item.Name)
	item.Command = strings.TrimSpace(item.Command)
	valid := func(s string, max int) bool {
		return utf8.ValidString(s) && s != "" && len(utf16.Encode([]rune(s))) <= max
	}
	if !valid(item.Name, 24) || !valid(item.Command, 2000) || strings.ContainsFunc(item.Command, func(c rune) bool { return c < 32 || c == 127 }) {
		apiError(w, 400, "名称需为 1～24 字，命令需为 1～2000 字的单行文本")
		return
	}
	tags, err := cleanTags(item.Tags)
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	item.Tags = tags
	if (item.TargetID == "") != (len(item.Tags) > 0) {
		apiError(w, 400, "请选择机器范围或至少一个标签，不能混合指定")
		return
	}
	tx, err := web.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		machineFailure(w, err)
		return
	}
	defer tx.Rollback()
	if item.TargetID != "*" && item.TargetID != "" {
		var exists bool
		err = tx.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM targets WHERE id=?)", item.TargetID).Scan(&exists)
		if err != nil {
			machineFailure(w, err)
			return
		}
		if !exists {
			apiError(w, 400, "机器不存在")
			return
		}
	}
	if r.Method == "POST" {
		item.ID = uuid.NewString()
		_, err = tx.ExecContext(r.Context(), "INSERT INTO shortcuts(id,name,command,target_id) VALUES(?,?,?,?)", item.ID, item.Name, item.Command, item.TargetID)
	} else {
		item.ID = r.PathValue("id")
		var result sql.Result
		result, err = tx.ExecContext(r.Context(), "UPDATE shortcuts SET name=?,command=?,target_id=? WHERE id=?", item.Name, item.Command, item.TargetID, item.ID)
		if err == nil {
			n, e := result.RowsAffected()
			err = e
			if err == nil && n == 0 {
				apiError(w, 404, "快捷命令不存在")
				return
			}
		}
	}
	if err != nil {
		machineFailure(w, err)
		return
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM shortcut_tags WHERE shortcut_id=?", item.ID); err != nil {
		machineFailure(w, err)
		return
	}
	for _, tag := range item.Tags {
		if _, err = tx.ExecContext(r.Context(), "INSERT INTO shortcut_tags(shortcut_id,tag) VALUES(?,?)", item.ID, tag); err != nil {
			machineFailure(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		machineFailure(w, err)
		return
	}
	jsonResponse(w, 200, item)
}
func (web *Web) deleteShortcut(w http.ResponseWriter, r *http.Request) {
	result, err := web.store.db.ExecContext(r.Context(), "DELETE FROM shortcuts WHERE id=?", r.PathValue("id"))
	if err != nil {
		machineFailure(w, err)
		return
	}
	n, err := result.RowsAffected()
	if err != nil {
		machineFailure(w, err)
		return
	}
	if n == 0 {
		apiError(w, 404, "快捷命令不存在")
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
