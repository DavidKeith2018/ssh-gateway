package gateway

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type Account struct {
	ID        string   `json:"id"`
	Username  string   `json:"username"`
	Enabled   bool     `json:"enabled"`
	TargetIDs []string `json:"target_ids"`
	Tags      []string `json:"tags"`
}
type accountInput struct {
	Username  string   `json:"username"`
	Password  string   `json:"password"`
	Enabled   bool     `json:"enabled"`
	TargetIDs []string `json:"target_ids"`
	Tags      []string `json:"tags"`
}

func cleanTags(tags []string) ([]string, error) {
	result := []string{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if !utf8.ValidString(tag) || len(tag) > 100 || strings.ContainsAny(tag, "\r\n\x00") {
			return nil, fail(400, "标签必须是 100 字节以内的单行文本")
		}
		if !slices.Contains(result, tag) {
			result = append(result, tag)
		}
	}
	if len(result) > 64 {
		return nil, fail(400, "最多允许 64 个标签")
	}
	slices.Sort(result)
	return result, nil
}

func (s *Store) accountCredential(ctx context.Context, id string) ([]byte, int64, error) {
	if id == "" {
		h, e := s.adminHash(ctx)
		return h, 0, e
	}
	var hash []byte
	var epoch int64
	err := s.db.QueryRowContext(ctx, `SELECT password_hash, epoch FROM users WHERE id=? AND enabled=1`, id).Scan(&hash, &epoch)
	return hash, epoch, err
}

func (s *Store) accountAccess(ctx context.Context, id, target string) bool {
	var allowed bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM targets t WHERE t.id=? AND
 (?='' OR EXISTS(SELECT 1 FROM users u WHERE u.id=? AND u.enabled=1 AND
 (EXISTS(SELECT 1 FROM user_targets g WHERE g.user_id=u.id AND g.target_id=t.id) OR
 EXISTS(SELECT 1 FROM user_tags g, json_each(t.config,'$.tags') tag WHERE g.user_id=u.id AND g.tag=tag.value)))))`, target, id, id).Scan(&allowed)
	return err == nil && allowed
}

func (web *Web) sessionID(r *http.Request) string {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return "invalid"
	}
	web.mu.Lock()
	defer web.mu.Unlock()
	session, ok := web.sessions[cookie.Value]
	if !ok {
		return "invalid"
	}
	return session.userID
}
func (web *Web) isAdmin(r *http.Request) bool { return web.validSession(r) && web.sessionID(r) == "" }
func (web *Web) canAccess(r *http.Request, target string) bool {
	return web.validSession(r) && web.store.accountAccess(r.Context(), web.sessionID(r), target)
}
func (web *Web) administrator(next http.HandlerFunc) http.HandlerFunc {
	return web.protected(func(w http.ResponseWriter, r *http.Request) {
		if !web.isAdmin(r) {
			apiError(w, 403, "仅管理员可操作")
			return
		}
		next(w, r)
	})
}
func (web *Web) machineProtected(next http.HandlerFunc) http.HandlerFunc {
	return web.protected(func(w http.ResponseWriter, r *http.Request) {
		if !web.canAccess(r, r.PathValue("id")) {
			apiError(w, 403, "没有此机器的访问权限")
			return
		}
		next(w, r)
	})
}
func (web *Web) userRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/users", web.administrator(web.accountPage))
	mux.HandleFunc("POST /api/users", web.administrator(web.saveUser))
	mux.HandleFunc("PUT /api/users/{id}", web.administrator(web.saveUser))
	mux.HandleFunc("DELETE /api/users/{id}", web.administrator(web.deleteUser))
}
func (s *Store) saveAccount(ctx context.Context, id string, in accountInput) (string, error) {
	creating := id == ""
	if !identifier.MatchString(in.Username) || strings.EqualFold(in.Username, "admin") || strings.EqualFold(in.Username, adminUsername) {
		return "", fail(400, "用户名必须为 1～64 位字母、数字、点、下划线或连字符，且不能使用 admin 或 "+adminUsername)
	}
	tags, err := cleanTags(in.Tags)
	if err != nil {
		return "", err
	}
	var hash []byte
	if creating || in.Password != "" {
		if len(in.Password) < 12 || len(in.Password) > 72 {
			return "", fail(400, "密码长度必须在 12～72 字节之间")
		}
		hash, err = bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			return "", err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if creating {
		id = uuid.NewString()
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username=?)`, in.Username).Scan(&exists); err != nil {
			return "", err
		}
		if exists {
			return "", fail(409, "用户名已存在")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,enabled,epoch) VALUES(?,?,?,?,1)`, id, in.Username, hash, in.Enabled)
	} else {
		var username string
		var enabled bool
		var oldHash []byte
		err = tx.QueryRowContext(ctx, `SELECT username,enabled,password_hash FROM users WHERE id=?`, id).Scan(&username, &enabled, &oldHash)
		if errors.Is(err, sql.ErrNoRows) {
			return "", fail(404, "用户不存在")
		}
		if err != nil {
			return "", err
		}
		if username != in.Username {
			return "", fail(400, "用户名不可修改")
		}
		changed := 0
		if hash != nil || enabled != in.Enabled {
			changed = 1
		}
		if hash == nil {
			hash = oldHash
		}
		_, err = tx.ExecContext(ctx, `UPDATE users SET password_hash=?,enabled=?,epoch=epoch+? WHERE id=?`, hash, in.Enabled, changed, id)
	}
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_targets WHERE user_id=?`, id); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_tags WHERE user_id=?`, id); err != nil {
		return "", err
	}
	for _, target := range in.TargetIDs {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM targets WHERE id=?)`, target).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return "", fail(400, "所选机器不存在")
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_targets VALUES(?,?)`, id, target); err != nil {
			return "", err
		}
	}
	for _, tag := range tags {
		if _, err = tx.ExecContext(ctx, `INSERT INTO user_tags VALUES(?,?)`, id, tag); err != nil {
			return "", err
		}
	}
	return id, tx.Commit()
}
func (web *Web) saveUser(w http.ResponseWriter, r *http.Request) {
	var in accountInput
	if err := decodeJSON(w, r, &in); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	id, err := web.store.saveAccount(r.Context(), r.PathValue("id"), in)
	if err != nil {
		machineFailure(w, err)
		return
	}
	web.store.logEvent("", sourceIP(r), "管理员保存帐号："+in.Username)
	jsonResponse(w, 200, map[string]string{"id": id})
}
func (web *Web) deleteUser(w http.ResponseWriter, r *http.Request) {
	result, err := web.store.db.ExecContext(r.Context(), `DELETE FROM users WHERE id=?`, r.PathValue("id"))
	if err != nil {
		machineFailure(w, err)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		apiError(w, 404, "用户不存在")
		return
	}
	web.store.logEvent("", sourceIP(r), "管理员删除帐号："+r.PathValue("id"))
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
