package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
)

const backupLimit = 64 << 20

var backupMagic = []byte("SGBACK01")

type BackupInfo struct {
	Version         int       `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
	Targets         int       `json:"targets"`
	MasterProtected bool      `json:"master_protected"`
}
type backupPayload struct {
	BackupInfo
	Files map[string][]byte `json:"files"`
}

// ExportBackup 的快照与密钥读取在同一个主密钥读锁内完成；不归档任意目录文件。
func (s *Store) ExportBackup(ctx context.Context, password string) ([]byte, error) {
	if err := validateMasterPassword(password); err != nil {
		return nil, err
	}
	s.keyMu.RLock()
	defer s.keyMu.RUnlock()
	if s.aead == nil {
		return nil, ErrMasterLocked
	}
	key, err := os.ReadFile(filepath.Join(s.dir, "master.key"))
	if err != nil || !bytes.Equal(key, s.keyFile) {
		return nil, fmt.Errorf("主密钥已变化，请重启后备份")
	}
	temp, err := os.MkdirTemp(s.dir, ".backup-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temp)
	snapshot := filepath.Join(temp, "gateway.db")
	if _, err = s.db.ExecContext(ctx, "VACUUM INTO ?", snapshot); err != nil {
		return nil, err
	}
	if err = protectFile(snapshot); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", snapshot)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var count int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM targets").Scan(&count); err != nil {
		return nil, err
	}
	p := backupPayload{BackupInfo: BackupInfo{1, time.Now().UTC(), count, len(key) != 32}, Files: map[string][]byte{"master.key": key}}
	for _, name := range []string{"gateway.db", "host.key"} {
		path := filepath.Join(s.dir, name)
		if name == "gateway.db" {
			path = snapshot
		}
		p.Files[name], err = readBackupFile(path)
		if err != nil {
			return nil, err
		}
	}
	current, err := os.ReadFile(filepath.Join(s.dir, "master.key"))
	if err != nil || !bytes.Equal(current, key) {
		return nil, fmt.Errorf("备份期间主密钥发生变化，请重试")
	}
	plain, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	if len(plain) > backupLimit {
		return nil, fmt.Errorf("备份超过 64 MiB 限制")
	}
	header := make([]byte, 24)
	copy(header, backupMagic)
	if _, err = rand.Read(header[8:]); err != nil {
		return nil, err
	}
	aead, err := masterPasswordCipher(password, header[8:])
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(append(header, nonce...), nonce, plain, header), nil
}
func readBackupFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > backupLimit {
		return nil, fmt.Errorf("备份文件类型或大小无效")
	}
	return os.ReadFile(path)
}
func decodeBackup(data []byte, password string) (backupPayload, error) {
	var p backupPayload
	if len(data) < 52 || len(data) > backupLimit+52 || !bytes.Equal(data[:8], backupMagic) {
		return p, fmt.Errorf("备份格式、大小或版本不受支持")
	}
	if err := validateMasterPassword(password); err != nil {
		return p, err
	}
	aead, err := masterPasswordCipher(password, data[8:24])
	if err != nil {
		return p, err
	}
	plain, err := aead.Open(nil, data[24:36], data[36:], data[:24])
	if err != nil {
		return p, fmt.Errorf("备份密码错误或文件已损坏")
	}
	defer clear(plain)
	decoder := json.NewDecoder(bytes.NewReader(plain))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&p); err != nil {
		return p, fmt.Errorf("备份内容无效")
	}
	if p.Version != 1 || len(p.Files) != 3 || p.CreatedAt.IsZero() {
		return p, fmt.Errorf("备份版本或文件清单无效")
	}
	for _, name := range []string{"gateway.db", "master.key", "host.key"} {
		if len(p.Files[name]) == 0 {
			return p, fmt.Errorf("备份缺少 %s", name)
		}
	}
	key := p.Files["master.key"]
	if len(key) != 32 && !validWrappedMasterKey(key) {
		return p, fmt.Errorf("备份主密钥无效")
	}
	if _, err = ssh.ParsePrivateKey(p.Files["host.key"]); err != nil {
		return p, fmt.Errorf("备份 SSH 主机密钥无效")
	}
	return p, nil
}
func stageBackup(ctx context.Context, parent string, p backupPayload) (string, error) {
	stage, err := os.MkdirTemp(parent, ".restore-")
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(stage)
		}
	}()
	for name, data := range p.Files {
		if err = os.WriteFile(filepath.Join(stage, name), data, 0600); err != nil {
			return "", err
		}
		if err = protectFile(filepath.Join(stage, name)); err != nil {
			return "", err
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(stage, "gateway.db"))
	if err != nil {
		return "", err
	}
	defer db.Close()
	var integrity string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return "", fmt.Errorf("备份数据库完整性检查失败")
	}
	var count int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM targets").Scan(&count); err != nil || count != p.Targets {
		return "", fmt.Errorf("备份数据库与元数据不一致")
	}
	// 检查当前程序所需的表，未知/不完整的数据库不可直接覆盖。
	for _, table := range []string{"settings", "relay_passwords", "mappings", "events", "shortcuts", "users", "user_targets", "user_tags", "global_ips", "shortcut_tags"} {
		var n int
		if err = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n); err != nil || n != 1 {
			return "", fmt.Errorf("备份缺少必需的数据表：%s", table)
		}
	}
	ok = true
	return stage, nil
}
func InspectBackup(ctx context.Context, data []byte, password string) (BackupInfo, error) {
	p, err := decodeBackup(data, password)
	if err != nil {
		return BackupInfo{}, err
	}
	stage, err := stageBackup(ctx, "", p)
	if err != nil {
		return BackupInfo{}, err
	}
	defer os.RemoveAll(stage)
	return p.BackupInfo, nil
}

// RestoreBackup 仅用于停服恢复；保留原数据库、密钥及日志文件，失败时回滚。
func RestoreBackup(ctx context.Context, dir string, data []byte, password string) (string, error) {
	p, err := decodeBackup(data, password)
	if err != nil {
		return "", err
	}
	lock, err := AcquireInstance(dir)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	maintenance, err := acquireMaintenance(dir, true)
	if err != nil {
		return "", err
	}
	defer maintenance.Close()
	if _, err = os.Stat(filepath.Join(dir, "restore.pending")); err == nil {
		return "", fmt.Errorf("检测到未完成的恢复，请先检查 restore.pending 中的原数据目录")
	}
	stage, err := stageBackup(ctx, dir, p)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	previous, err := os.MkdirTemp(dir, ".before-restore-")
	if err != nil {
		return "", err
	}
	if err = protectDirectory(previous); err != nil {
		return "", err
	}
	marker := filepath.Join(dir, "restore.pending")
	if err = os.WriteFile(marker, []byte(previous), 0600); err != nil {
		return "", err
	}
	names := []string{"gateway.db", "gateway.db-wal", "gateway.db-shm", "gateway.db-journal", "master.key", "host.key"}
	moved := []string{}
	installed := []string{}
	rollback := func(cause error) (string, error) {
		var rollbackErr error
		for _, name := range installed {
			if e := os.Remove(filepath.Join(dir, name)); e != nil {
				rollbackErr = e
			}
		}
		for _, name := range moved {
			if e := os.Rename(filepath.Join(previous, name), filepath.Join(dir, name)); e != nil {
				rollbackErr = e
			}
		}
		if rollbackErr != nil {
			return previous, fmt.Errorf("恢复失败：%v；回滚失败：%v，原数据：%s", cause, rollbackErr, previous)
		}
		os.Remove(marker)
		return previous, cause
	}
	for _, name := range names {
		_, e := os.Lstat(filepath.Join(dir, name))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return rollback(e)
		}
		if e = os.Rename(filepath.Join(dir, name), filepath.Join(previous, name)); e != nil {
			return rollback(e)
		}
		moved = append(moved, name)
	}
	for _, name := range []string{"gateway.db", "master.key", "host.key"} {
		if err = os.Rename(filepath.Join(stage, name), filepath.Join(dir, name)); err != nil {
			return rollback(err)
		}
		installed = append(installed, name)
	}
	if err = os.Remove(marker); err != nil {
		return rollback(err)
	}
	return previous, nil
}
