package gateway

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

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
