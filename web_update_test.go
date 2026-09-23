package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"ssh-gateway/updater"
)

type updateTransport func(*http.Request) (*http.Response, error)

func (f updateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWebProgramVersionAndUpdate(t *testing.T) {
	f := newWebFixture(t)
	response := f.request(t, "GET", "/api/version", nil, nil)
	var version map[string]string
	if err := json.NewDecoder(response.Body).Decode(&version); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || version["version"] != updater.Version {
		t.Fatalf("版本响应错误：%v", version)
	}
	if got := f.request(t, "GET", "/api/update", nil, nil).StatusCode; got != 401 {
		t.Fatalf("未登录不应触发更新检查：%d", got)
	}
	f.web.updateClient = updater.New("owner/repo", "0.2.0")
	calls := 0
	f.web.updateClient.HTTP.Transport = updateTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v0.3.0","body":"更新说明"}`))}, nil
	})
	f.login(t)
	for range 2 {
		response = f.request(t, "GET", "/api/update", nil, nil)
		var info updater.Info
		if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 || !info.Available || info.Version != "0.3.0" {
			t.Fatalf("更新提示错误：%+v", info)
		}
	}
	if calls != 1 {
		t.Fatalf("未复用更新缓存：%d", calls)
	}
}
