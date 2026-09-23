package gateway

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type pageRequest struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}
type pageResult[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
	pageRequest
}

func readPage(w http.ResponseWriter, r *http.Request) (pageRequest, bool) {
	p := pageRequest{1, 20}
	for key, dst := range map[string]*int{"page": &p.Page, "page_size": &p.PageSize} {
		if values, ok := r.URL.Query()[key]; ok {
			n, err := strconv.Atoi(values[0])
			if len(values) != 1 || err != nil || n < 1 || (key == "page_size" && n > 100) {
				apiError(w, 400, "分页参数无效："+key)
				return p, false
			}
			*dst = n
		}
	}
	return p, true
}
func (p *pageRequest) clamp(total int) {
	last := 1
	if total > 0 {
		last = (total-1)/p.PageSize + 1
	}
	if p.Page > last {
		p.Page = last
	}
}

// 在同一读事务中计数和读取，避免删除或新增造成页码与总数不一致。
func sqlPage[T any](w http.ResponseWriter, r *http.Request, s *Store, p pageRequest, from, columns, order string, args []any, scan func(*sql.Rows) (T, error)) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		machineFailure(w, err)
		return
	}
	defer tx.Rollback()
	result := pageResult[T]{Items: []T{}, pageRequest: p}
	if err = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) "+from, args...).Scan(&result.Total); err != nil {
		machineFailure(w, err)
		return
	}
	result.clamp(result.Total)
	queryArgs := append(append([]any{}, args...), result.PageSize, (result.Page-1)*result.PageSize)
	rows, err := tx.QueryContext(r.Context(), "SELECT "+columns+" "+from+" ORDER BY "+order+" LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		machineFailure(w, err)
		return
	}
	for rows.Next() {
		item, e := scan(rows)
		if e != nil {
			rows.Close()
			machineFailure(w, e)
			return
		}
		result.Items = append(result.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		machineFailure(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		machineFailure(w, err)
		return
	}
	jsonResponse(w, 200, result)
}

// 与 accountAccess 相同的授权条件，用于分页和汇总；别名固定为 t。
const targetAccessSQL = `(?='' OR EXISTS(SELECT 1 FROM users u WHERE u.id=? AND u.enabled=1 AND
 (EXISTS(SELECT 1 FROM user_targets g WHERE g.user_id=u.id AND g.target_id=t.id) OR
 EXISTS(SELECT 1 FROM user_tags g,json_each(t.config,'$.tags') tag WHERE g.user_id=u.id AND g.tag=tag.value))))`

func scanTarget(rows *sql.Rows) (Target, error) {
	var t Target
	var raw string
	var revision int64
	err := rows.Scan(&raw, &revision)
	if err != nil {
		return t, err
	}
	err = json.Unmarshal([]byte(raw), &t)
	t.Revision = revision
	if t.Tags == nil {
		t.Tags = []string{}
	}
	t.AuthType = authType(t.AuthType)
	return t, err
}

const targetNameOrder = `gateway_lower(COALESCE(json_extract(t.config,'$.name'),'')),t.id`

func (web *Web) targetPage(w http.ResponseWriter, r *http.Request) {
	p, ok := readPage(w, r)
	if !ok {
		return
	}
	uid := web.sessionID(r)
	args := []any{uid, uid}
	from := "FROM targets t WHERE " + targetAccessSQL
	if q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); q != "" {
		from += ` AND instr(gateway_lower(COALESCE(json_extract(t.config,'$.name'),'')||' '||COALESCE(json_extract(t.config,'$.host'),'')||' '||t.relay_user||' '||COALESCE((SELECT group_concat(json_extract(value,'$.username'),' ') FROM json_each(t.config,'$.relays')),'')||' '||COALESCE((SELECT group_concat(json_extract(value,'$.user'),' ') FROM json_each(t.config,'$.logins')),'')||' '||COALESCE((SELECT group_concat(value,' ') FROM json_each(t.config,'$.tags')),'')),?)>0`
		args = append(args, q)
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		from += ` AND EXISTS(SELECT 1 FROM json_each(t.config,'$.tags') WHERE value=?)`
		args = append(args, tag)
	}
	sqlPage(w, r, web.store, p, from, "t.config,t.revision", targetNameOrder, args, scanTarget)
}

