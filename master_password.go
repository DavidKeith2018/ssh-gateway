package gateway

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// 版本 1 固定使用 Argon2id（64 MiB、3 次、2 路）和 AES-256-GCM。
// 头部、盐和版本一并认证，不接受文件提供的任意 KDF 参数。
var masterPasswordMagic = []byte{'S', 'G', 'M', 'P', 0, 0, 0, 1}

const wrappedMasterKeySize = 8 + 16 + 12 + 32 + 16

func validateMasterPassword(password string) error {
	if len(password) < 12 || len(password) > 1024 {
		return fmt.Errorf("主密码长度必须为 12～1024 字节")
	}
	return nil
}

func masterPasswordCipher(password string, salt []byte) (cipher.AEAD, error) {
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func wrapMasterKey(key []byte, password string) ([]byte, error) {
	if err := validateMasterPassword(password); err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("数据加密密钥长度无效")
	}
	header := make([]byte, 24)
	copy(header, masterPasswordMagic)
	if _, err := rand.Read(header[8:]); err != nil {
		return nil, err
	}
	aead, err := masterPasswordCipher(password, header[8:])
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	result := append(header, nonce...)
	return aead.Seal(result, nonce, key, header), nil
}

// The unified envelope commits the administrator hash and wrapped key together.
var unifiedPasswordMagic = []byte{'S', 'G', 'M', 'P', 0, 0, 0, 2}

const unifiedMasterKeySize = wrappedMasterKeySize + 60

func unifiedAdminHash(data []byte) []byte {
	if len(data) == unifiedMasterKeySize && bytes.Equal(data[:8], unifiedPasswordMagic) {
		return data[24:84]
	}
	return nil
}
func validWrappedMasterKey(data []byte) bool {
	return len(unifiedAdminHash(data)) > 0 || (len(data) == wrappedMasterKeySize && bytes.Equal(data[:8], masterPasswordMagic))
}
func wrapUnifiedMasterKey(key []byte, password string, hash []byte) ([]byte, error) {
	if len(hash) != 60 || len(key) != 32 {
		return nil, fmt.Errorf("Invalid credential envelope")
	}
	header := make([]byte, 84)
	copy(header, unifiedPasswordMagic)
	if _, err := rand.Read(header[8:24]); err != nil {
		return nil, err
	}
	copy(header[24:], hash)
	aead, err := masterPasswordCipher(password, header[8:24])
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(append(header, nonce...), nonce, key, header), nil
}
func unwrapMasterKey(data []byte, password string) ([]byte, error) {
	if !validWrappedMasterKey(data) {
		return nil, fmt.Errorf("主密码密钥文件损坏或版本不受支持，请恢复备份")
	}
	if len(password) > 1024 {
		return nil, fmt.Errorf("主密码过长")
	}
	aead, err := masterPasswordCipher(password, data[8:24])
	if err != nil {
		return nil, err
	}
	headerSize := 24
	if len(unifiedAdminHash(data)) > 0 {
		headerSize = 84
	}
	key, err := aead.Open(nil, data[headerSize:headerSize+12], data[headerSize+12:], data[:headerSize])
	if err != nil {
		return nil, fmt.Errorf("主密码不正确或密钥文件已损坏")
	}
	return key, nil
}

// MasterPasswordStatus 只公开锁定状态，不包含密钥或凭证。
type MasterPasswordStatus struct {
	Enabled bool `json:"enabled"`
	Locked  bool `json:"locked"`
}

var ErrMasterLocked = errors.New("凭证已锁定，请先输入主密码解锁")

func (s *Store) MasterPasswordStatus() MasterPasswordStatus {
	s.keyMu.RLock()
	defer s.keyMu.RUnlock()
	return MasterPasswordStatus{Enabled: len(s.keyFile) != 32, Locked: s.aead == nil}
}

func (s *Store) credentialCipher() (cipher.AEAD, error) {
	s.keyMu.RLock()
	defer s.keyMu.RUnlock()
	if s.aead == nil {
		return nil, ErrMasterLocked
	}
	return s.aead, nil
}

