package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
	"net"
	"net/http"
	"net/url"
	"os"
	"ssh-gateway/updater"
	"strings"
	"sync"
	"time"
)

type DesktopSettings struct {
	External    bool   `json:"external"`
	Port        int    `json:"port"`
	ConnectHost string `json:"connect_host"`
}
type DesktopInfo struct {
	MessageKey    string          `json:"message_key,omitempty"`
	MessageParams []any           `json:"message_params,omitempty"`
	Ready         bool            `json:"ready"`
	NeedsSetup    bool            `json:"needs_setup"`
	DataDir       string          `json:"data_dir"`
	Listen        string          `json:"listen"`
	Error         string          `json:"error"`
	Active        int64           `json:"active"`
	Settings      DesktopSettings `json:"settings"`
	Version       string          `json:"version"`
}
type DesktopReply struct {
	Status int             `json:"status"`
	Data   json.RawMessage `json:"data"`
}
type TerminalEvent struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}
type nativeTerminal struct {
	cancel  context.CancelFunc
	session *TerminalSession
	credit  chan struct{}
}

// Desktop 默认通过内存 HTTP 适配器访问；独立窗口按需启动仅回环可达的浏览器服务。
type Desktop struct {
	windowServer  *http.Server
	windowAddress string
	windowTickets map[string]machineWindowTicket
	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	store         *Store
	lock          *os.File
	dir           string
	web           *Web
	handler       http.Handler
	cookie        *http.Cookie
	signer        ssh.Signer
	server        *Server
	serverCancel  context.CancelFunc
	serverDone    chan error
	settings      DesktopSettings
	address       string
	listenError   string
	closed        bool
	terminalMu    sync.Mutex
	terminals     map[string]*nativeTerminal
	workers       sync.WaitGroup
}

func OpenDesktop(parent context.Context, dir string) (*Desktop, error) {
	lock, err := AcquireInstance(dir)
	if err != nil {
		return nil, err
	}
	store, err := OpenStore(dir)
	if err != nil {
		lock.Close()
		return nil, err
	}
	signer, err := LoadHostKey(dir)
	if err != nil {
		store.Close()
		lock.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	d := &Desktop{ctx: ctx, cancel: cancel, store: store, lock: lock, dir: dir, signer: signer, terminals: map[string]*nativeTerminal{}, settings: DesktopSettings{Port: 2222, ConnectHost: "127.0.0.1"}}
	var raw []byte
	err = store.db.QueryRow(`SELECT value FROM settings WHERE name='desktop_settings'`).Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &d.settings)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		d.Close()
		return nil, err
	}
	if err := validateDesktopSettings(d.settings); err != nil {
		d.Close()
		return nil, err
	}
	d.web = NewWeb(ctx, store, signer, "")
	d.web.localDesktop = true
	d.handler = d.web.Handler()
	// 第一次设置密码后才启动；已有配置直接启动。
	if _, err := store.adminHash(ctx); err == nil && !store.MasterPasswordStatus().Locked {
		d.startLocked()
	}
	return d, nil
}

