package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zalando/go-keyring"
	"strings"
	"testing"
)

func TestRememberedLoginLifecycleAndPasswordChange(t *testing.T) {
	keyring.MockInit()
	a := &App{ctx: context.Background(), dir: t.TempDir()}
	a.open(a.dir)
	defer a.ServiceShutdown()
	if a.core == nil {
		t.Fatal("desktop failed to open")
	}
	password := "remembered-test-password"
	call := func(path string, data any) {
		body, _ := json.Marshal(data)
		reply, err := a.Call("POST", path, string(body))
		if err != nil || reply.Status != 200 {
			t.Fatalf("%s failed: %v status=%d", path, err, reply.Status)
		}
	}
	if err := a.SaveRememberedLogin("ssh-admin", password); err == nil {
		t.Fatal("saved unauthenticated password")
	}
	call("/desktop/setup", map[string]any{"password": password, "encryption": true})
	if err := a.SaveRememberedLogin("ssh-admin", password); err != nil {
		t.Fatal(err)
	}
	saved, err := a.RememberedLogin()
	if err != nil || saved.Password != password {
		t.Fatal("saved login missing")
	}
	if err = a.SaveRememberedLogin("ssh-admin", "wrong-password"); err == nil {
		t.Fatal("saved invalid login")
	}
	other, err := readRememberedLogin(t.TempDir())
	if err != nil || other.Password != "" {
		t.Fatal("credentials crossed data directories")
	}
	next := "changed-remembered-password"
	call("/desktop/password", map[string]string{"current": password, "password": next})
	saved, err = a.RememberedLogin()
	if err != nil || saved.Password != next {
		t.Fatal("saved password did not follow password change")
	}
	if err = a.ForgetRememberedLogin(); err != nil {
		t.Fatal(err)
	}
	saved, err = a.RememberedLogin()
	if err != nil || saved.Password != "" {
		t.Fatal("password not forgotten")
	}
}
func TestRememberedLoginStorageFailureDoesNotExposeSecret(t *testing.T) {
	keyring.MockInitWithError(errors.New("secret-from-provider"))
	defer keyring.MockInit()
	err := writeRememberedLogin(t.TempDir(), "test", "saved-test-password")
	if err == nil || strings.Contains(err.Error(), "secret-from-provider") || strings.Contains(err.Error(), "saved-test-password") {
		t.Fatal("unsafe storage error")
	}
}
