package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"
)

type fileFavorite struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}
type favoriteRequest struct {
	Op      string         `json:"op"`
	Entries []fileFavorite `json:"entries,omitempty"`
}

func (web *Web) favorites(w http.ResponseWriter, r *http.Request) {
	var in favoriteRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || dec.Decode(new(any)) != io.EOF {
		apiError(w, 400, "文件请求无效")
		return
	}
	if in.Op != "read" && in.Op != "add" && in.Op != "remove" && in.Op != "import" || len(in.Entries) > 1000 {
		apiError(w, 400, "文件请求无效")
		return
	}
	for i := range in.Entries {
		entry := &in.Entries[i]
		if !path.IsAbs(entry.Path) || len(entry.Path) > 4096 || strings.ContainsRune(entry.Path, 0) || !utf8.ValidString(entry.Path) || (entry.Kind != "directory" && entry.Kind != "file" && entry.Kind != "link") {
			apiError(w, 400, "文件路径无效")
			return
		}
		entry.Path = path.Clean(entry.Path)
		entry.Name = path.Base(entry.Path)
	}
	id := r.PathValue("id")
	if !web.canAccess(r, id) {
		apiError(w, 403, "登录已失效或机器授权已撤销")
		return
	}
	entries, err := web.store.fileFavorites(r.Context(), web.sessionID(r), id, in)
	if err != nil {
		machineFailure(w, err)
		return
	}
	jsonResponse(w, 200, entries)
}

// Individual mutations avoid overwriting changes made in another browser.
func (s *Store) fileFavorites(ctx context.Context, user, target string, in favoriteRequest) ([]fileFavorite, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var allowed bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM targets t WHERE t.id=? AND
 (?='' OR EXISTS(SELECT 1 FROM users u WHERE u.id=? AND u.enabled=1 AND
 (EXISTS(SELECT 1 FROM user_targets g WHERE g.user_id=u.id AND g.target_id=t.id) OR
 EXISTS(SELECT 1 FROM user_tags g, json_each(t.config,'$.tags') tag WHERE g.user_id=u.id AND g.tag=tag.value)))))`, target, user, user).Scan(&allowed)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, fail(403, "没有此机器的访问权限")
	}
	for _, entry := range in.Entries {
		switch in.Op {
		case "add", "import":
			_, err = tx.ExecContext(ctx, `INSERT INTO file_favorites(user_id,target_id,path,kind) VALUES(?,?,?,?) ON CONFLICT(user_id,target_id,path) DO NOTHING`, user, target, entry.Path, entry.Kind)
		case "remove":
			_, err = tx.ExecContext(ctx, `DELETE FROM file_favorites WHERE user_id=? AND target_id=? AND path=?`, user, target, entry.Path)
		}
		if err != nil {
			return nil, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT path,kind FROM file_favorites WHERE user_id=? AND target_id=? ORDER BY rowid`, user, target)
	if err != nil {
		return nil, err
	}
	entries := []fileFavorite{}
	for rows.Next() {
		var entry fileFavorite
		if err = rows.Scan(&entry.Path, &entry.Kind); err != nil {
			rows.Close()
			return nil, err
		}
		entry.Name = path.Base(entry.Path)
		entries = append(entries, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return entries, nil
}
