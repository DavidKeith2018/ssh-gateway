package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testUserPassword = "ordinary-user-password"

func addTestAccount(t *testing.T, s *Store, in accountInput) string {
	t.Helper()
	if in.Password == "" {
		in.Password = testUserPassword
	}
	id, err := s.saveAccount(context.Background(), "", in)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func userLogin(t *testing.T, f *webFixture, name string) *http.Cookie {
	t.Helper()
	f.cookie = nil
	res := f.request(t, "POST", "/api/login", map[string]string{"username": name, "password": testUserPassword}, nil)
	if res.StatusCode != 200 {
		t.Fatal("用户登录失败", res.StatusCode)
	}
	f.cookie = res.Cookies()[0]
	return f.cookie
}
func requireStatus(t *testing.T, f *webFixture, method, path string, body any, status int) {
	t.Helper()
	res := f.request(t, method, path, body, nil)
	if res.StatusCode != status {
		data, _ := io.ReadAll(res.Body)
		t.Fatalf("%s %s：%d，预期 %d：%s", method, path, res.StatusCode, status, data)
	}
}
func TestAccountAPIAndMachineIsolation(t *testing.T) {
	f := newWebFixture(t)
	ctx := context.Background()
	f.login(t)
	admin := f.cookie
	in := accountInput{Username: "alice", Password: testUserPassword, Enabled: true}
	requireStatus(t, f, "POST", "/api/users", in, 200)
	requireStatus(t, f, "POST", "/api/users", in, 409)
	res := f.request(t, "GET", "/api/users", nil, nil)
	data, _ := io.ReadAll(res.Body)
	if strings.Contains(string(data), "password") || strings.Contains(string(data), testUserPassword) {
		t.Fatal("泄露密码")
	}
	var users pageResult[Account]
	if err := json.Unmarshal(data, &users); err != nil || len(users.Items) != 1 {
		t.Fatal(string(data))
	}
	id := users.Items[0].ID
	alice := userLogin(t, f, "alice")
	res = f.request(t, "GET", "/api/targets", nil, nil)
	var targets pageResult[Target]
	json.NewDecoder(res.Body).Decode(&targets)
	if len(targets.Items) != 0 {
		t.Fatal("新帐号获得了机器")
	}
	for _, path := range []string{"/api/users", "/api/global-ips", "/api/events", "/api/mappings"} {
		requireStatus(t, f, "GET", path, nil, 403)
	}
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/targets"}, {"PUT", "/api/targets/test"}, {"DELETE", "/api/targets/test"}, {"POST", "/api/probe"}, {"POST", "/api/targets/test/test"}, {"POST", "/api/users"}, {"PUT", "/api/users/" + id}, {"DELETE", "/api/users/" + id}, {"PUT", "/api/global-ips"}, {"DELETE", "/api/global-ips"}, {"POST", "/api/mappings"}, {"PUT", "/api/mappings/test"}, {"DELETE", "/api/mappings/test"}, {"POST", "/api/mappings/test/start"}, {"POST", "/api/mappings/test/stop"},
		{"GET", "/api/targets/test/terminal"}, {"POST", "/api/targets/test/notes"}, {"POST", "/api/targets/test/files"}, {"GET", "/api/targets/test/resources"}, {"GET", "/api/targets/test/hardware"}, {"GET", "/api/targets/test/transfer"}, {"PUT", "/api/targets/test/transfer"},
	} {
		requireStatus(t, f, tc.method, tc.path, map[string]string{}, 403)
	}
	f.cookie = admin
	requireStatus(t, f, "GET", "/api/me", nil, 200)
	in.TargetIDs = []string{"test"}
	in.Password = ""
	requireStatus(t, f, "PUT", "/api/users/"+id, in, 200)
	f.cookie = alice
	requireStatus(t, f, "POST", "/api/targets/test/notes", map[string]string{"op": "read"}, 200)
	res = f.request(t, "GET", "/api/targets", nil, nil)
	json.NewDecoder(res.Body).Decode(&targets)
	if len(targets.Items) != 1 {
		t.Fatal("已授权机器不可见")
	}
	var me struct {
		Username string `json:"username"`
		Admin    bool   `json:"is_admin"`
	}
	res = f.request(t, "GET", "/api/me", nil, nil)
	json.NewDecoder(res.Body).Decode(&me)
	if me.Username != "alice" || me.Admin {
		t.Fatal("身份错误")
	}
	if err := f.store.Delete(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Put(ctx, f.input); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f, "POST", "/api/targets/test/notes", map[string]string{"op": "read"}, 403)
	f.cookie = admin
	in.Username = "renamed"
	requireStatus(t, f, "PUT", "/api/users/"+id, in, 400)
	requireStatus(t, f, "DELETE", "/api/users/"+id, nil, 200)
	f.cookie = alice
	requireStatus(t, f, "GET", "/api/me", nil, 401)
}
func TestAccountTagsAndSessionRevocation(t *testing.T) {
	f := newWebFixture(t)
	ctx := context.Background()
	in := accountInput{Username: "alice", Enabled: true, Tags: []string{"开发"}}
	id := addTestAccount(t, f.store, in)
	addTestAccount(t, f.store, accountInput{Username: "bob", Enabled: true})
	alice := userLogin(t, f, "alice")
	bob := userLogin(t, f, "bob")
	f.cookie = alice
	requireStatus(t, f, "GET", "/api/me", nil, 200)
	if f.web.canAccess(f.webRequest(alice), "test") {
		t.Fatal("标签未匹配仍允许访问")
	}
	target := f.input
	target.Tags = []string{" 开发 ", "开发", "中文"}
	if _, err := f.store.Put(ctx, target); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f, "POST", "/api/targets/test/notes", map[string]string{"op": "read"}, 200)
	target.Tags = nil
	f.store.Put(ctx, target)
	requireStatus(t, f, "POST", "/api/targets/test/notes", map[string]string{"op": "read"}, 403)
	in.TargetIDs = []string{"test"}
	if _, err := f.store.saveAccount(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f, "POST", "/api/targets/test/notes", map[string]string{"op": "read"}, 200)
	for _, change := range []string{"禁用再启用", "重置密码"} {
		f.cookie = alice
		if change == "禁用再启用" {
			in.Enabled = false
			f.store.saveAccount(ctx, id, in)
			in.Enabled = true
			f.store.saveAccount(ctx, id, in)
		} else {
			in.Password = "reset-user-password"
			f.store.saveAccount(ctx, id, in)
		}
		requireStatus(t, f, "GET", "/api/me", nil, 401)
		f.cookie = bob
		requireStatus(t, f, "GET", "/api/me", nil, 200)
		if change == "禁用再启用" {
			alice = userLogin(t, f, "alice")
		}
	}
}
func (f *webFixture) webRequest(cookie *http.Cookie) *http.Request {
	r, _ := http.NewRequest("GET", f.http.URL+"/api/me", nil)
	r.AddCookie(cookie)
	return r
}
func TestAccountActiveTerminalRevocation(t *testing.T) {
	for _, change := range []string{"直接撤权", "标签撤权", "重置密码", "禁用帐号", "删除帐号", "保存笔记"} {
		t.Run(change, func(t *testing.T) {
			f := newWebFixture(t)
			in := accountInput{Username: "alice", Enabled: true, TargetIDs: []string{"test"}}
			if change == "标签撤权" {
				in.TargetIDs = nil
				in.Tags = []string{"开发"}
				f.input.Tags = []string{"开发"}
				f.store.Put(context.Background(), f.input)
			}
			id := addTestAccount(t, f.store, in)
			userLogin(t, f, "alice")
			ws, ctx := f.terminal(t)
			switch change {
			case "直接撤权":
				in.TargetIDs = nil
			case "标签撤权":
				in.Tags = nil
			case "重置密码":
				in.Password = "reset-user-password"
			case "禁用帐号":
				in.Enabled = false
			case "删除帐号":
				f.store.db.Exec(`DELETE FROM users WHERE id=?`, id)
			case "保存笔记":
				requireStatus(t, f, "POST", "/api/targets/test/notes", map[string]any{"op": "write", "content": "连接中的笔记", "overwrite": true}, 200)
				time.Sleep(1200 * time.Millisecond)
				if len(f.web.terminals) != 1 {
					t.Fatal("笔记保存断开了终端")
				}
				return
			}
			if change != "删除帐号" {
				if _, err := f.store.saveAccount(ctx, id, in); err != nil {
					t.Fatal(err)
				}
			}
			wait, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			for {
				if _, _, err := ws.Read(wait); err != nil {
					if wait.Err() != nil {
						t.Fatal("撤权未关闭终端")
					}
					break
				}
			}
		})
	}
}
func TestDesktopAccountAuthorization(t *testing.T) {
	d := freshDesktop(t)
	loginDesktop(t, d)
	f := newFixture(t)
	ctx := context.Background()
	d.store.Put(ctx, f.input)
	in := accountInput{Username: "alice", Enabled: true}
	id := addTestAccount(t, d.store, in)
	if reply := desktopCall(t, d, "POST", "/login", map[string]string{"username": "alice", "password": testUserPassword}); reply.Status != 200 {
		t.Fatal(string(reply.Data))
	}
	if err := d.SaveSettings(d.settings, true); err == nil {
		t.Fatal("用户可管理桌面设置")
	}
	if reply := desktopCall(t, d, "POST", "/desktop/password", map[string]string{"current": testAdminPassword, "password": "new-admin-password"}); reply.Status < 400 {
		t.Fatal("用户可重置管理员")
	}
	if err := d.OpenTerminal("denied", "test", func(TerminalEvent) {}); err == nil {
		t.Fatal("原生终端越权")
	}
	if _, err := d.MachineWindowURL("test"); err == nil {
		t.Fatal("窗口入口越权")
	}
	if reply, err := d.MachineCall(ctx, "test", "notes", `{"op":"read"}`); err != nil || reply.Status != 403 {
		t.Fatal("原生笔记越权", err)
	}
	in.TargetIDs = []string{"test"}
	d.store.saveAccount(ctx, id, in)
	if err := d.OpenTerminal("allowed", "test", func(TerminalEvent) {}); err != nil {
		t.Fatal(err)
	}
	entry, err := d.MachineWindowURL("test")
	if err != nil {
		t.Fatal(err)
	}
	in.TargetIDs = nil
	d.store.saveAccount(ctx, id, in)
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(entry)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode < 400 {
		t.Fatal("已撤权窗口票据仍有效")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		d.terminalMu.Lock()
		n := len(d.terminals)
		d.terminalMu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("桌面终端撤权后未关闭")
}

func TestAccountMachineOperationCancellation(t *testing.T) {
	f := newWebFixture(t)
	in := accountInput{Username: "alice", Enabled: true, TargetIDs: []string{"test"}}
	id := addTestAccount(t, f.store, in)
	cookie := userLogin(t, f, "alice")
	r := f.webRequest(cookie)
	r.RemoteAddr = "127.0.0.1:1234"
	r.SetPathValue("id", "test")
	client, ctx, cleanup, err := f.web.machineClient(r, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	closed := make(chan error, 1)
	go func() { closed <- client.Wait() }()
	in.TargetIDs = nil
	if _, err = f.store.saveAccount(context.Background(), id, in); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("机器操作撤权后未取消")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("机器操作底层 SSH 未关闭")
	}
	if _, _, release, err := f.web.machineClient(r, time.Second); err == nil {
		release()
		t.Fatal("撤权后重新连接")
	}
}

func TestAccountValidationAndAtomicGrants(t *testing.T) {
	f := newWebFixture(t)
	ctx := context.Background()
	for _, in := range []accountInput{
		{Username: "admin", Password: testUserPassword},
		{Username: adminUsername, Password: testUserPassword},
		{Username: "SSH-ADMIN", Password: testUserPassword},
		{Username: "bad/name", Password: testUserPassword},
		{Username: "short", Password: "short"},
		{Username: "long", Password: strings.Repeat("密", 25)},
		{Username: "missing", Password: testUserPassword, TargetIDs: []string{"missing"}},
		{Username: "badtag", Password: testUserPassword, Tags: []string{"a\nb"}},
	} {
		if _, err := f.store.saveAccount(ctx, "", in); err == nil {
			t.Fatal("接受无效帐号", in.Username)
		}
	}
	in := accountInput{Username: "alice", Enabled: true, TargetIDs: []string{"test"}}
	id := addTestAccount(t, f.store, in)
	in.TargetIDs = []string{"missing"}
	if _, err := f.store.saveAccount(ctx, id, in); err == nil {
		t.Fatal("接受不存在机器")
	}
	if !f.store.accountAccess(ctx, id, "test") {
		t.Fatal("失败的保存破坏原授权")
	}
	in.TargetIDs = nil
	in.Tags = []string{"开发"}
	if _, err := f.store.saveAccount(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	next := f.input
	next.ID = "new"
	next.RelayUser = "new"
	next.Tags = []string{"开发"}
	if _, err := f.store.Put(ctx, next); err != nil {
		t.Fatal(err)
	}
	if !f.store.accountAccess(ctx, id, "new") {
		t.Fatal("标签未动态包含新机器")
	}
	// 普通 SSH 继续使用机器独立中转凭证。
	connection, err := f.dial(f.input.RelayUser, f.input.RelayPassword)
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
}

func TestAdministratorRequiresPrefixedUsername(t *testing.T) {
	f := newWebFixture(t)
	for _, username := range []string{"", "admin"} {
		requireStatus(t, f, "POST", "/api/login", map[string]string{"username": username, "password": testAdminPassword}, 401)
	}
	f.login(t)
	res := f.request(t, "GET", "/api/me", nil, nil)
	var me struct {
		Username string `json:"username"`
		IsAdmin  bool   `json:"is_admin"`
	}
	if err := json.NewDecoder(res.Body).Decode(&me); err != nil {
		t.Fatal(err)
	}
	if me.Username != adminUsername || !me.IsAdmin {
		t.Fatalf("管理员身份错误：%+v", me)
	}
}
