package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const adminUsername = "ssh-admin"

func (s *Store) adminHash(ctx context.Context) ([]byte, error) {
	s.keyMu.RLock()
	defer s.keyMu.RUnlock()
	// The encrypted envelope is authoritative so a password change needs only
	// one atomic file replacement, without a separate database commit.
	if hash := unifiedAdminHash(s.keyFile); len(hash) > 0 {
		return bytes.Clone(hash), nil
	}
	var hash []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE name='admin_password'`).Scan(&hash)
	return hash, err
}

// SetAdminPassword 重置唯一管理员的密码，已有网页登录随后失效。
func (s *Store) SetAdminPassword(ctx context.Context, password string) error {
	if len(password) < 12 || len(password) > 72 {
		return fmt.Errorf("管理员密码长度必须在 12～72 字节之间")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if s.aead == nil {
		return ErrMasterLocked
	}
	if len(s.keyFile) != 32 {
		data, err := wrapUnifiedMasterKey(s.dataKey, password, hash)
		if err != nil {
			return err
		}
		defer clear(data)
		if err = replaceMasterKey(s.dir, s.keyFile, data); err != nil {
			return err
		}
		clear(s.keyFile)
		s.keyFile = bytes.Clone(data)
		return nil
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings VALUES ('admin_password', ?) ON CONFLICT(name) DO UPDATE SET value=excluded.value`, hash)
	return err
}

// EnsureAdmin 仅在首次启动时生成管理员密码；返回值只应展示给本机管理员。
func (s *Store) EnsureAdmin(ctx context.Context) (string, error) {
	if _, err := s.adminHash(ctx); err == nil {
		return "", nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	password := base64.RawURLEncoding.EncodeToString(secret)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO settings VALUES ('admin_password', ?) ON CONFLICT(name) DO NOTHING`, hash)
	if err != nil {
		return "", err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return "", nil
	}
	if err := s.SetCredentialProtection(ctx, password, true); err != nil {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM settings WHERE name='admin_password' AND value=?`, hash)
		return "", err
	}
	return password, nil
}

type Event struct {
	ID      int64  `json:"id"`
	Time    string `json:"time"`
	Target  string `json:"target"`
	Source  string `json:"source"`
	Message string `json:"message"`
}

func (s *Store) logEvent(target, source, message string) {
	slog.Info(message, "目标", target, "来源", source)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO events(time,target,source,message) VALUES (?,?,?,?)`, time.Now().UTC().Format(time.RFC3339), target, source, message)
	if err != nil {
		slog.Error("保存连接日志失败", "错误", err)
		return
	}
	// 本机版只保留最近 1000 条基础事件，避免持续增长。
	s.db.ExecContext(ctx, `DELETE FROM events WHERE id <= (SELECT MAX(id)-1000 FROM events)`)
}

func (s *Store) Events(ctx context.Context) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,time,target,source,message FROM events ORDER BY id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Event, 0)
	for rows.Next() {
		var item Event
		if err := rows.Scan(&item.ID, &item.Time, &item.Target, &item.Source, &item.Message); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
