package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"ssh-gateway/localization"
	"strings"
	"time"
)

type machineWindowTicket struct {
	target    string
	selection string
	cookie    http.Cookie
	expires   time.Time
}

// MachineWindowURL 为已登录桌面会话签发一次性浏览器入口；只按需监听回环地址。
func (d *Desktop) MachineWindowURL(target string, selections ...string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || !d.web.canAccess(d.request("GET", "/me", ""), target) {
		return "", fmt.Errorf("请先登录中转台")
	}
	if !identifier.MatchString(target) {
		return "", fmt.Errorf("机器 ID 无效")
	}
	if _, err := d.store.Get(d.ctx, target); err != nil {
		return "", fmt.Errorf("机器不存在")
	}
	selection := ""
	if len(selections) > 0 {
		selection = selections[0]
	}
	if _, err := d.store.connectionRecord(d.ctx, target, selection); err != nil {
		return "", err
	}
	if strings.HasPrefix(selection, "server:") && !d.web.isAdmin(d.request("GET", "/me", "")) {
		return "", ErrDenied
	}
	if d.windowServer == nil {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", err
		}
		d.windowAddress = listener.Addr().String()
		d.windowTickets = make(map[string]machineWindowTicket)
		d.windowServer = &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, Handler: http.HandlerFunc(d.serveMachineWindow)}
		server := d.windowServer
		d.workers.Add(1)
		go func() { defer d.workers.Done(); _ = server.Serve(listener) }()
	}
	now := time.Now()
	for key, ticket := range d.windowTickets {
		if !now.Before(ticket.expires) {
			delete(d.windowTickets, key)
		}
	}
	if len(d.windowTickets) >= 32 {
		return "", fmt.Errorf("打开窗口过于频繁，请稍后再试")
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	key := hex.EncodeToString(token)
	d.windowTickets[key] = machineWindowTicket{target: target, selection: selection, cookie: *d.cookie, expires: now.Add(time.Minute)}
	return "http://" + d.windowAddress + "/desktop/window?ticket=" + key, nil
}

func (d *Desktop) serveMachineWindow(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		http.Error(w, "应用已退出", 503)
		return
	}
	if r.Host != d.windowAddress {
		d.mu.Unlock()
		http.Error(w, "主机无效", 403)
		return
	}
	d.workers.Add(1)
	handler := d.handler
	d.mu.Unlock()
	defer d.workers.Done()
	if r.URL.Path != "/desktop/window" {
		handler.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		http.Error(w, "只允许 GET", 405)
		return
	}
	d.mu.Lock()
	ticket, ok := d.windowTickets[r.URL.Query().Get("ticket")]
	delete(d.windowTickets, r.URL.Query().Get("ticket"))
	d.mu.Unlock()
	if !ok || !time.Now().Before(ticket.expires) {
		http.Error(w, "窗口入口已过期，请重新打开", 403)
		return
	}
	check := r.Clone(r.Context())
	check.Header = r.Header.Clone()
	check.Header.Del("Cookie")
	check.AddCookie(&ticket.cookie)
	if !d.web.canAccess(check, ticket.target) {
		http.Error(w, "登录已失效，请重新登录", 401)
		return
	}
	http.SetCookie(w, &ticket.cookie)
	http.Redirect(w, r, "/?window=1&machine="+url.QueryEscape(ticket.target)+"&connection="+url.QueryEscape(ticket.selection)+windowLanguageQuery(r), http.StatusSeeOther)
}

func windowLanguageQuery(r *http.Request) string {
	value := r.URL.Query().Get("lang")
	if value == "" {
		return ""
	}
	return "&lang=" + url.QueryEscape(localization.Normalize(value))
}
