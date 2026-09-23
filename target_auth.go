package gateway

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"
)

const maxPrivateKeySize = 32 << 10

type privateKeyCredentials struct {
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase"`
}

func authType(value string) string {
	if value == "" {
		return "password"
	}
	return value
}

func credentialAAD(id, kind string) []byte {
	if authType(kind) == "password" {
		return []byte(id) // 保持旧数据库的密码密文兼容。
	}
	return []byte(id + "\x00" + kind)
}

func parseTargetKey(key, passphrase string) (ssh.Signer, error) {
	if len(key) > maxPrivateKeySize || len(passphrase) > 4096 {
		return nil, fmt.Errorf("私钥不能超过 32 KiB，私钥密码不能超过 4096 字节")
	}
	signer, err := ssh.ParsePrivateKey([]byte(key))
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) {
		if passphrase == "" {
			return nil, fmt.Errorf("该私钥已加密，请填写私钥密码")
		}
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(key), []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("无法解密私钥，请检查私钥密码和私钥内容")
		}
	} else if err != nil {
		return nil, fmt.Errorf("私钥格式无效或不受支持，请提供 OpenSSH 或 PEM 格式私钥")
	} else if passphrase != "" {
		return nil, fmt.Errorf("该私钥未加密，请清空私钥密码")
	}
	return signer, nil
}

func (s *Store) prepareCredentials(in PutInput, old record, creating bool) ([]byte, error) {
	required := creating || in.AuthType != authType(old.AuthType)
	var plaintext []byte
	switch in.AuthType {
	case "password":
		if in.TargetPrivateKey != "" || in.TargetKeyPassphrase != "" {
			return nil, fmt.Errorf("密码认证不能同时提供私钥或私钥密码")
		}
		if in.TargetPassword == "" {
			if required {
				return nil, fmt.Errorf("新建目标或切换到密码认证时必须提供目标密码")
			}
			return old.password, nil
		}
		plaintext = []byte(in.TargetPassword)
	case "private_key":
		if in.TargetPassword != "" {
			return nil, fmt.Errorf("私钥认证不能同时提供目标密码")
		}
		if in.TargetPrivateKey == "" {
			if required || in.TargetKeyPassphrase != "" {
				return nil, fmt.Errorf("请提供私钥；更换私钥密码时也需重新提供私钥")
			}
			return old.password, nil
		}
		if _, err := parseTargetKey(in.TargetPrivateKey, in.TargetKeyPassphrase); err != nil {
			return nil, err
		}
		var err error
		plaintext, err = json.Marshal(privateKeyCredentials{in.TargetPrivateKey, in.TargetKeyPassphrase})
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("认证方式必须为 password 或 private_key")
	}
	aead, err := s.credentialCipher()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, credentialAAD(in.ID, in.AuthType)), nil
}

func (s *Store) targetAuth(r record) (ssh.AuthMethod, error) {
	if len(r.Logins) > 0 {
		secrets, err := s.savedSecrets(r)
		if err != nil {
			return nil, err
		}
		id := r.loginID
		if id == "" {
			id = r.DefaultLoginID
		}
		secret, ok := secrets.Logins[id]
		if !ok || secret.AuthType != r.AuthType {
			return nil, fmt.Errorf("目标账号凭证无效")
		}
		if secret.AuthType == "password" {
			return ssh.Password(secret.Password), nil
		}
		signer, err := parseTargetKey(secret.PrivateKey, secret.Passphrase)
		if err != nil {
			return nil, err
		}
		return ssh.PublicKeys(signer), nil
	}

	secret, err := s.decrypt(r)
	if err != nil {
		return nil, fmt.Errorf("无法解密目标认证信息")
	}
	switch authType(r.AuthType) {
	case "password":
		return ssh.Password(secret), nil
	case "private_key":
		var credentials privateKeyCredentials
		if err := json.Unmarshal([]byte(secret), &credentials); err != nil {
			return nil, fmt.Errorf("目标私钥数据损坏")
		}
		signer, err := parseTargetKey(credentials.PrivateKey, credentials.Passphrase)
		if err != nil {
			return nil, err
		}
		return ssh.PublicKeys(signer), nil
	default:
		return nil, fmt.Errorf("不支持的目标认证方式")
	}
}
