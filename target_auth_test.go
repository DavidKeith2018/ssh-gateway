package gateway

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testPrivateKey(t *testing.T, passphrase string) (string, ssh.PublicKey) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(key, "测试私钥")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(key, "测试私钥", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block)), signer.PublicKey()
}

func keyTarget(t *testing.T, passphrase string) PutInput {
	t.Helper()
	key, public := testPrivateKey(t, passphrase)
	address, hostKey, _ := startBackend(t, public)
	in := testInput(t)
	in.Host, _, _ = net.SplitHostPort(address.String())
	_, port, _ := net.SplitHostPort(address.String())
	in.Port, _ = strconv.Atoi(port)
	in.HostFingerprint = ssh.FingerprintSHA256(hostKey.PublicKey())
	in.AuthType, in.TargetPassword = "private_key", ""
	in.TargetPrivateKey, in.TargetKeyPassphrase = key, passphrase
	return in
}

func TestPrivateKeyConnections(t *testing.T) {
	for _, passphrase := range []string{"", "测试私钥密码-secret"} {
		t.Run(map[bool]string{true: "加密私钥", false: "无密码私钥"}[passphrase != ""], func(t *testing.T) {
			f := newFixture(t)
			f.input = keyTarget(t, passphrase)
			ctx := context.Background()
			if _, err := f.store.Put(ctx, f.input); err != nil {
				t.Fatal(err)
			}
			if err := f.store.CheckTarget(ctx, f.input.ID); err != nil {
				t.Fatal(err)
			}
			client := f.connect(t)
			session, err := client.NewSession()
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			session.Stdin = strings.NewReader("密钥中转成功\n")
			out, err := session.Output("cat")
			if err != nil || string(out) != "密钥中转成功\n" {
				t.Fatalf("私钥中转失败：%v，%q", err, out)
			}
			bad := f.input
			bad.HostFingerprint = ssh.FingerprintSHA256(testSigner(t).PublicKey())
			if _, err := f.store.Put(ctx, bad); err != nil {
				t.Fatal(err)
			}
			if err := f.store.CheckTarget(ctx, bad.ID); err == nil {
				t.Fatal("私钥认证绕过了指纹检查")
			}
			bad = f.input
			bad.TargetPrivateKey, _ = testPrivateKey(t, passphrase)
			if _, err := f.store.Put(ctx, bad); err != nil {
				t.Fatal(err)
			}
			if err := f.store.CheckTarget(ctx, bad.ID); err == nil {
				t.Fatal("目标接受了未授权的私钥")
			}
		})
	}
}

