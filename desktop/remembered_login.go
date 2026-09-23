package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zalando/go-keyring"
)

const loginKeyringService = "SSH Gateway Desktop Login"

type RememberedLogin struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func loginKeyringAccount(dir string) string {
	path, _ := filepath.Abs(dir)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:])
}

func readRememberedLogin(dir string) (RememberedLogin, error) {
	var login RememberedLogin
	data, err := keyring.Get(loginKeyringService, loginKeyringAccount(dir))
	if errors.Is(err, keyring.ErrNotFound) {
		return login, nil
	}
	if err != nil {
		return login, fmt.Errorf("System credential storage is unavailable")
	}
	if json.Unmarshal([]byte(data), &login) != nil {
		return RememberedLogin{}, fmt.Errorf("Saved login is invalid")
	}
	return login, nil
}
func writeRememberedLogin(dir, username, password string) error {
	if username == "" || password == "" || len(password) > 72 {
		return fmt.Errorf("Invalid saved login")
	}
	data, _ := json.Marshal(RememberedLogin{username, password})
	if err := keyring.Set(loginKeyringService, loginKeyringAccount(dir), string(data)); err != nil {
		return fmt.Errorf("Unable to save login in system credential storage")
	}
	return nil
}
func forgetRememberedLogin(dir string) error {
	err := keyring.Delete(loginKeyringService, loginKeyringAccount(dir))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("Unable to remove saved login from system credential storage")
	}
	return nil
}
func (a *App) RememberedLogin() (RememberedLogin, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.core != nil {
		reply, err := a.core.Call("GET", "/security", "")
		var status struct {
			Locked bool `json:"locked"`
		}
		if err != nil || reply.Status != 200 || json.Unmarshal(reply.Data, &status) != nil || status.Locked {
			return RememberedLogin{}, nil
		}
	}
	return readRememberedLogin(a.dir)
}
func (a *App) SaveRememberedLogin(username, password string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.core == nil {
		return fmt.Errorf("Sign in before saving a password")
	}
	// Authenticate through the existing desktop login path before saving.
	data, _ := json.Marshal(map[string]string{"username": username, "password": password})
	reply, err := a.core.Call("POST", "/login", string(data))
	if err != nil || reply.Status != 200 {
		return fmt.Errorf("Sign in before saving a password")
	}
	return writeRememberedLogin(a.dir, username, password)
}
func (a *App) ForgetRememberedLogin() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return forgetRememberedLogin(a.dir)
}