func validateDesktopSettings(s DesktopSettings) error {
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("端口必须在 1～65535 之间")
	}
	if strings.TrimSpace(s.ConnectHost) == "" || strings.ContainsAny(s.ConnectHost, " \t\r\n/\\'\";$`|&<>()") {
		return fmt.Errorf("连接地址应为主机名或 IP")
	}
	return nil
}
func (d *Desktop) startLocked() {
	if d.store.MasterPasswordStatus().Locked {
		return
	}
	host := "127.0.0.1"
	if d.settings.External {
		host = "0.0.0.0"
	}
	address := net.JoinHostPort(host, fmt.Sprint(d.settings.Port))
	listener, err := net.Listen("tcp", address)
	d.address = ""
	d.listenError = ""
	if err != nil {
		d.listenError = "SSH 启动失败，请修改端口或关闭占用程序：" + err.Error()
		return
	}
	d.address = listener.Addr().String()
	d.web.sshAddress = d.address
	d.web.relayHost = d.settings.ConnectHost
	ctx, cancel := context.WithCancel(d.ctx)
	d.serverCancel = cancel
	d.serverDone = make(chan error, 1)
	d.server = NewServer(d.store, d.signer)
	server, done := d.server, d.serverDone
	go func() { done <- server.Serve(ctx, listener) }()
}
func (d *Desktop) stopLocked() {
	if d.serverCancel != nil {
		d.serverCancel()
		<-d.serverDone
		d.serverCancel = nil
		d.serverDone = nil
		d.server = nil
	}
	d.address = ""
}
func (d *Desktop) activeLocked() int64 {
	var n int64
	if d.server != nil {
		n = d.server.Active()
	}
	d.terminalMu.Lock()
	n += int64(len(d.terminals))
	if d.web != nil {
		n += int64(len(d.web.terminals))
		n += d.web.mappings.Active()
	}
	d.terminalMu.Unlock()
	return n
}
func (d *Desktop) Info() DesktopInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.store.adminHash(d.ctx)
	return DesktopInfo{Ready: !d.closed, NeedsSetup: errors.Is(err, sql.ErrNoRows), DataDir: d.dir, Listen: d.address, Error: d.listenError, Active: d.activeLocked(), Settings: d.settings, Version: updater.Version}
}
func (d *Desktop) request(method, path, body string) *http.Request {
	r, _ := http.NewRequestWithContext(d.ctx, method, "http://localhost/api"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:0"
	r.Header.Set("Content-Type", "application/json")
	if d.cookie != nil {
		r.AddCookie(d.cookie)
	}
	return r
}

type nativeResponse struct {
	header http.Header
	code   int
	bytes.Buffer
}

func (r *nativeResponse) Header() http.Header { return r.header }
func (r *nativeResponse) WriteHeader(code int) {
	if r.code == 0 {
		r.code = code
	}
}
func (r *nativeResponse) Write(p []byte) (int, error) {
	if r.code == 0 {
		r.code = 200
	}
	return r.Buffer.Write(p)
}

func (d *Desktop) Call(method, path, body string) (DesktopReply, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return DesktopReply{}, fmt.Errorf("应用已退出")
	}
	u, err := url.ParseRequestURI(path)
	if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\r\n#") || len(path) > 16384 {
		return DesktopReply{}, fmt.Errorf("请求路径无效")
	}
	for _, ch := range u.Path {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("/_.-", ch)) {
			return DesktopReply{}, fmt.Errorf("请求路径无效")
		}
	}
	if _, err := url.ParseQuery(u.RawQuery); err != nil {
		return DesktopReply{}, fmt.Errorf("查询参数无效")
	}
	if (method != "GET" && method != "POST" && method != "PUT" && method != "DELETE") || len(body) > 1<<20 || !strings.HasPrefix(path, "/") || strings.Contains(u.Path, "terminal") || (u.RawQuery != "" && (method != "GET" || strings.HasPrefix(u.Path, "/desktop/"))) {
		return DesktopReply{}, fmt.Errorf("不支持的请求")
	}

	if path == "/desktop/setup" && method == "POST" {
		if d.store.MasterPasswordStatus().Locked {
			return DesktopReply{423, json.RawMessage(`{"error":"请先输入主密码解锁"}`)}, nil
		}
		var in struct {
			Password       string `json:"password"`
			MasterPassword string `json:"master_password"`
		}
		if err := json.Unmarshal([]byte(body), &in); err != nil {
			return DesktopReply{}, err
		}
		if len(in.Password) < 12 || len(in.Password) > 72 {
			return DesktopReply{}, fmt.Errorf("管理员密码要求 12～72 字节")
		}
		if in.MasterPassword != "" {
			if err := validateMasterPassword(in.MasterPassword); err != nil {
				return DesktopReply{}, err
			}
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			return DesktopReply{}, err
		}
		result, err := d.store.db.Exec(`INSERT INTO settings VALUES ('admin_password',?) ON CONFLICT(name) DO NOTHING`, hash)
		if err != nil {
			return DesktopReply{}, err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return DesktopReply{}, fmt.Errorf("管理员已设置，请登录")
		}
		if in.MasterPassword != "" {
			if err := d.store.ChangeMasterPassword("", in.MasterPassword); err != nil {
				// 只有本次新建的管理员记录才可回滚，避免失败后首次设置无法重试。
				_, rollbackErr := d.store.db.Exec(`DELETE FROM settings WHERE name='admin_password' AND value=?`, hash)
				if rollbackErr != nil {
					return DesktopReply{}, fmt.Errorf("主密码设置失败：%v；初始化回滚失败，请重启后检查设置", err)
				}
				return DesktopReply{}, err
			}
		}
		d.startLocked()
		return DesktopReply{200, json.RawMessage(`{"ok":true}`)}, nil
	}
	if path == "/desktop/password" && method == "POST" {
		if !d.web.isAdmin(d.request("GET", "/me", "")) {
			return DesktopReply{401, json.RawMessage(`{"error":"请先登录"}`)}, nil
		}
		var in struct {
			Current  string `json:"current"`
			Password string `json:"password"`
		}
		if err := json.Unmarshal([]byte(body), &in); err != nil {
			return DesktopReply{}, err
		}
		hash, err := d.store.adminHash(d.ctx)
		if err != nil || bcrypt.CompareHashAndPassword(hash, []byte(in.Current)) != nil {
			return DesktopReply{}, fmt.Errorf("当前密码不正确")
		}
		if err := d.store.SetAdminPassword(d.ctx, in.Password); err != nil {
			return DesktopReply{}, err
		}
		d.cookie = nil
		d.closeTerminals()
		return DesktopReply{200, json.RawMessage(`{"ok":true}`)}, nil
	}
	response := &nativeResponse{header: make(http.Header)}
	d.handler.ServeHTTP(response, d.request(method, path, body))
	if path == "/unlock" && response.code == 200 && d.serverCancel == nil {
		if _, err := d.store.adminHash(d.ctx); err == nil {
			d.startLocked()
		}
	}
	result := &http.Response{Header: response.header}
	for _, cookie := range result.Cookies() {
		if cookie.Name == cookieName {
			d.cookie = cookie
			if cookie.MaxAge < 0 {
				d.cookie = nil
			}
		}
	}
	if path == "/logout" {
		d.closeTerminals()
	}
	if !json.Valid(response.Bytes()) {
		return DesktopReply{}, fmt.Errorf("不支持的管理操作")
	}
	return DesktopReply{response.code, append(json.RawMessage(nil), response.Bytes()...)}, nil
}

