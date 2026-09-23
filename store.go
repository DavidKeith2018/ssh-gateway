package gateway

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// Target 是列表和网页共用的目标配置，不包含密码、私钥或私钥密码。
type Target struct {
	RelayExpiresAt  *time.Time    `json:"relay_expires_at,omitempty"`
	Logins          []TargetLogin `json:"logins,omitempty"`
	Relays          []TargetRelay `json:"relays,omitempty"`
	DefaultLoginID  string        `json:"default_login_id,omitempty"`
	Tags            []string      `json:"tags"`
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Host            string        `json:"host"`
	Port            int           `json:"port"`
	User            string        `json:"user"`
	AuthType        string        `json:"auth_type"`
	HostFingerprint string        `json:"host_fingerprint"`
	RelayUser       string        `json:"relay_user"`
	AllowedSources  []string      `json:"allowed_sources"`
	SourceMode      string        `json:"source_mode"`
	Enabled         bool          `json:"enabled"`
	Revision        int64         `json:"revision"`
}

// PutInput 更新时认证材料留空表示保留；替换私钥时必须同时提供该私钥的口令。
// 新建时中转密码留空则自动生成；更新时非零 Revision 必须匹配原配置版本。
type PutInput struct {
	Target
	LoginInputs         []TargetLoginInput `json:"logins,omitempty"`
	RelayInputs         []TargetRelayInput `json:"relays,omitempty"`
	TargetPassword      string             `json:"target_password"`
	TargetPrivateKey    string             `json:"target_private_key"`
	TargetKeyPassphrase string             `json:"target_key_passphrase"`
	RelayPassword       string             `json:"relay_password"`
}

type record struct {
	isRelay bool
	Target
	loginID  string
	relayID  string
	password []byte // 加密的目标密码或私钥凭证，沿用原数据库列。
	hash     []byte
}

type Store struct {
	maintenance      *os.File
	terminalSourceMu sync.Mutex
	terminalSources  map[string]terminalSourceTicket
	activeMu         sync.Mutex
	activeSSH        map[string]int
	keyMu            sync.RWMutex
	keyFile          []byte
	dataKey          []byte
	unlocked         chan struct{}
	dir              string
	mappingMu        sync.Mutex
	mappings         *MappingManager
	db               *sql.DB
	aead             cipher.AEAD
}

func OpenStore(dir string) (*Store, error) { return openStore(dir, nil) }

