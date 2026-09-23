package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMasterPasswordEnvelope(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	password := "主密码测试-password"
	wrapped, err := wrapMasterKey(key, password)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wrapped, key) {
		t.Fatal("文件不应包含明文密钥")
	}
	got, err := unwrapMasterKey(wrapped, password)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("主密码未能还原密钥", err)
	}
	another, err := wrapMasterKey(key, password)
	if err != nil || bytes.Equal(wrapped, another) {
		t.Fatal("每次加密必须生成独立盐和 nonce", err)
	}
	if _, err := unwrapMasterKey(wrapped, "wrong-password"); err == nil {
		t.Fatal("错误密码被接受")
	}
	for _, offset := range []int{0, 8, 24, 36, len(wrapped) - 1} {
		damaged := bytes.Clone(wrapped)
		damaged[offset] ^= 1
		if _, err := unwrapMasterKey(damaged, password); err == nil {
			t.Fatalf("损坏位置 %d 被接受", offset)
		}
	}
	for _, input := range [][]byte{nil, key, wrapped[:20], append(bytes.Clone(wrapped), 0)} {
		if _, err := unwrapMasterKey(input, password); err == nil {
			t.Fatal("损坏或未知格式被接受")
		}
	}
	if _, err := wrapMasterKey(key, "short"); err == nil {
		t.Fatal("弱主密码被接受")
	}
}

func TestMasterPasswordStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := testInput(t)
	if _, err = s.Put(ctx, in); err != nil {
		t.Fatal(err)
	}
	before, err := s.get(ctx, "id", in.ID)
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.savedSecrets(before)
	if err != nil {
		t.Fatal(err)
	}
	rawKey, err := readSecret(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ChangeMasterPassword("", "first-master-password"); err != nil {
		t.Fatal(err)
	}
	encryptedKey, _ := readSecret(filepath.Join(dir, "master.key"))
	if bytes.Contains(encryptedKey, rawKey) || len(encryptedKey) != wrappedMasterKeySize {
		t.Fatal("主密钥未受保护")
	}
	after, _ := s.get(ctx, "id", in.ID)
	if !bytes.Equal(before.password, after.password) {
		t.Fatal("启用主密码不应重写目标凭证")
	}
	if err = s.ChangeMasterPassword("wrong-password", "second-master-password"); err == nil {
		t.Fatal("改密未验证旧密码")
	}
	unchanged, _ := readSecret(filepath.Join(dir, "master.key"))
	if !bytes.Equal(encryptedKey, unchanged) {
		t.Fatal("错误密码改变了密钥文件")
	}
	if err = s.ChangeMasterPassword("first-master-password", "second-master-password"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if state := s.MasterPasswordStatus(); !state.Enabled || !state.Locked {
		t.Fatal("重启后必须锁定", state)
	}
	if _, err = s.decrypt(before); !errors.Is(err, ErrMasterLocked) {
		t.Fatal("锁定后仍可解密", err)
	}
	if _, err = s.authenticate(ctx, in.RelayUser, []byte(in.RelayPassword), &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}); !errors.Is(err, ErrMasterLocked) {
		t.Fatal("锁定时中转认证应拒绝", err)
	}
	if err = s.UnlockMasterPassword("first-master-password"); err == nil {
		t.Fatal("旧主密码仍然有效")
	}
	if err = s.UnlockMasterPassword("second-master-password"); err != nil {
		t.Fatal(err)
	}
	got, err := s.savedSecrets(before)
	if err != nil || !reflect.DeepEqual(got, original) {
		t.Fatal("解锁后的目标凭证不一致", err)
	}
	if err = s.ChangeMasterPassword("second-master-password", ""); err != nil {
		t.Fatal(err)
	}
	restored, _ := readSecret(filepath.Join(dir, "master.key"))
	if !bytes.Equal(rawKey, restored) {
		t.Fatal("关闭保护未恢复原数据密钥")
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if state := reopened.MasterPasswordStatus(); state.Enabled || state.Locked {
		t.Fatal("关闭后仍要求解锁", state)
	}
	files, _ := filepath.Glob(filepath.Join(dir, ".master-key-*"))
	if len(files) != 0 {
		t.Fatal("遗留临时密钥文件")
	}
}

func TestMasterPasswordPrivateKeyAndConcurrentUnlock(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in := keyTarget(t, "私钥口令")
	if _, err = s.Put(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	old, _ := s.get(context.Background(), "id", in.ID)
	original, err := s.savedSecrets(old)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ChangeMasterPassword("", "private-master-password"); err != nil {
		t.Fatal(err)
	}
	dir := s.dir
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.UnlockMasterPassword("private-master-password"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	got, err := s.savedSecrets(old)
	if err != nil || !reflect.DeepEqual(got, original) {
		t.Fatal("私钥与私钥口令未保留", err)
	}
}

func TestMasterPasswordRejectsStaleWriterAndMissingKey(t *testing.T) {
	dir := t.TempDir()
	first, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err = first.ChangeMasterPassword("", "first-master-password"); err != nil {
		t.Fatal(err)
	}
	saved, _ := readSecret(filepath.Join(dir, "master.key"))
	if err = second.ChangeMasterPassword("", "stale-master-password"); err == nil {
		t.Fatal("过时进程覆盖了主密码")
	}
	got, _ := readSecret(filepath.Join(dir, "master.key"))
	if !bytes.Equal(saved, got) {
		t.Fatal("失败后密钥文件改变")
	}
	if err = os.Remove(filepath.Join(dir, "master.key")); err != nil {
		t.Fatal(err)
	}
	if reopened, err := OpenStore(dir); err == nil {
		reopened.Close()
		t.Fatal("丢失密钥时不能生成替代密钥")
	}
}

func TestMasterPasswordWebAuthorization(t *testing.T) {
	f := newWebFixture(t)
	body := map[string]string{"action": "enable", "admin_password": testAdminPassword, "password": "first-master-password"}
	requireStatus(t, f, "POST", "/api/security/master-password", body, 401)
	f.login(t)
	body["admin_password"] = "wrong-admin-password"
	requireStatus(t, f, "POST", "/api/security/master-password", body, 403)
	body["admin_password"] = testAdminPassword
	requireStatus(t, f, "POST", "/api/security/master-password", body, 200)
	body["action"], body["current"], body["password"] = "change", "wrong-master-password", "second-master-password"
	requireStatus(t, f, "POST", "/api/security/master-password", body, 400)
	body["current"] = "first-master-password"
	requireStatus(t, f, "POST", "/api/security/master-password", body, 200)
	addTestAccount(t, f.store, accountInput{Username: "vault-user", Enabled: true})
	userLogin(t, f, "vault-user")
	requireStatus(t, f, "POST", "/api/security/master-password", body, 403)
	f.login(t)
	body["action"], body["current"], body["password"] = "disable", "second-master-password", ""
	requireStatus(t, f, "POST", "/api/security/master-password", body, 200)
}

func TestMasterPasswordLockedWebAndRateLimit(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAdminPassword(context.Background(), testAdminPassword)
	if err = s.ChangeMasterPassword("", "first-master-password"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	web := NewWeb(context.Background(), s, testSigner(t), "127.0.0.1:2222")
	handler := web.Handler()
	call := func(path, body, remote, origin string) int {
		method := "POST"
		if body == "" {
			method = "GET"
		}
		r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
		r.RemoteAddr = remote
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	if code := call("/api/security", "", "127.0.0.1:10", ""); code != 200 {
		t.Fatal(code)
	}
	if code := call("/api/targets", "", "127.0.0.1:10", ""); code != 423 {
		t.Fatal("锁定接口仍可用", code)
	}
	if code := call("/api/login", `{"username":"ssh-admin","password":"test-admin-password-only"}`, "127.0.0.1:10", ""); code != 423 {
		t.Fatal("锁定时可登录", code)
	}
	if code := call("/api/unlock", `{"password":"first-master-password"}`, "192.0.2.1:10", ""); code != 403 {
		t.Fatal("允许通过远程 HTTP 传输主密码", code)
	}
	if code := call("/api/unlock", `{"password":"first-master-password"}`, "127.0.0.1:10", "https://evil.example"); code != 403 {
		t.Fatal("允许跨站解锁", code)
	}
	for i := 0; i < 10; i++ {
		if code := call("/api/unlock", `{"password":"wrong"}`, "127.0.0.1:10", ""); code != 401 {
			t.Fatal(code)
		}
	}
	if code := call("/api/unlock", `{"password":"first-master-password"}`, "127.0.0.1:10", ""); code != 429 {
		t.Fatal("未限制主密码重试", code)
	}
	if err = s.SetAdminPassword(context.Background(), "changed-admin-password"); err != nil {
		t.Fatal(err)
	}
	if !s.MasterPasswordStatus().Locked {
		t.Fatal("重置管理员密码解锁了凭证")
	}
	web.mu.Lock()
	web.attempts = map[string]loginAttempts{}
	web.mu.Unlock()
	if code := call("/api/unlock", `{"password":"first-master-password"}`, "127.0.0.1:10", ""); code != 200 {
		t.Fatal("解锁失败", code)
	}
	if code := call("/api/targets", "", "127.0.0.1:10", ""); code != 401 {
		t.Fatal("主密码代替了账号登录", code)
	}
}

func TestMasterPasswordDesktopSetupAndRestart(t *testing.T) {
	dir := t.TempDir()
	d, err := OpenDesktop(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	reply := desktopCall(t, d, "POST", "/desktop/setup", map[string]string{"password": testAdminPassword, "master_password": "desktop-master-password"})
	if reply.Status != 200 || !d.store.MasterPasswordStatus().Enabled {
		t.Fatal("首次设置未启用保护")
	}
	d.Close()
	d, err = OpenDesktop(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !d.store.MasterPasswordStatus().Locked || d.Info().Listen != "" {
		t.Fatal("桌面重启后未等待解锁")
	}
	reply = desktopCall(t, d, "POST", "/unlock", map[string]string{"password": "wrong-password"})
	if reply.Status != 401 || d.Info().Listen != "" {
		t.Fatal("错误主密码启动了监听")
	}
	reply = desktopCall(t, d, "POST", "/unlock", map[string]string{"password": "desktop-master-password"})
	if reply.Status != 200 || d.Info().Listen == "" {
		t.Fatal("解锁后未启动监听")
	}
	loginDesktop(t, d)
}

func TestMasterPasswordAutoMappingsWaitForUnlock(t *testing.T) {
	s, m, in := mappingStore(t, "")
	rule := testMapping(t, in.ID, "local", "loopback")
	rule.AutoStart = true
	rule.ServicePort = startMappingEcho(t, false)
	rule = saveTestMapping(t, m, rule)
	if err := s.ChangeMasterPassword("", "mapping-master-password"); err != nil {
		t.Fatal(err)
	}
	dir := s.dir
	s.Close()
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	next := reopened.mappingManager(context.Background())
	time.Sleep(100 * time.Millisecond)
	next.mu.Lock()
	count := len(next.runs)
	next.mu.Unlock()
	if count != 0 {
		t.Fatal("锁定时尝试自动启动映射")
	}
	if err = reopened.UnlockMasterPassword("mapping-master-password"); err != nil {
		t.Fatal(err)
	}
	awaitMapping(t, next, rule.ID, "running")
}
