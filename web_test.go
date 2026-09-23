package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const testAdminPassword = "test-admin-password-only"

type webFixture struct {
	*fixture
	web    *Web
	http   *httptest.Server
	cookie *http.Cookie
}

func newWebFixture(t *testing.T) *webFixture {
	t.Helper()
	f := newFixture(t)
	if err := f.store.SetAdminPassword(context.Background(), testAdminPassword); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	web := NewWeb(ctx, f.store, f.signer, f.address)
	server := httptest.NewServer(web.Handler())
	t.Cleanup(func() {
		cancel()
		server.Close()
		// WebSocket 劫持的连接不在 httptest.Server.Close 的等待范围内。
		deadline := time.Now().Add(5 * time.Second)
		for len(web.terminals) > 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if len(web.terminals) > 0 {
			t.Error("网页终端未完成退出清理")
		}
	})
	return &webFixture{fixture: f, web: web, http: server}
}

func (f *webFixture) request(t *testing.T, method, path string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	r, err := http.NewRequest(method, f.http.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if method != "GET" {
		r.Header.Set("Content-Type", "application/json")
	}
	if f.cookie != nil {
		r.AddCookie(f.cookie)
	}
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	response, err := f.http.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func (f *webFixture) login(t *testing.T) {
	t.Helper()
	response := f.request(t, "POST", "/api/login", map[string]string{"username": adminUsername, "password": testAdminPassword}, nil)
	if response.StatusCode != 200 || len(response.Cookies()) != 1 {
		t.Fatalf("登录失败：%d", response.StatusCode)
	}
	f.cookie = response.Cookies()[0]
	if !f.cookie.HttpOnly || f.cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("登录 Cookie 配置错误")
	}
}

func TestWebManagementAndAuthentication(t *testing.T) {
	f := newWebFixture(t)
	if got := f.request(t, "GET", "/api/targets", nil, nil).StatusCode; got != 401 {
		t.Fatalf("未登录访问列表应被拒绝：%d", got)
	}
	if got := f.request(t, "POST", "/api/login", map[string]string{"username": adminUsername, "password": "wrong"}, nil).StatusCode; got != 401 {
		t.Fatal("错误的管理员密码被接受")
	}
	f.login(t)
	response := f.request(t, "GET", "/api/targets", nil, nil)
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || bytes.Contains(data, []byte(`"target_password"`)) || bytes.Contains(data, []byte(f.input.TargetPassword)) {
		t.Fatal("列表失败或泄露密码")
	}
	if got := f.request(t, "POST", "/api/targets/test/test", map[string]bool{}, nil).StatusCode; got != 200 {
		t.Fatalf("连接测试失败：%d", got)
	}
	in := f.input
	in.ID, in.RelayUser, in.RelayPassword = "new", "new-user", ""
	response = f.request(t, "POST", "/api/targets", in, nil)
	var result map[string]string
	json.NewDecoder(response.Body).Decode(&result)
	if response.StatusCode != 200 || result["relay_password"] == "" {
		t.Fatal("创建目标未生成中转密码")
	}
	if got := f.request(t, "POST", "/api/targets", in, nil).StatusCode; got != 409 {
		t.Fatal("重复创建应返回冲突")
	}
	in.TargetPassword = ""
	in.Enabled = false
	in.Revision = 1
	if got := f.request(t, "PUT", "/api/targets/new", in, nil).StatusCode; got != 200 {
		t.Fatal("更新目标失败")
	}
	if got := f.request(t, "DELETE", "/api/targets/new", nil, nil).StatusCode; got != 200 {
		t.Fatal("删除目标失败")
	}
	response = f.request(t, "GET", "/api/events", nil, nil)
	data, _ = io.ReadAll(response.Body)
	if response.StatusCode != 200 || !bytes.Contains(data, []byte("目标已删除")) || bytes.Contains(data, []byte(testAdminPassword)) || bytes.Contains(data, []byte(f.input.TargetPassword)) {
		t.Fatal("日志缺失或包含秘密")
	}
	if got := f.request(t, "POST", "/api/logout", map[string]bool{}, nil).StatusCode; got != 200 {
		t.Fatal("退出失败")
	}
	if got := f.request(t, "GET", "/api/me", nil, nil).StatusCode; got != 401 {
		t.Fatal("退出后旧 Cookie 仍能访问")
	}
}

func TestWebCSRFSourceSpoofingAndPrivateMode(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	for _, headers := range []map[string]string{
		{"Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "cross-site"},
		{"Content-Type": "text/plain"},
	} {
		if status := f.request(t, "DELETE", "/api/targets/test", nil, headers).StatusCode; status != 403 && status != 415 {
			t.Fatalf("应拒绝跨站或非 JSON 请求：%d", status)
		}
	}
	f.input.SourceMode = "private"
	f.input.AllowedSources = []string{"0.0.0.0/0"}
	if got := f.request(t, "PUT", "/api/targets/test", f.input, nil).StatusCode; got != 400 {
		t.Fatal("内网模式不应接受公网范围")
	}
	f.input.SourceMode = "custom"
	f.input.AllowedSources = []string{"192.0.2.1"}
	if _, err := f.store.Put(context.Background(), f.input); err != nil {
		t.Fatal(err)
	}
	response := f.request(t, "GET", "/api/targets/test/terminal", nil, map[string]string{"X-Forwarded-For": "192.0.2.1", "X-Real-IP": "192.0.2.1"})
	if response.StatusCode != 403 {
		t.Fatalf("伪造来源请求头绕过了白名单：%d", response.StatusCode)
	}
}

func (f *webFixture) terminal(t *testing.T) (*websocket.Conn, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	header := http.Header{}
	header.Set("Cookie", f.cookie.String())
	header.Set("Origin", f.http.URL)
	ws, _, err := websocket.Dial(ctx, strings.Replace(f.http.URL, "http:", "ws:", 1)+"/api/targets/test/terminal", &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	for {
		kind, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.MessageText && bytes.Contains(data, []byte(`"type":"ready"`)) {
			return ws, ctx
		}
	}
}

func TestWebSSHTerminal(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	ws, ctx := f.terminal(t)
	for _, tc := range []struct {
		input terminalInput
		want  string
	}{
		{terminalInput{Type: "resize", Columns: 120, Rows: 40}, "120x40\n"},
		{terminalInput{Type: "input", Data: "hello-web\n"}, "hello-web\n"},
	} {
		data, _ := json.Marshal(tc.input)
		if err := ws.Write(ctx, websocket.MessageText, data); err != nil {
			t.Fatal(err)
		}
		var output strings.Builder
		for !strings.Contains(output.String(), tc.want) {
			kind, data, err := ws.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if kind == websocket.MessageBinary {
				output.Write(data)
			}
		}
	}
	if got := f.request(t, "POST", "/api/logout", map[string]bool{}, nil).StatusCode; got != 200 {
		t.Fatal("退出失败")
	}
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for {
		if _, _, err := ws.Read(waitCtx); err != nil {
			if waitCtx.Err() != nil {
				t.Fatal("退出后 WebSSH 未被关闭")
			}
			break
		}
	}
}

func TestWebSSHRevocationAndAdminReset(t *testing.T) {
	for _, change := range []string{"修改目标 IP", "重置管理员密码"} {
		t.Run(change, func(t *testing.T) {
			f := newWebFixture(t)
			f.login(t)
			ws, ctx := f.terminal(t)
			if change == "修改目标 IP" {
				f.input.AllowedSources = []string{"192.0.2.1"}
				if _, err := f.store.Put(ctx, f.input); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := f.store.SetAdminPassword(ctx, "changed-admin-password"); err != nil {
					t.Fatal(err)
				}
				if got := f.request(t, "GET", "/api/me", nil, nil).StatusCode; got != 401 {
					t.Fatal("重置密码后旧登录仍然有效")
				}
			}
			waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			for {
				if _, _, err := ws.Read(waitCtx); err != nil {
					if waitCtx.Err() != nil {
						t.Fatal("配置改变后网页终端未关闭")
					}
					break
				}
			}
		})
	}
}

func TestLoginRateLimitAndInitialAdmin(t *testing.T) {
	f := newWebFixture(t)
	for i := 0; i < 10; i++ {
		if got := f.request(t, "POST", "/api/login", map[string]string{"username": adminUsername, "password": "wrong"}, nil).StatusCode; got != 401 {
			t.Fatalf("错误的登录响应：%d", got)
		}
	}
	if got := f.request(t, "POST", "/api/login", map[string]string{"username": adminUsername, "password": testAdminPassword}, nil).StatusCode; got != 429 {
		t.Fatal("登录尝试未被限流")
	}
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	password, err := store.EnsureAdmin(context.Background())
	if err != nil || len(password) != 32 {
		t.Fatal("首次启动未生成管理员密码")
	}
	if password, err := store.EnsureAdmin(context.Background()); err != nil || password != "" {
		t.Fatal("已有管理员时不应再生成或展示密码")
	}
}

func TestWebPublicOrigin(t *testing.T) {
	f := newWebFixture(t)
	for _, invalid := range []string{"ftp://ssh.example", "https://user@ssh.example", "https://ssh.example/path", "https://ssh.example?query", "https://ssh.example#fragment", "https:///"} {
		if err := f.web.SetPublicOrigin(invalid); err == nil {
			t.Fatalf("不应接受外部来源 %q", invalid)
		}
	}
	const origin = "https://ssh.example:28081"
	if err := f.web.SetPublicOrigin(origin); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, host, origin string
		status             int
		secure             bool
	}{
		{"代理 HTTPS 登录", "ssh.example:28081", origin, 200, true},
		{"拒绝其他站点", "ssh.example:28081", "https://evil.example", 403, false},
		{"拒绝错误端口", "ssh.example:28081", "https://ssh.example", 403, false},
		{"拒绝协议降级", "ssh.example:28081", "http://ssh.example:28081", 403, false},
		{"拒绝来源与主机不匹配", "127.0.0.1:3311", origin, 403, false},
		{"本机访问保持同源", "127.0.0.1:3311", "http://127.0.0.1:3311", 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://"+tc.host+"/api/login", strings.NewReader(`{"username":"`+adminUsername+`","password":"`+testAdminPassword+`"}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("X-Forwarded-Host", "evil.example")
			w := httptest.NewRecorder()
			f.web.Handler().ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("状态 %d，预期 %d", w.Code, tc.status)
			}
			if tc.status == 200 {
				cookies := w.Result().Cookies()
				if len(cookies) != 1 || cookies[0].Secure != tc.secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
					t.Fatal("登录 Cookie 的代理 HTTPS 属性不正确")
				}
			}
		})
	}
}