func (s *Store) UnlockMasterPassword(password string) error {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if s.aead != nil {
		return nil
	}
	// 每次解锁都核对磁盘，避免另一个进程修改主密码后继续接受旧密码。
	data, err := readSecret(filepath.Join(s.dir, "master.key"))
	if err != nil {
		return err
	}
	if !bytes.Equal(data, s.keyFile) {
		return fmt.Errorf("主密钥文件已被其他进程修改，请重启后解锁")
	}
	key, err := unwrapMasterKey(data, password)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		clear(key)
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		clear(key)
		return err
	}
	if err := s.migrateRelayPasswords(aead); err != nil {
		clear(key)
		return err
	}
	s.dataKey, s.aead = key, aead
	close(s.unlocked)
	return nil
}

// ChangeMasterPassword 只重包裹数据密钥，不重写数据库中的凭证。
// newPassword 为空表示明确关闭主密码保护。启用后本次进程保持解锁。
func (s *Store) ChangeMasterPassword(current, newPassword string) error {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if s.aead == nil {
		return ErrMasterLocked
	}
	if newPassword != "" {
		if err := validateMasterPassword(newPassword); err != nil {
			return err
		}
	}
	if len(s.keyFile) != 32 {
		key, err := unwrapMasterKey(s.keyFile, current)
		if err != nil {
			return err
		}
		matches := bytes.Equal(key, s.dataKey)
		clear(key)
		if !matches {
			return fmt.Errorf("数据密钥不一致，请重启应用")
		}
	} else if newPassword == "" {
		return fmt.Errorf("尚未启用主密码")
	}
	data := bytes.Clone(s.dataKey)
	defer clear(data)
	var err error
	if newPassword != "" {
		clear(data)
		data, err = wrapMasterKey(s.dataKey, newPassword)
		if err != nil {
			return err
		}
	}
	if err = replaceMasterKey(s.dir, s.keyFile, data); err != nil {
		return err
	}
	clear(s.keyFile)
	s.keyFile = bytes.Clone(data)
	return nil
}

// 同目录临时文件写入并同步成功后再替换，绝不先截断唯一的有效密钥文件。
func replaceMasterKey(dir string, previous, next []byte) error {
	lockPath := filepath.Join(dir, "master-password.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = protectFile(lockPath); err != nil {
		return err
	}
	if err = lockFile(lock); err != nil {
		return fmt.Errorf("另一个进程正在修改主密码，请稍后重试")
	}
	path := filepath.Join(dir, "master.key")
	current, err := readSecret(path)
	if err != nil {
		return err
	}
	defer clear(current)
	if !bytes.Equal(current, previous) {
		return fmt.Errorf("主密钥文件已改变，请重启后重试")
	}
	f, err := os.CreateTemp(dir, ".master-key-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	defer f.Close()
	if err = protectFile(name); err != nil {
		return err
	}
	if _, err = f.Write(next); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = replaceSecretFile(name, path); err != nil {
		return err
	}
	return nil
}

// SetCredentialProtection uses the administrator password for optional encryption.
func (s *Store) SetCredentialProtection(ctx context.Context, password string, enabled bool) error {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if s.aead == nil {
		return ErrMasterLocked
	}
	hash := bytes.Clone(unifiedAdminHash(s.keyFile))
	if len(hash) == 0 {
		if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE name='admin_password'`).Scan(&hash); err != nil {
			return err
		}
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil {
		return fmt.Errorf("管理员密码不正确")
	}
	var data []byte
	var err error
	if enabled {
		data, err = wrapUnifiedMasterKey(s.dataKey, password, hash)
	} else {
		// Persist the authoritative hash before removing the envelope. A failed key
		// replacement leaves the same password active in the encrypted envelope.
		_, err = s.db.ExecContext(ctx, `UPDATE settings SET value=? WHERE name='admin_password'`, hash)
		data = bytes.Clone(s.dataKey)
	}
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
