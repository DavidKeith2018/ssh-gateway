package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestDesktopMachineWindowAuthAndLifetime(t *testing.T) {
	d := freshDesktop(t)
	if _, err := d.MachineWindowURL("test"); err == nil {
		t.Fatal("未登录允许开窗口")
	}
	loginDesktop(t, d)
	f := newFixture(t)
	if _, err := d.store.Put(context.Background(), f.input); err != nil {
		t.Fatal(err)
	}
	if _, err := d.MachineWindowURL("../test"); err == nil {
		t.Fatal("非法路径未拒绝")
	}
	entry, err := d.MachineWindowURL("test")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(entry)
	if !strings.HasPrefix(parsed.Host, "127.0.0.1:") {
		t.Fatal("不是回环监听")
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(address string, want int, host string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", address, nil)
		if host != "" {
			req.Host = host
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != want {
			data, _ := io.ReadAll(res.Body)
			t.Fatalf("状态 %d，预期 %d：%s", res.StatusCode, want, data)
		}
		return res
	}
	base := parsed.Scheme + "://" + parsed.Host
	request(base+"/api/me", 401, "")
	request(entry, 403, "attacker.example")
	response := request(entry, 303, "")
	if strings.Contains(response.Header.Get("Location"), "ticket") || !strings.Contains(response.Header.Get("Location"), "machine=test") {
		t.Fatal("重定向未清理票据或缺少机器")
	}
	if len(response.Cookies()) != 1 || !response.Cookies()[0].HttpOnly {
		t.Fatal("缺少 HttpOnly 登录 cookie")
	}
	request(entry, 403, "")
	jar, _ := cookiejar.New(nil)
	client.Jar = jar
	jar.SetCookies(parsed, response.Cookies())
	request(base+"/api/me", 200, "")
	request(base+"/?window=1&machine=test", 200, "")
	stale, _ := d.MachineWindowURL("test")
	ticketURL, _ := url.Parse(stale)
	d.mu.Lock()
	ticket := d.windowTickets[ticketURL.Query().Get("ticket")]
	ticket.expires = time.Now().Add(-time.Second)
	d.windowTickets[ticketURL.Query().Get("ticket")] = ticket
	d.mu.Unlock()
	request(stale, 403, "")
	revoked, _ := d.MachineWindowURL("test")
	desktopCall(t, d, "POST", "/logout", nil)
	request(revoked, 401, "")
	request(base+"/api/me", 401, "")
	d.Close()
	if res, err := client.Get(base); err == nil {
		res.Body.Close()
		t.Fatal("应用退出后仍在监听")
	}
}