// memoryKey 仅供导入暂存库使用，不创建密钥文件；异常退出也不留下裸密钥。
func openStore(dir string, memoryKey []byte) (*Store, error) {
	if _, err := os.Stat(filepath.Join(dir, "restore.pending")); err == nil {
		return nil, fmt.Errorf("检测到未完成的恢复，请检查 restore.pending 并恢复原数据后再启动")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := protectDirectory(dir); err != nil {
		return nil, err
	}
	maintenance, err := acquireMaintenance(dir, false)
	if err != nil {
		return nil, err
	}
	opened := false
	defer func() {
		if !opened {
			maintenance.Close()
		}
	}()
	var key []byte
	if memoryKey != nil {
		if len(memoryKey) != 32 {
			return nil, fmt.Errorf("数据加密密钥长度无效")
		}
		key = append([]byte(nil), memoryKey...)
	} else {
		key, err = loadMasterKey(dir)
		if err != nil {
			return nil, err
		}
	}
	var aead cipher.AEAD
	var dataKey []byte
	unlocked := make(chan struct{})
	if len(key) == 32 {
		block, e := aes.NewCipher(key)
		if e != nil {
			return nil, e
		}
		aead, e = cipher.NewGCM(block)
		if e != nil {
			return nil, e
		}
		dataKey = append([]byte(nil), key...)
		close(unlocked)
	}
	path := filepath.Join(dir, "gateway.db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := protectFile(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000;
PRAGMA secure_delete=ON;
CREATE TABLE IF NOT EXISTS targets (
 id TEXT PRIMARY KEY,
 config TEXT NOT NULL,
 relay_user TEXT NOT NULL UNIQUE,
 password BLOB NOT NULL,
 password_hash BLOB NOT NULL,
 revision INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS mappings (
 id TEXT PRIMARY KEY, target_id TEXT NOT NULL, config TEXT NOT NULL,
 source_ip TEXT NOT NULL, revision INTEGER NOT NULL,
 listener_key TEXT NOT NULL, listen_port INTEGER NOT NULL,
 UNIQUE(listener_key, listen_port)
);
CREATE TRIGGER IF NOT EXISTS delete_target_mappings AFTER DELETE ON targets
 BEGIN DELETE FROM mappings WHERE target_id=OLD.id; END;
CREATE TABLE IF NOT EXISTS relay_passwords (target_id TEXT PRIMARY KEY, passwords TEXT NOT NULL);
CREATE TRIGGER IF NOT EXISTS delete_target_relay_passwords AFTER DELETE ON targets
 BEGIN DELETE FROM relay_passwords WHERE target_id=OLD.id; END;
CREATE TABLE IF NOT EXISTS global_ips (ip TEXT PRIMARY KEY, expires_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS settings (name TEXT PRIMARY KEY, value BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS shortcuts (
 seq INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL, command TEXT NOT NULL, target_id TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS shortcut_tags (
 shortcut_id TEXT NOT NULL, tag TEXT NOT NULL, PRIMARY KEY(shortcut_id,tag)
);
CREATE INDEX IF NOT EXISTS shortcut_tags_tag ON shortcut_tags(tag,shortcut_id);
CREATE TRIGGER IF NOT EXISTS delete_shortcut_tags AFTER DELETE ON shortcuts
 BEGIN DELETE FROM shortcut_tags WHERE shortcut_id=OLD.id; END;
CREATE INDEX IF NOT EXISTS shortcuts_target ON shortcuts(target_id,seq);
CREATE TRIGGER IF NOT EXISTS delete_target_shortcuts AFTER DELETE ON targets
 BEGIN DELETE FROM shortcuts WHERE target_id=OLD.id; END;
CREATE TABLE IF NOT EXISTS events (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 time TEXT NOT NULL,
 target TEXT NOT NULL,
 source TEXT NOT NULL,
 message TEXT NOT NULL
);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{maintenance: maintenance, db: db, aead: aead, dir: dir, keyFile: key, dataKey: dataKey, unlocked: unlocked}
	if err := s.migrateAccountsAndNotes(); err != nil {
		db.Close()
		return nil, err
	}
	if aead != nil {
		if err := s.migrateRelayPasswords(aead); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := s.seedDefaultShortcuts(); err != nil {
		db.Close()
		return nil, err
	}
	opened = true
	return s, nil
}

func (s *Store) Close() error {
	s.mappingMu.Lock()
	m := s.mappings
	s.mappingMu.Unlock()
	if m != nil {
		m.Close()
	}
	err := s.db.Close()
	if s.maintenance != nil {
		s.maintenance.Close()
	}
	s.keyMu.Lock()
	clear(s.dataKey)
	clear(s.keyFile)
	s.aead = nil
	s.keyMu.Unlock()
	return err
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

func validate(t Target) error {
	if !identifier.MatchString(t.ID) || !identifier.MatchString(t.RelayUser) {
		return fmt.Errorf("ID 和中转用户名必须是 1～64 位字母、数字、下划线、点或连字符，且以字母或数字开头")
	}
	if strings.TrimSpace(t.Name) == "" || strings.TrimSpace(t.Host) == "" || strings.TrimSpace(t.User) == "" || strings.ContainsAny(t.Host, " \t\r\n/[]") {
		return fmt.Errorf("名称、主机和目标用户名不能为空；主机应为 IP 或域名，不含端口")
	}
	if t.Port < 1 || t.Port > 65535 {
		return fmt.Errorf("端口必须在 1～65535 之间")
	}
	fp, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(t.HostFingerprint, "SHA256:"))
	if !strings.HasPrefix(t.HostFingerprint, "SHA256:") || err != nil || len(fp) != 32 {
		return fmt.Errorf("必须填写目标主机的 SHA256 SSH 指纹")
	}
	prefixes, err := ParseSources(t.AllowedSources)
	if err != nil {
		return err
	}
	if t.SourceMode != "" && t.SourceMode != "custom" && t.SourceMode != "private" {
		return fmt.Errorf("来源模式必须为 private 或 custom")
	}
	if t.SourceMode == "private" {
		for _, prefix := range prefixes {
			if !privatePrefix(prefix) {
				return fmt.Errorf("仅内网模式只能填写私有或回环网段")
			}
		}
	}
	return nil
}

// Put 保留单账号调用接口；多账号调用者通过 PutTarget 接收生成的全部中转凭证。
func (s *Store) Put(ctx context.Context, in PutInput) (string, error) {
	result, err := s.PutTarget(ctx, in)
	return result.RelayPassword, err
}

func (s *Store) PutTarget(ctx context.Context, in PutInput) (result PutResult, err error) {
	if in.ID == "" {
		in.ID = newTargetID()
	}
	result.ID = in.ID
	in.Tags, err = cleanTags(in.Tags)
	if err != nil {
		return result, err
	}
	if in.Port == 0 {
		in.Port = 22
	}
	old, err := s.get(ctx, "id", in.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	creating := errors.Is(err, sql.ErrNoRows)
	if !creating && in.Revision != 0 && in.Revision != old.Revision {
		return result, fail(409, "配置已被修改，请刷新后重试")
	}
	password, hash := old.password, old.hash
	grouped := in.LoginInputs != nil || in.RelayInputs != nil || len(old.Logins) > 0
	if grouped {
		password, hash, err = s.prepareLogins(&in, old, &result)
		if err != nil {
			return PutResult{}, err
		}
	} else {
		if in.RelayUser == "" {
			in.RelayUser = old.RelayUser
			if in.RelayUser == "" {
				in.RelayUser = newRelayUser(in.User)
			}
		}
		result.RelayUser = in.RelayUser
		if in.AuthType == "" {
			in.AuthType = authType(old.AuthType)
		}
		password, err = s.prepareCredentials(in, old, creating)
		if err != nil {
			return result, err
		}
		if creating && in.RelayPassword == "" {
			in.RelayPassword, err = generateRelayPassword()
			if err != nil {
				return result, err
			}
			result.RelayPassword = in.RelayPassword
		}
		if in.RelayPassword != "" {
			if len(in.RelayPassword) < 12 || len(in.RelayPassword) > 72 {
				return PutResult{}, fmt.Errorf("中转密码长度必须在 12～72 字节之间")
			}
			hash, err = bcrypt.GenerateFromPassword([]byte(in.RelayPassword), bcrypt.DefaultCost)
			if err != nil {
				return PutResult{}, err
			}
		}
	}
	if err := validate(in.Target); err != nil {
		return PutResult{}, err
	}
	in.Revision = 0
	config, err := json.Marshal(in.Target)
	if err != nil {
		return PutResult{}, err
	}
	relaySecrets, err := s.prepareRelayExport(ctx, in, result)
	if err != nil {
		return PutResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PutResult{}, err
	}
	defer tx.Rollback()
	// 在同一个事务内检查所有中转名称，避免不同机器或管理进程抢占同名凭证。
	for _, username := range targetRelayNames(in.Target) {
		var exists bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM targets WHERE id<>? AND (relay_user=? OR EXISTS(SELECT 1 FROM json_each(config,'$.relays') WHERE json_extract(value,'$.username')=?)))`, in.ID, username, username).Scan(&exists)
		if err != nil {
			return PutResult{}, err
		}
		if exists {
			return PutResult{}, fail(409, "中转用户名已被其他机器使用")
		}
	}
	if creating {
		_, err = tx.ExecContext(ctx, `INSERT INTO targets(id,config,relay_user,password,password_hash,revision) VALUES(?,?,?,?,?,1)`, in.ID, string(config), in.RelayUser, password, hash)
	} else {
		var updated sql.Result
		updated, err = tx.ExecContext(ctx, `UPDATE targets SET config=?,relay_user=?,password=?,password_hash=?,revision=revision+1 WHERE id=? AND revision=?`, string(config), in.RelayUser, password, hash, in.ID, old.Revision)
		if err == nil {
			if n, _ := updated.RowsAffected(); n != 1 {
				err = fail(409, "配置已被其他进程修改，请刷新后重试")
			}
		}
	}
	if err != nil {
		return PutResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO relay_passwords(target_id,passwords) VALUES(?,?) ON CONFLICT(target_id) DO UPDATE SET passwords=excluded.passwords`, in.ID, relaySecrets); err != nil {
		return PutResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return PutResult{}, err
	}
	if grouped {
		in.Revision = old.Revision + 1
		result.Target = &in.Target
	}
	return result, nil
}

func (s *Store) get(ctx context.Context, column, value string) (record, error) {
	var r record
	var config string
	// column 只由本包的固定调用点提供。
	err := s.db.QueryRowContext(ctx, `SELECT config, password, password_hash, revision FROM targets WHERE `+column+`=?`, value).Scan(&config, &r.password, &r.hash, &r.Revision)
	if err != nil {
		return r, err
	}
	revision := r.Revision
	err = json.Unmarshal([]byte(config), &r.Target)
	if r.Tags == nil {
		r.Tags = []string{}
	}
	r.AuthType = authType(r.AuthType)
	r.Revision = revision
	return r, err
}

func (s *Store) Get(ctx context.Context, id string) (Target, error) {
	r, err := s.get(ctx, "id", id)
	return r.Target, err
}

func (s *Store) List(ctx context.Context) ([]Target, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT config, revision FROM targets ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Target, 0)
	for rows.Next() {
		var config string
		var revision int64
		if err := rows.Scan(&config, &revision); err != nil {
			return nil, err
		}
		var item Target
		if err := json.Unmarshal([]byte(config), &item); err != nil {
			return nil, err
		}
		if item.Tags == nil {
			item.Tags = []string{}
		}
		item.Revision = revision
		item.AuthType = authType(item.AuthType)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM targets WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

var ErrDenied = errors.New("认证失败或来源 IP 不允许")

func (s *Store) authenticate(ctx context.Context, user string, password []byte, remote net.Addr) (record, error) {
	if s.MasterPasswordStatus().Locked {
		return record{}, ErrMasterLocked
	}
	r, err := s.relayRecord(ctx, user)
	if err != nil || !r.Enabled || (len(r.Relays) == 0 && r.RelayExpiresAt != nil && !time.Now().Before(*r.RelayExpiresAt)) || !s.sourceAllowed(ctx, r.Target, remote) {
		return record{}, ErrDenied
	}
	if len(r.Logins) > 0 {
		secrets, e := s.savedSecrets(r)
		if e != nil {
			return record{}, ErrDenied
		}
		found := false
		for _, relay := range r.Relays {
			if relay.Username == user && relay.Active() {
				r.relayID, r.loginID, r.hash = relay.ID, relay.LoginID, secrets.Relays[relay.ID]
				for _, login := range r.Logins {
					if login.ID == relay.LoginID {
						r.User, r.AuthType = login.User, login.AuthType
						found = true
					}
				}
				break
			}
		}
		if !found {
			return record{}, ErrDenied
		}
	}
	r.isRelay = true
	if bcrypt.CompareHashAndPassword(r.hash, password) != nil {
		return record{}, ErrDenied
	}
	return r, nil
}

func (s *Store) decrypt(r record) (string, error) {
	aead, err := s.credentialCipher()
	if err != nil {
		return "", err
	}
	n := aead.NonceSize()
	if len(r.password) < n {
		return "", fmt.Errorf("目标认证数据损坏")
	}
	aad := credentialAAD(r.ID, r.AuthType)
	if len(r.Logins) > 0 {
		aad = []byte(r.ID + "\x00logins")
	}
	data, err := aead.Open(nil, r.password[:n], r.password[n:], aad)
	return string(data), err
}
