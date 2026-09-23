package gateway

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func testInput(t *testing.T) PutInput {
	t.Helper()
	return PutInput{
		Target: Target{
			ID: "test", Name: "测试服务器", Host: "127.0.0.1", Port: 22, User: "target-user",
			HostFingerprint: ssh.FingerprintSHA256(testSigner(t).PublicKey()),
			RelayUser:       "relay-test", AllowedSources: []string{"127.0.0.1"}, Enabled: true,
		},
		TargetPassword: "target-secret-value", RelayPassword: "relay-secret-value",
	}
}

func TestStorePersistenceAndSecrets(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := testInput(t)
	in.RelayPassword = ""
	password, err := store.Put(ctx, in)
	if err != nil || len(password) != 32 {
		t.Fatalf("生成中转密码失败：%v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(in.TargetPassword)) {
		t.Fatal("数据库中出现原始目标密码")
	}
	if bytes.Contains(data, []byte(password)) {
		t.Fatal("数据库不应包含明文中转密码")
	}
	store, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 4000}
	r, err := store.authenticate(ctx, in.RelayUser, []byte(password), addr)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := store.decrypt(r)
	if err != nil || decrypted != in.TargetPassword {
		t.Fatalf("重启后无法恢复目标密码：%v", err)
	}
	items, err := store.List(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("列表失败：%v", err)
	}
	public, _ := json.Marshal(items)
	if bytes.Contains(public, []byte(`"target_password"`)) || bytes.Contains(public, []byte(password)) {
		t.Fatal("列表不应含有密码")
	}
	in.TargetPassword = ""
	in.Name = "更新名称"
	if generated, err := store.Put(ctx, in); err != nil || generated != "" {
		t.Fatalf("更新失败：%v", err)
	}
	r2, err := store.authenticate(ctx, in.RelayUser, []byte(password), addr)
	if err != nil || r2.Revision != r.Revision+1 {
		t.Fatalf("更新不应重置密码：%v", err)
	}
	if got, err := store.decrypt(r2); err != nil || got != decrypted {
		t.Fatal("空密码更新未保留原目标密码")
	}
	r2.password[len(r2.password)-1] ^= 1
	if _, err := store.decrypt(r2); err == nil {
		t.Fatal("应拒绝被篡改的目标密码密文")
	}
	for _, name := range []string{"gateway.db", "master.key"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
			t.Fatalf("私密文件权限错误：%s", name)
		}
	}
	if err := store.Delete(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, in.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("删除后目标仍存在")
	}
}

func TestStoreValidationAndAuthentication(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	in := testInput(t)
	if _, err := store.Put(ctx, in); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ user, password, ip string }{
		{in.RelayUser, "错误密码", "127.0.0.1"},
		{"unknown", in.RelayPassword, "127.0.0.1"},
		{in.RelayUser, in.RelayPassword, "192.168.1.1"},
	} {
		if _, err := store.authenticate(ctx, tc.user, []byte(tc.password), &net.TCPAddr{IP: net.ParseIP(tc.ip)}); !errors.Is(err, ErrDenied) {
			t.Fatalf("应拒绝认证：%+v", tc)
		}
	}
	duplicate := in
	duplicate.ID = "other"
	if _, err := store.Put(ctx, duplicate); err == nil {
		t.Fatal("不应允许重复的中转用户名")
	}
	for _, mutate := range []func(*PutInput){
		func(i *PutInput) { i.AllowedSources = nil },
		func(i *PutInput) { i.AllowedSources = []string{"错误"} },
		func(i *PutInput) { i.RelayPassword = "短密码" },
		func(i *PutInput) { i.HostFingerprint = "" },
		func(i *PutInput) { i.Port = -1 },
	} {
		bad := in
		mutate(&bad)
		if _, err := store.Put(ctx, bad); err == nil {
			t.Fatal("不应接受无效目标配置")
		}
	}
	in.Enabled = false
	if _, err := store.Put(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := store.authenticate(ctx, in.RelayUser, []byte(in.RelayPassword), &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}); !errors.Is(err, ErrDenied) {
		t.Fatal("禁用后仍可认证")
	}
}

func TestMissingMasterKeyAndPersistentHostKey(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err := os.Remove(filepath.Join(dir, "master.key")); err != nil {
		t.Fatal(err)
	}
	if s, err := OpenStore(dir); err == nil {
		s.Close()
		t.Fatal("已有数据库时不应重新生成解密密钥")
	}
	first, err := LoadHostKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadHostKey(dir)
	if err != nil || !bytes.Equal(first.PublicKey().Marshal(), second.PublicKey().Marshal()) {
		t.Fatal("重启后主机密钥发生变化")
	}
}