type targetOption struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Host string   `json:"host"`
	Tags []string `json:"tags"`
}

func (web *Web) targetOptions(w http.ResponseWriter, r *http.Request) {
	uid := web.sessionID(r)
	rows, err := web.store.db.QueryContext(r.Context(), `SELECT t.id,json_extract(t.config,'$.name'),json_extract(t.config,'$.host'),COALESCE(json_extract(t.config,'$.tags'),'[]') FROM targets t WHERE `+targetAccessSQL+` ORDER BY `+targetNameOrder, uid, uid)
	if err != nil {
		machineFailure(w, err)
		return
	}
	defer rows.Close()
	items := []targetOption{}
	for rows.Next() {
		var item targetOption
		var raw string
		if err = rows.Scan(&item.ID, &item.Name, &item.Host, &raw); err != nil {
			machineFailure(w, err)
			return
		}
		if err = json.Unmarshal([]byte(raw), &item.Tags); err != nil {
			machineFailure(w, err)
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		machineFailure(w, err)
		return
	}
	jsonResponse(w, 200, items)
}
func (web *Web) targetDetail(w http.ResponseWriter, r *http.Request) {
	t, err := web.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		machineFailure(w, err)
		return
	}
	jsonResponse(w, 200, t)
}
func (web *Web) targetSummary(w http.ResponseWriter, r *http.Request) {
	uid := web.sessionID(r)
	var total, enabled int
	err := web.store.db.QueryRowContext(r.Context(), `SELECT COUNT(*),COALESCE(SUM(CASE WHEN json_extract(t.config,'$.enabled') THEN CASE WHEN json_type(t.config,'$.relays')='array' THEN (SELECT COUNT(*) FROM json_each(t.config,'$.relays') WHERE json_extract(value,'$.enabled')) ELSE 1 END ELSE 0 END),0) FROM targets t WHERE `+targetAccessSQL, uid, uid).Scan(&total, &enabled)
	if err != nil {
		machineFailure(w, err)
		return
	}
	active, err := web.store.activeConnections(r.Context(), uid)
	if err != nil {
		machineFailure(w, err)
		return
	}
	jsonResponse(w, 200, map[string]int{"total": total, "enabled": enabled, "active": active})
}
func (web *Web) accountPage(w http.ResponseWriter, r *http.Request) {
	p, ok := readPage(w, r)
	if !ok {
		return
	}
	sqlPage(w, r, web.store, p, "FROM users u", `u.id,u.username,u.enabled,(SELECT json_group_array(target_id) FROM user_targets WHERE user_id=u.id),(SELECT json_group_array(tag) FROM user_tags WHERE user_id=u.id)`, "u.username,u.id", nil, func(rows *sql.Rows) (Account, error) {
		var item Account
		var targets, tags string
		err := rows.Scan(&item.ID, &item.Username, &item.Enabled, &targets, &tags)
		if err == nil {
			err = json.Unmarshal([]byte(targets), &item.TargetIDs)
		}
		if err == nil {
			err = json.Unmarshal([]byte(tags), &item.Tags)
		}
		return item, err
	})
}
func (web *Web) eventPage(w http.ResponseWriter, r *http.Request) {
	p, ok := readPage(w, r)
	if !ok {
		return
	}
	sqlPage(w, r, web.store, p, "FROM events", "id,time,target,source,message", "id DESC", nil, func(rows *sql.Rows) (Event, error) {
		var item Event
		err := rows.Scan(&item.ID, &item.Time, &item.Target, &item.Source, &item.Message)
		return item, err
	})
}
func (web *Web) globalIPPage(w http.ResponseWriter, r *http.Request) {
	p, ok := readPage(w, r)
	if !ok {
		return
	}
	sqlPage(w, r, web.store, p, "FROM global_ips", "ip,expires_at", "ip", nil, func(rows *sql.Rows) (GlobalIP, error) {
		var item GlobalIP
		var expiry int64
		err := rows.Scan(&item.IP, &expiry)
		item.ExpiresAt = time.Unix(0, expiry).UTC()
		return item, err
	})
}
