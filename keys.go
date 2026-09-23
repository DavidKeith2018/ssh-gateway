package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// 私密文件使用排他创建，已有文件绝不覆盖。
func createSecret(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if err = protectFile(path); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}

func readSecret(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("私密文件必须是权限为 0600 的普通文件：%s", path)
	}
	if err := checkPrivateFile(path, info); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func loadMasterKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, "master.key")
	key, err := readSecret(path)
	if errors.Is(err, os.ErrNotExist) {
		if _, dbErr := os.Stat(filepath.Join(dir, "gateway.db")); !errors.Is(dbErr, os.ErrNotExist) {
			return nil, fmt.Errorf("已有数据库但缺少 master.key，请恢复原密钥")
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		if err = createSecret(path, key); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		key, err = readSecret(path)
	}
	if err != nil {
		return nil, err
	}
	if len(key) != 32 && !validWrappedMasterKey(key) {
		return nil, fmt.Errorf("master.key 损坏或版本不受支持，请恢复原密钥")
	}
	return key, nil
}

// LoadHostKey 生成或读取持久化的堡垒机 SSH 主机密钥。
func LoadHostKey(dir string) (ssh.Signer, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := protectDirectory(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "host.key")
	data, err := readSecret(path)
	if errors.Is(err, os.ErrNotExist) {
		_, key, genErr := ed25519.GenerateKey(rand.Reader)
		if genErr != nil {
			return nil, genErr
		}
		block, genErr := ssh.MarshalPrivateKey(key, "SSH 中转主机密钥")
		if genErr != nil {
			return nil, genErr
		}
		if err = createSecret(path, pem.EncodeToMemory(block)); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		data, err = readSecret(path)
	}
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}