func TestPrivateKeyPersistenceAndUpdates(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := keyTarget(t, "持久化私钥口令-secret")
	if _, err := s.Put(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{in.TargetPrivateKey, strings.Split(in.TargetPrivateKey, "\n")[1], in.TargetKeyPassphrase} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("数据库泄露明文认证材料")
		}
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CheckTarget(ctx, in.ID); err != nil {
		t.Fatalf("重启后密钥连接失败：%v", err)
	}
	r, err := s.get(ctx, "id", in.ID)
	if err != nil {
		t.Fatal(err)
	}
	in.TargetPrivateKey, in.TargetKeyPassphrase, in.RelayPassword = "", "", ""
	in.AuthType = "" // 老客户端编辑时未传认证方式也应保留私钥。
	in.Name = "只更新名称"
	if _, err := s.Put(ctx, in); err != nil {
		t.Fatal(err)
	}
	r2, err := s.get(ctx, "id", in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r2.AuthType != "private_key" || !bytes.Equal(r.password, r2.password) {
		t.Fatal("编辑时未保留私钥")
	}
	if err := s.CheckTarget(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	bad := in
	bad.TargetKeyPassphrase = "不能单独替换口令"
	if _, err := s.Put(ctx, bad); err == nil {
		t.Fatal("只填写口令时不应替换或清空已存私钥")
	}
	r2.AuthType = "password"
	if _, err := s.targetAuth(r2); err == nil {
		t.Fatal("认证方式篡改未被拒绝")
	}
	r2 = r
	r2.password = bytes.Clone(r.password)
	r2.password[len(r2.password)-1] ^= 1
	if _, err := s.targetAuth(r2); err == nil {
		t.Fatal("密文篡改未被拒绝")
	}
	replacement := keyTarget(t, "")
	if _, err := s.Put(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckTarget(ctx, replacement.ID); err != nil {
		t.Fatalf("换成无密码私钥时未清除旧口令：%v", err)
	}
	in.AuthType = "password"
	if _, err := s.Put(ctx, in); err == nil {
		t.Fatal("切换认证方式必须提供密码")
	}
	in.TargetPassword = "replacement-password"
	if _, err := s.Put(ctx, in); err != nil {
		t.Fatal(err)
	}
	r2, err = s.get(ctx, "id", in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.decrypt(r2); err != nil || got != in.TargetPassword {
		t.Fatal("切换密码失败或保留了旧私钥")
	}
	// 模拟旧版本没有 auth_type 的配置，确认无需迁移即可读取和更新。
	if _, err := s.db.Exec(`UPDATE targets SET config=json_remove(config, '$.auth_type') WHERE id=?`, in.ID); err != nil {
		t.Fatal(err)
	}
	r2, err = s.get(ctx, "id", in.ID)
	if err != nil || r2.AuthType != "password" {
		t.Fatal("旧配置读取失败")
	}
	if _, err := s.targetAuth(r2); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateKeyValidationAndAPI(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	in := keyTarget(t, "接口私钥口令-secret")
	in.ID, in.RelayUser = "key-api", "key-api-relay"
	for _, tc := range []struct {
		name   string
		mutate func(*PutInput)
	}{
		{"错误口令", func(i *PutInput) { i.TargetKeyPassphrase = "wrong-secret" }},
		{"缺少口令", func(i *PutInput) { i.TargetKeyPassphrase = "" }},
		{"无效私钥", func(i *PutInput) { i.TargetPrivateKey = "invalid-private-secret" }},
		{"缺少私钥", func(i *PutInput) { i.TargetPrivateKey = "" }},
		{"私钥过大", func(i *PutInput) { i.TargetPrivateKey = strings.Repeat("x", maxPrivateKeySize+1) }},
		{"混合凭证", func(i *PutInput) { i.TargetPassword = "password-secret" }},
		{"非法认证方式", func(i *PutInput) { i.AuthType = "unknown" }},
		{"口令过长", func(i *PutInput) { i.TargetKeyPassphrase = strings.Repeat("x", 4097) }},
		{"未加密私钥填写口令", func(i *PutInput) { i.TargetPrivateKey, _ = testPrivateKey(t, "") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := in
			tc.mutate(&bad)
			response := f.request(t, "POST", "/api/targets", bad, nil)
			body, _ := io.ReadAll(response.Body)
			if response.StatusCode != 400 {
				t.Fatalf("应拒绝无效配置：%d", response.StatusCode)
			}
			if bytes.Contains(body, []byte("secret")) {
				t.Fatal("错误信息泄露认证材料")
			}
		})
	}
	if response := f.request(t, "POST", "/api/targets", in, nil); response.StatusCode != 200 {
		t.Fatalf("私钥保存失败：%d", response.StatusCode)
	}
	response := f.request(t, "GET", "/api/targets", nil, nil)
	var targets pageResult[map[string]any]
	if err := json.NewDecoder(response.Body).Decode(&targets); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, target := range targets.Items {
		for _, name := range []string{"target_private_key", "target_key_passphrase", "target_password", "relay_password"} {
			if _, ok := target[name]; ok {
				t.Fatal("接口回显认证材料")
			}
		}
		if target["id"] == in.ID {
			found = target["auth_type"] == "private_key"
		}
	}
	if !found {
		t.Fatal("列表未返回认证方式")
	}
	if response := f.request(t, "POST", "/api/targets/"+in.ID+"/test", map[string]bool{}, nil); response.StatusCode != 200 {
		t.Fatalf("私钥连接测试失败：%d", response.StatusCode)
	}
}