func (d *Desktop) SaveSettings(settings DesktopSettings, confirmed bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.web.isAdmin(d.request("GET", "/me", "")) {
		return ErrDenied
	}
	if err := validateDesktopSettings(settings); err != nil {
		return err
	}
	if d.activeLocked() > 0 && !confirmed {
		return fmt.Errorf("存在活动连接，请确认断开后再修改监听")
	}
	data, _ := json.Marshal(settings)
	if _, err := d.store.db.Exec(`INSERT INTO settings VALUES ('desktop_settings',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value`, data); err != nil {
		return err
	}
	d.closeTerminals()
	d.stopLocked()
	d.settings = settings
	d.startLocked()
	return nil
}

type desktopWriter struct {
	ctx    context.Context
	id     string
	credit chan struct{}
	emit   func(TerminalEvent)
}

func (w desktopWriter) Write(data []byte) (int, error) {
	total := len(data)
	for len(data) > 0 {
		n := len(data)
		if n > 16384 {
			n = 16384
		}
		select {
		case w.credit <- struct{}{}:
		case <-w.ctx.Done():
			return 0, w.ctx.Err()
		}
		w.emit(TerminalEvent{ID: w.id, Type: "output", Data: base64.StdEncoding.EncodeToString(data[:n])})
		data = data[n:]
	}
	return total, nil
}
func (d *Desktop) OpenTerminal(id, target string, emit func(TerminalEvent), selections ...string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || !identifier.MatchString(id) || !d.web.canAccess(d.request("GET", "/me", ""), target) {
		return ErrDenied
	}
	selection := ""
	if len(selections) > 0 {
		selection = selections[0]
	}
	request := d.request("GET", "/me?connection="+url.QueryEscape(selection), "")
	if strings.HasPrefix(selection, "server:") && !d.web.isAdmin(request) {
		return ErrDenied
	}
	endpoint, err := d.web.terminalEndpoint(request)
	if err != nil {
		return err
	}
	d.terminalMu.Lock()
	if _, ok := d.terminals[id]; ok || len(d.terminals) >= 16 {
		d.terminalMu.Unlock()
		return fmt.Errorf("终端已存在或数量达到上限")
	}
	ctx, cancel := context.WithCancel(d.ctx)
	state := &nativeTerminal{cancel: cancel, credit: make(chan struct{}, 16)}
	d.terminals[id] = state
	d.terminalMu.Unlock()
	selected, err := d.store.terminalRecord(ctx, target, selection, endpoint != nil)
	if err != nil {
		d.CloseTerminal(id)
		return err
	}
	emit(TerminalEvent{ID: id, Type: "connection", Message: terminalConnectionInfo(selected, endpoint)})
	session, err := d.store.openTerminal(ctx, target, &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}, desktopWriter{ctx, id, state.credit, emit}, endpoint, selection)
	if err != nil {
		d.CloseTerminal(id)
		return err
	}
	d.terminalMu.Lock()
	if d.terminals[id] != state {
		d.terminalMu.Unlock()
		session.Close()
		return fmt.Errorf("终端已取消")
	}
	sharedID, unshare := d.web.shareTerminal(request, ctx, session.client, target, selection)
	context.AfterFunc(ctx, unshare)
	state.session = session
	d.terminalMu.Unlock()
	emit(TerminalEvent{ID: id, Type: "connection", Message: terminalConnectionInfo(selected, endpoint, sharedID)})

	cookie := d.cookie
	d.workers.Add(1)
	go func() {
		defer d.workers.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r, _ := http.NewRequestWithContext(ctx, "GET", "http://localhost/api/me", nil)
				r.AddCookie(cookie)
				if !d.web.canAccess(r, target) {
					cancel()
					return
				}
			}
		}
	}()
	d.store.logEvent(target, "127.0.0.1", "桌面终端已连接")
	d.workers.Add(1)
	go func() {
		defer d.workers.Done()
		err := session.Wait()
		d.CloseTerminal(id)
		msg := "终端会话已结束"
		if err != nil {
			msg = "终端已断开"
		}
		emit(TerminalEvent{ID: id, Type: "closed", Message: msg})
		d.store.logEvent(target, "127.0.0.1", "桌面终端已断开")
	}()
	emit(TerminalEvent{ID: id, Type: "ready", Message: "已连接"})
	return nil
}
func (d *Desktop) TerminalInput(id, data string, columns, rows int) error {
	if len(data) > 64<<10 {
		return fmt.Errorf("输入过长")
	}
	d.terminalMu.Lock()
	state := d.terminals[id]
	var session *TerminalSession
	if state != nil {
		session = state.session
	}
	d.terminalMu.Unlock()
	if session == nil {
		return fmt.Errorf("终端已断开")
	}
	if columns > 0 || rows > 0 {
		return session.Resize(columns, rows)
	}
	return session.Write(data)
}
func (d *Desktop) AckTerminal(id string) {
	d.terminalMu.Lock()
	state := d.terminals[id]
	d.terminalMu.Unlock()
	if state != nil {
		select {
		case <-state.credit:
		default:
		}
	}
}
func (d *Desktop) CloseTerminal(id string) {
	d.terminalMu.Lock()
	state := d.terminals[id]
	delete(d.terminals, id)
	d.terminalMu.Unlock()
	if state != nil {
		state.cancel()
		if state.session != nil {
			state.session.Close()
		}
	}
}
func (d *Desktop) closeTerminals() {
	d.terminalMu.Lock()
	ids := make([]string, 0, len(d.terminals))
	for id := range d.terminals {
		ids = append(ids, id)
	}
	d.terminalMu.Unlock()
	for _, id := range ids {
		d.CloseTerminal(id)
	}
}
func (d *Desktop) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	if d.windowServer != nil {
		d.windowServer.Close()
	}
	d.cancel()
	d.closeTerminals()
	d.stopLocked()
	d.mu.Unlock()
	d.workers.Wait()
	d.store.Close()
	d.lock.Close()
}
