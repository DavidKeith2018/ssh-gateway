package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBrowserSessionThirtyDayLifetime(t *testing.T) {
	f := newWebFixture(t)
	before := time.Now()
	f.login(t)
	after := time.Now()
	if f.cookie.MaxAge != 30*24*60*60 {
		t.Fatalf("Cookie lifetime = %d seconds, want 30 days", f.cookie.MaxAge)
	}
	f.web.mu.Lock()
	session := f.web.sessions[f.cookie.Value]
	f.web.mu.Unlock()
	if session.expires.Before(before.Add(30*24*time.Hour)) || session.expires.After(after.Add(30*24*time.Hour)) {
		t.Fatal("Server session must expire 30 days after login")
	}
	for _, elapsed := range []time.Duration{29 * 24 * time.Hour, 30 * 24 * time.Hour} {
		aged := session
		aged.expires = aged.expires.Add(-elapsed)
		f.web.mu.Lock()
		f.web.sessions[f.cookie.Value] = aged
		f.web.mu.Unlock()
		want := http.StatusOK
		if elapsed == 30*24*time.Hour {
			want = http.StatusUnauthorized
		}
		if got := f.request(t, "GET", "/api/me", nil, nil).StatusCode; got != want {
			t.Fatalf("After %v: status = %d, want %d", elapsed, got, want)
		}
	}
	f.web.mu.Lock()
	_, exists := f.web.sessions[f.cookie.Value]
	f.web.mu.Unlock()
	if exists {
		t.Fatal("Expired session was not removed")
	}
}

func TestCanceledWindowRequestKeepsLogin(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	request := httptest.NewRequest("GET", "/api/me", nil)
	request.AddCookie(f.cookie)
	for _, deadline := range []bool{false, true} {
		var ctx context.Context
		var cancel context.CancelFunc
		if deadline {
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		} else {
			ctx, cancel = context.WithCancel(context.Background())
			cancel()
		}
		if f.web.validSession(request.WithContext(ctx)) {
			t.Fatal("已取消的认证请求不应通过")
		}
		cancel()
		if !f.web.validSession(request) {
			t.Fatal("关闭窗口取消请求不应注销其他窗口")
		}
	}
	if err := f.store.SetAdminPassword(context.Background(), "new-admin-password-only"); err != nil {
		t.Fatal(err)
	}
	if f.web.validSession(request) {
		t.Fatal("修改密码后原会话必须失效")
	}
}
