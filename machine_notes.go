package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"
)

// 笔记保存于 SSH 记录的 notes 字段，不依赖远程 SSH。
func (web *Web) notes(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !identifier.MatchString(id) {
		apiError(w, 400, "机器 ID 无效")
		return
	}
	if _, err := web.store.get(r.Context(), "id", id); err != nil {
		apiError(w, 404, "机器不存在")
		return
	}
	var in struct {
		Op        string `json:"op"`
		Content   string `json:"content"`
		Version   string `json:"version"`
		Overwrite bool   `json:"overwrite"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || dec.Decode(new(any)) != io.EOF {
		apiError(w, 400, "笔记请求无效")
		return
	}
	if in.Op != "read" && in.Op != "write" {
		apiError(w, 400, "未知笔记操作")
		return
	}
	if len(in.Content) > editLimit || !utf8.ValidString(in.Content) {
		apiError(w, 413, "笔记必须是 2 MiB 以内的 UTF-8 文本")
		return
	}
	// 请求体可能长时间未发送完；操作前重新检查登录与机器授权。
	if !web.canAccess(r, id) {
		apiError(w, 403, "登录已失效或机器授权已撤销")
		return
	}
	result, err := web.store.recordNote(r.Context(), id, in.Op, in.Content, in.Version, in.Overwrite)
	if err != nil {
		machineFailure(w, err)
		return
	}
	jsonResponse(w, 200, result)
}

func (s *Store) recordNote(ctx context.Context, id, op, content, expected string, overwrite bool) (map[string]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT notes FROM targets WHERE id=?`, id).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fail(404, "机器不存在")
		}
		return nil, err
	}
	if op == "write" {
		if !overwrite && expected != version([]byte(current)) {
			return nil, fail(409, "笔记已修改，请刷新或确认覆盖")
		}
		if _, err = tx.ExecContext(ctx, `UPDATE targets SET notes=? WHERE id=?`, content, id); err != nil {
			return nil, err
		}
		current = content
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]string{"content": current, "version": version([]byte(current))}, nil
}
