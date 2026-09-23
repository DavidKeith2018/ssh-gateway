package gateway

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// 每台机器可以保存多个登录账号，中转凭证通过 LoginID 明确绑定目标账号。
type TargetLogin struct {
	ID       string `json:"id"`
	User     string `json:"user"`
	AuthType string `json:"auth_type"`
}
type TargetRelay struct {
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	LoginID   string     `json:"login_id"`
	Enabled   bool       `json:"enabled"`
}
type TargetLoginInput struct {
	TargetLogin
	TargetPassword      string `json:"target_password,omitempty"`
	TargetPrivateKey    string `json:"target_private_key,omitempty"`
	TargetKeyPassphrase string `json:"target_key_passphrase,omitempty"`
}
type TargetRelayInput struct {
	TargetRelay
	Password string `json:"password,omitempty"`
}
type RelayCredential struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	LoginID  string `json:"login_id"`
	Password string `json:"password"`
}
type PutResult struct {
	Target        *Target           `json:"target,omitempty"`
	ID            string            `json:"id"`
	RelayUser     string            `json:"relay_user"`
	RelayPassword string            `json:"relay_password"`
	Credentials   []RelayCredential `json:"credentials,omitempty"`
}
type loginSecret struct {
	AuthType   string `json:"auth_type"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}
type targetSecrets struct {
	Logins map[string]loginSecret `json:"logins"`
	Relays map[string][]byte      `json:"relays"`
}

func newTargetID() string { return "ssh-" + uuid.NewString() }
func newRelayUser(user string) string {
	var label strings.Builder
	for _, ch := range user {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' {
			label.WriteRune(ch)
		}
		if label.Len() >= 24 {
			break
		}
	}
	if label.Len() == 0 {
		label.WriteString("user")
	}
	return "relay-" + label.String() + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
}
func generateRelayPassword() (string, error) {
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(secret), nil
}
func targetRelayNames(t Target) []string {
	if len(t.Relays) == 0 {
		return []string{t.RelayUser}
	}
	names := make([]string, 0, len(t.Relays))
	for _, relay := range t.Relays {
		names = append(names, relay.Username)
	}
	return names
}

// 旧配置仅在首次编辑多账号时转换，保留原凭证、账号和中转口令。
func (s *Store) savedSecrets(old record) (targetSecrets, error) {
	result := targetSecrets{Logins: map[string]loginSecret{}, Relays: map[string][]byte{}}
	if len(old.password) == 0 {
		return result, nil
	}
	plain, err := s.decrypt(old)
	if err != nil {
		return result, fmt.Errorf("无法读取已保存的目标凭证")
	}
	if len(old.Logins) > 0 {
		if err = json.Unmarshal([]byte(plain), &result); err != nil || result.Logins == nil || result.Relays == nil {
			return result, fmt.Errorf("目标凭证数据损坏")
		}
		return result, nil
	}
	secret := loginSecret{AuthType: authType(old.AuthType)}
	if secret.AuthType == "password" {
		secret.Password = plain
	} else {
		var key privateKeyCredentials
		if err = json.Unmarshal([]byte(plain), &key); err != nil {
			return result, err
		}
		secret.PrivateKey, secret.Passphrase = key.PrivateKey, key.Passphrase
	}
	result.Logins["default"] = secret
	result.Relays["default"] = old.hash
	return result, nil
}

func (s *Store) prepareLogins(in *PutInput, old record, result *PutResult) ([]byte, []byte, error) {
	saved, err := s.savedSecrets(old)
	if err != nil {
		return nil, nil, err
	}
	oldLogins, oldRelays := old.Logins, old.Relays
	if len(oldLogins) == 0 && len(old.password) > 0 {
		oldLogins = []TargetLogin{{ID: "default", User: old.User, AuthType: authType(old.AuthType)}}
		oldRelays = []TargetRelay{{ID: "default", Username: old.RelayUser, LoginID: "default", Enabled: true, ExpiresAt: old.RelayExpiresAt}}
	}
	if in.LoginInputs == nil {
		for _, login := range oldLogins {
			in.LoginInputs = append(in.LoginInputs, TargetLoginInput{TargetLogin: login})
		}
		for i := range in.LoginInputs {
			login := &in.LoginInputs[i]
			if login.ID == old.DefaultLoginID || old.DefaultLoginID == "" && i == 0 {
				if in.User != "" {
					login.User = in.User
				}
				if in.AuthType != "" {
					login.AuthType = in.AuthType
				}
				login.TargetPassword, login.TargetPrivateKey, login.TargetKeyPassphrase = in.TargetPassword, in.TargetPrivateKey, in.TargetKeyPassphrase
			}
		}
	}
	if in.RelayInputs == nil {
		for _, relay := range oldRelays {
			in.RelayInputs = append(in.RelayInputs, TargetRelayInput{TargetRelay: relay})
		}
		if len(in.RelayInputs) > 0 {
			if in.RelayUser != "" {
				in.RelayInputs[0].Username = in.RelayUser
			}
			in.RelayInputs[0].Password = in.RelayPassword
		}
	}
	if len(in.LoginInputs) < 1 || len(in.LoginInputs) > 16 {
		return nil, nil, fmt.Errorf("请配置 1～16 个目标账号")
	}
	if len(in.RelayInputs) < 1 || len(in.RelayInputs) > 32 {
		return nil, nil, fmt.Errorf("请配置 1～32 组中转凭证")
	}
	next := targetSecrets{Logins: map[string]loginSecret{}, Relays: map[string][]byte{}}
	in.Logins = []TargetLogin{}
	users := map[string]bool{}
	for _, input := range in.LoginInputs {
		if input.ID == "" {
			input.ID = uuid.NewString()
		}
		input.User = strings.TrimSpace(input.User)
		if !identifier.MatchString(input.ID) || input.User == "" || len(input.User) > 128 || strings.ContainsAny(input.User, "\r\n\x00") {
			return nil, nil, fmt.Errorf("目标账号名称或编号无效")
		}
		if _, ok := next.Logins[input.ID]; ok || users[input.User] {
			return nil, nil, fmt.Errorf("目标账号名称和编号不能重复")
		}
		users[input.User] = true
		previous, exists := saved.Logins[input.ID]
		if input.AuthType == "" && exists {
			input.AuthType = previous.AuthType
		}
		input.AuthType = authType(input.AuthType)
		secret := loginSecret{AuthType: input.AuthType}
		switch input.AuthType {
		case "password":
			if input.TargetPrivateKey != "" || input.TargetKeyPassphrase != "" {
				return nil, nil, fmt.Errorf("密码认证不能同时填写私钥")
			}
			secret.Password = input.TargetPassword
			if secret.Password == "" && exists && previous.AuthType == input.AuthType {
				secret = previous
			}
			if secret.Password == "" {
				return nil, nil, fmt.Errorf("请填写目标账号 %s 的密码", input.User)
			}
		case "private_key":
			if input.TargetPassword != "" {
				return nil, nil, fmt.Errorf("私钥认证不能同时填写目标密码")
			}
			if input.TargetPrivateKey == "" {
				if !exists || previous.AuthType != input.AuthType || input.TargetKeyPassphrase != "" {
					return nil, nil, fmt.Errorf("请提供目标账号 %s 的私钥；修改私钥密码也需重新提供私钥", input.User)
				}
				secret = previous
			} else {
				if _, err := parseTargetKey(input.TargetPrivateKey, input.TargetKeyPassphrase); err != nil {
					return nil, nil, err
				}
				secret.PrivateKey, secret.Passphrase = input.TargetPrivateKey, input.TargetKeyPassphrase
			}
		default:
			return nil, nil, fmt.Errorf("目标认证方式无效")
		}
		in.Logins = append(in.Logins, input.TargetLogin)
		next.Logins[input.ID] = secret
	}
	if in.DefaultLoginID == "" {
		in.DefaultLoginID = old.DefaultLoginID
	}
	if in.DefaultLoginID == "" {
		in.DefaultLoginID = in.Logins[0].ID
	}
	if _, ok := next.Logins[in.DefaultLoginID]; !ok {
		return nil, nil, fmt.Errorf("请选择有效的默认目标账号")
	}
	for _, login := range in.Logins {
		if login.ID == in.DefaultLoginID {
			in.User, in.AuthType = login.User, login.AuthType
		}
	}
	in.Relays = []TargetRelay{}
	names := map[string]bool{}
	for _, input := range in.RelayInputs {
		if input.ID == "" {
			input.ID = uuid.NewString()
		}
		if input.Username == "" {
			user := ""
			for _, login := range in.Logins {
				if login.ID == input.LoginID {
					user = login.User
					break
				}
			}
			input.Username = newRelayUser(user)
		}
		if !identifier.MatchString(input.ID) || !identifier.MatchString(input.Username) {
			return nil, nil, fmt.Errorf("中转账号名称或编号无效")
		}
		if _, ok := next.Relays[input.ID]; ok || names[input.Username] {
			return nil, nil, fmt.Errorf("中转账号名称和编号不能重复")
		}
		names[input.Username] = true
		if _, ok := next.Logins[input.LoginID]; !ok {
			return nil, nil, fmt.Errorf("中转账号必须绑定一个有效的目标账号")
		}
		hash := saved.Relays[input.ID]
		if len(hash) == 0 && input.Password == "" {
			input.Password, err = generateRelayPassword()
			if err != nil {
				return nil, nil, err
			}
		}
		if input.Password != "" {
			if len(input.Password) < 12 || len(input.Password) > 72 {
				return nil, nil, fmt.Errorf("中转密码长度必须在 12～72 字节之间")
			}
			hash, err = bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
			if err != nil {
				return nil, nil, err
			}
			result.Credentials = append(result.Credentials, RelayCredential{input.ID, input.Username, input.LoginID, input.Password})
		}
		next.Relays[input.ID] = hash
		in.Relays = append(in.Relays, input.TargetRelay)
	}
	in.RelayUser = in.Relays[0].Username
	result.RelayUser = in.RelayUser
	for _, credential := range result.Credentials {
		if credential.ID == in.Relays[0].ID {
			result.RelayPassword = credential.Password
		}
	}
	plain, err := json.Marshal(next)
	if err != nil {
		return nil, nil, err
	}
	aead, err := s.credentialCipher()
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	encrypted := aead.Seal(nonce, nonce, plain, []byte(in.ID+"\x00logins"))
	return encrypted, next.Relays[in.Relays[0].ID], nil
}

func (s *Store) relayRecord(ctx context.Context, username string) (record, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM targets WHERE relay_user=? OR EXISTS (SELECT 1 FROM json_each(config,'$.relays') WHERE json_extract(value,'$.username')=?)`, username, username).Scan(&id)
	if err != nil {
		return record{}, err
	}
	return s.get(ctx, "id", id)
}

func (r TargetRelay) Active() bool {
	return r.Enabled && (r.ExpiresAt == nil || time.Now().Before(*r.ExpiresAt))
}
