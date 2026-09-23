package gateway

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const relayCipherPrefix = "aes-gcm:v1:"

func encryptRelayPasswords(aead cipher.AEAD, id string, plain []byte) (string, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	encrypted := aead.Seal(nonce, nonce, plain, []byte(id+"\x00relay-passwords"))
	return relayCipherPrefix + base64.RawStdEncoding.EncodeToString(encrypted), nil
}

// 启动或主密码解锁时，将此前保存的明文一次性转为密文，不改变账号、密码或目标版本。
// 调用方传入已解锁的密码器，避免在主密码解锁持锁期间再次获取 keyMu。
func (s *Store) migrateRelayPasswords(aead cipher.AEAD) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT target_id,passwords FROM relay_passwords`)
	if err != nil {
		return err
	}
	updates := map[string]string{}
	for rows.Next() {
		var id, stored string
		if err := rows.Scan(&id, &stored); err != nil {
			rows.Close()
			return err
		}
		if strings.HasPrefix(stored, relayCipherPrefix) {
			continue
		}
		var passwords map[string]string
		if err := json.Unmarshal([]byte(stored), &passwords); err != nil {
			rows.Close()
			return fmt.Errorf("中转凭证数据损坏，无法迁移")
		}
		encrypted, err := encryptRelayPasswords(aead, id, []byte(stored))
		if err != nil {
			rows.Close()
			return err
		}
		updates[id] = encrypted
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for id, encrypted := range updates {
		if _, err := tx.Exec(`UPDATE relay_passwords SET passwords=? WHERE target_id=?`, encrypted, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// 中转密码可逆加密保存，获授权用户通过独立接口解密读取。
func (s *Store) relayPasswords(ctx context.Context, id string) (map[string]string, error) {
	aead, err := s.credentialCipher()
	if err != nil {
		return nil, err
	}
	passwords := map[string]string{}
	var plain string
	err = s.db.QueryRowContext(ctx, `SELECT passwords FROM relay_passwords WHERE target_id=?`, id).Scan(&plain)
	if errors.Is(err, sql.ErrNoRows) {
		return passwords, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(plain, relayCipherPrefix) {
		encrypted, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(plain, relayCipherPrefix))
		if err != nil || len(encrypted) < aead.NonceSize() {
			return nil, fmt.Errorf("中转凭证数据损坏")
		}
		decrypted, err := aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], []byte(id+"\x00relay-passwords"))
		if err != nil {
			return nil, fmt.Errorf("无法解密中转凭证")
		}
		defer clear(decrypted)
		if err := json.Unmarshal(decrypted, &passwords); err != nil {
			return nil, fmt.Errorf("中转凭证数据损坏")
		}
	} else if err := json.Unmarshal([]byte(plain), &passwords); err != nil {
		return nil, fmt.Errorf("中转凭证数据损坏")
	}
	return passwords, nil
}

func (s *Store) prepareRelayExport(ctx context.Context, in PutInput, result PutResult) (string, error) {
	previous, err := s.relayPasswords(ctx, in.ID)
	if err != nil {
		return "", err
	}
	passwords := map[string]string{}
	if len(in.Relays) == 0 {
		passwords["default"] = previous["default"]
		if in.RelayPassword != "" {
			passwords["default"] = in.RelayPassword
		}
	} else {
		for _, relay := range in.Relays {
			passwords[relay.ID] = previous[relay.ID]
		}
		for _, credential := range result.Credentials {
			passwords[credential.ID] = credential.Password
		}
	}
	plain, err := json.Marshal(passwords)
	if err != nil {
		return "", err
	}
	defer clear(plain)
	aead, err := s.credentialCipher()
	if err != nil {
		return "", err
	}
	return encryptRelayPasswords(aead, in.ID, plain)
}

type relayAccess struct {
	SSHHost     string          `json:"ssh_host"`
	Target      Target          `json:"target"`
	Credential  RelayCredential `json:"credential"`
	Login       TargetLogin     `json:"login"`
	GlobalIPs   []GlobalIP      `json:"global_ips"`
	SSHPort     string          `json:"ssh_port"`
	Fingerprint string          `json:"host_fingerprint"`
}

func (web *Web) relayCredentials(w http.ResponseWriter, r *http.Request) {
	target, err := web.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		apiError(w, 404, "目标不存在")
		return
	}
	relays, logins := target.Relays, target.Logins
	if len(relays) == 0 {
		relays = []TargetRelay{{ID: "default", Username: target.RelayUser, LoginID: "default", Enabled: true, ExpiresAt: target.RelayExpiresAt}}
		logins = []TargetLogin{{ID: "default", User: target.User, AuthType: target.AuthType}}
	}
	var selected *TargetRelay
	for _, relay := range relays {
		if relay.ID == r.PathValue("relay") {
			selected = &relay
			break
		}
	}
	if selected == nil {
		apiError(w, 404, "中转凭证不存在")
		return
	}
	if !selected.Active() && selected.Enabled {
		apiError(w, 409, "中转凭证已过期，请续期后复制")
		return
	}
	if !target.Enabled || !selected.Enabled {
		apiError(w, 409, "目标或中转凭证已禁用，请启用后复制")
		return
	}
	var login TargetLogin
	for _, item := range logins {
		if item.ID == selected.LoginID {
			login = item
			break
		}
	}
	if login.ID == "" {
		apiError(w, 409, "中转凭证未绑定有效目标账号")
		return
	}
	passwords, err := web.store.relayPasswords(r.Context(), target.ID)
	if err != nil {
		apiError(w, 500, "读取中转密码失败，请重试")
		return
	}
	globalIPs, err := web.store.ListGlobalIPs(r.Context())
	if err != nil {
		apiError(w, 500, "读取允许来源失败，请重试")
		return
	}
	active := make([]GlobalIP, 0, len(globalIPs))
	for _, item := range globalIPs {
		if item.ExpiresAt.After(time.Now()) {
			active = append(active, item)
		}
	}
	latest, err := web.store.Get(r.Context(), target.ID)
	if err != nil || latest.Revision != target.Revision {
		apiError(w, 409, "配置已被修改，请重新读取中转凭证")
		return
	}
	host, port, err := web.relayAccessEndpoint(r.Context())
	if err != nil {
		apiError(w, 500, "读取中转访问设置失败")
		return
	}
	if !selected.Active() {
		apiError(w, 409, "中转凭证已过期，请续期后复制")
		return
	}
	if !web.canAccess(r, target.ID) {
		apiError(w, 403, "没有此机器的访问权限")
		return
	}
	web.store.logEvent(target.ID, sourceIP(r), "读取中转凭证："+selected.Username)
	jsonResponse(w, 200, relayAccess{host, target, RelayCredential{selected.ID, selected.Username, selected.LoginID, passwords[selected.ID]}, login, active, port, ssh.FingerprintSHA256(web.signer.PublicKey())})
}
