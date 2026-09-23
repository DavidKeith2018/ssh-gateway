package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"ssh-gateway/localization"
	"strings"
	"sync"
	"time"

	"ssh-gateway/updater"

	"github.com/coder/websocket"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
)

const cookieName = "ssh_gateway_session"

type browserSession struct {
	userID  string
	epoch   int64
	expires time.Time
	hash    []byte
}

type loginAttempts struct {
	count int
	until time.Time
}

type Web struct {
	diagnostics    map[string]*diagnosticJob
	sharedMachines map[string]sharedMachine
	updateClient   *updater.Client
	masterSlots    chan struct{}
	publicOrigin   *url.URL
	mappings       *MappingManager
	localDesktop   bool
	ctx            context.Context
	store          *Store
	sshAddress     string
	relayHost      string
	signer         ssh.Signer
	mu             sync.Mutex
	sessions       map[string]browserSession
	attempts       map[string]loginAttempts
	terminals      chan struct{}
	machineSlots   chan struct{}
}

// NewWeb 提供同源管理页面和 API；ctx 取消时终止全部网页终端。
func NewWeb(ctx context.Context, store *Store, signer ssh.Signer, sshAddress string) *Web {
	return &Web{updateClient: updater.New(updater.Repository, updater.Version), masterSlots: make(chan struct{}, 1), mappings: store.mappingManager(ctx), ctx: ctx, store: store, signer: signer, sshAddress: sshAddress, sessions: make(map[string]browserSession), attempts: make(map[string]loginAttempts), terminals: make(chan struct{}, 32), machineSlots: make(chan struct{}, 16)}
}

// SetPublicOrigin 在启动服务前设置反向代理的固定外部来源，不信任转发请求头。
func (web *Web) SetPublicOrigin(origin string) error {
	if origin == "" {
		web.publicOrigin = nil
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("网页外部来源必须是 HTTP 或 HTTPS 的协议、主机和可选端口，不能包含路径或认证信息")
	}
	u.Path = ""
	web.publicOrigin = u
	return nil
}

func (web *Web) secureRequest(r *http.Request) bool {
	return r.TLS != nil || (web.publicOrigin != nil && web.publicOrigin.Scheme == "https" && r.Host == web.publicOrigin.Host)
}

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func apiError(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, struct {
		Error string `json:"error"`
		localization.Message
	}{message, localization.Describe(message)})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("JSON 配置无效")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("只允许一个 JSON 对象")
	}
	return nil
}

func (web *Web) Handler() http.Handler {
	mux := http.NewServeMux()
	web.masterPasswordRoutes(mux)
	mux.HandleFunc("POST /api/targets/{id}/diagnostics", web.administrator(web.startDiagnostic))
	mux.HandleFunc("GET /api/diagnostics/{job}", web.administrator(web.diagnosticResult))
	mux.HandleFunc("DELETE /api/diagnostics/{job}", web.administrator(web.diagnosticResult))
	mux.HandleFunc("POST /api/import/preview", web.administrator(web.importPreview))
	mux.HandleFunc("POST /api/import/commit", web.administrator(web.importCommit))
	mux.HandleFunc("POST /api/backups", web.administrator(web.exportBackup))
	mux.HandleFunc("GET /api/version", web.programVersion)
	mux.HandleFunc("GET /api/update", web.protected(web.programUpdate))
	mux.HandleFunc("GET /api/relay-endpoint", web.administrator(web.relayEndpointSettings))
	mux.HandleFunc("PUT /api/relay-endpoint", web.administrator(web.relayEndpointSettings))
	web.mappingRoutes(mux)
	web.userRoutes(mux)
	mux.HandleFunc("POST /api/login", web.login)
	mux.HandleFunc("POST /api/logout", web.protected(web.logout))
	mux.HandleFunc("GET /api/me", web.protected(web.me))
	mux.HandleFunc("GET /api/targets/options", web.protected(web.targetOptions))
	mux.HandleFunc("GET /api/targets/summary", web.protected(web.targetSummary))
	mux.HandleFunc("GET /api/targets/{id}", web.machineProtected(web.targetDetail))
	web.shortcutRoutes(mux)
	mux.HandleFunc("GET /api/targets", web.protected(web.targetPage))
	mux.HandleFunc("POST /api/targets", web.administrator(web.put))
	mux.HandleFunc("PUT /api/targets/{id}", web.administrator(web.put))
	mux.HandleFunc("DELETE /api/targets/{id}", web.administrator(web.remove))
	mux.HandleFunc("POST /api/targets/{id}/test", web.administrator(web.test))
	mux.HandleFunc("POST /api/targets/{id}/relays/{relay}/credentials", web.machineProtected(web.relayCredentials))
	mux.HandleFunc("POST /api/targets/{id}/logins/{login}/credentials", web.administrator(web.serverCredentials))
	mux.HandleFunc("POST /api/probe", web.administrator(web.probe))
	mux.HandleFunc("GET /api/global-ips", web.administrator(web.globalIPs))
	mux.HandleFunc("PUT /api/global-ips", web.administrator(web.globalIPs))
	mux.HandleFunc("DELETE /api/global-ips", web.administrator(web.globalIPs))
	mux.HandleFunc("GET /api/events", web.administrator(web.eventPage))
	mux.HandleFunc("GET /api/targets/{id}/terminal", web.machineProtected(web.terminal))
	mux.HandleFunc("POST /api/targets/{id}/notes", web.machineProtected(web.notes))
	mux.HandleFunc("POST /api/targets/{id}/files", web.machineProtected(web.files))
	mux.HandleFunc("GET /api/targets/{id}/resources", web.machineProtected(web.resources))
	mux.HandleFunc("GET /api/targets/{id}/hardware", web.machineProtected(web.hardware))
	mux.HandleFunc("GET /api/targets/{id}/transfer", web.machineProtected(web.transfer))
	mux.HandleFunc("PUT /api/targets/{id}/transfer", web.machineProtected(web.transfer))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { apiError(w, 404, "接口不存在") })
	mux.Handle("/", assetHandler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" || !web.sameOrigin(r) {
				apiError(w, 403, "不允许跨站请求")
				return
			}
			if r.Method != http.MethodGet && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") && !(r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/transfer") && r.Header.Get("Content-Type") == "application/octet-stream") {
				apiError(w, 415, "请使用 application/json")
				return
			}
			if web.store.MasterPasswordStatus().Locked && r.URL.Path != "/api/security" && r.URL.Path != "/api/unlock" && r.URL.Path != "/api/version" {
				apiError(w, 423, ErrMasterLocked.Error())
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func (web *Web) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if web.publicOrigin != nil && r.Host == web.publicOrigin.Host {
		return origin == web.publicOrigin.String()
	}
	u, err := url.Parse(origin)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return err == nil && u.Host == r.Host && u.Scheme == scheme && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}

func sourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	return host // 不读取客户端可伪造的 X-Forwarded-For。
}

func (web *Web) validSession(r *http.Request) bool {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	web.mu.Lock()
	session, ok := web.sessions[cookie.Value]
	web.mu.Unlock()
	if !ok {
		return false
	}
	hash, epoch, err := web.store.accountCredential(r.Context(), session.userID)
	// 关闭一个窗口会取消其请求；查询取消不代表凭证失效，不能注销其他窗口。
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if err == nil && time.Now().Before(session.expires) && bytes.Equal(hash, session.hash) && epoch == session.epoch {
		return true
	}
	web.mu.Lock()
	delete(web.sessions, cookie.Value)
	web.mu.Unlock()
	return false
}

func (web *Web) protected(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !web.validSession(r) {
			apiError(w, 401, "请先登录")
			return
		}
		next(w, r)
	}
}

func (web *Web) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	ip, now := sourceIP(r), time.Now()
	web.mu.Lock()
	for key, value := range web.attempts {
		if now.After(value.until) {
			delete(web.attempts, key)
		}
	}
	attempt := web.attempts[ip]
	if attempt.count >= 10 || len(web.attempts) >= 1024 {
		web.mu.Unlock()
		apiError(w, 429, "登录尝试过多，请一分钟后再试")
		return
	}
	if attempt.count == 0 {
		attempt.until = now.Add(time.Minute)
	}
	attempt.count++
	web.attempts[ip] = attempt
	web.mu.Unlock()
	userID := ""
	if input.Username != adminUsername {
		if err := web.store.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE username=?`, input.Username).Scan(&userID); err != nil {
			apiError(w, 401, "用户名或密码不正确")
			return
		}
	}
	hash, epoch, err := web.store.accountCredential(r.Context(), userID)
	if err != nil || bcrypt.CompareHashAndPassword(hash, []byte(input.Password)) != nil {
		web.store.logEvent("", ip, "帐号登录失败")
		apiError(w, 401, "用户名或密码不正确")
		return
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		apiError(w, 500, "创建登录会话失败")
		return
	}
	value := base64.RawURLEncoding.EncodeToString(secret)
	web.mu.Lock()
	for key, session := range web.sessions {
		if now.After(session.expires) || (session.userID == userID && (!bytes.Equal(hash, session.hash) || session.epoch != epoch)) {
			delete(web.sessions, key)
		}
	}
	if len(web.sessions) >= 128 {
		web.mu.Unlock()
		apiError(w, 429, "登录会话过多，请先退出其他会话")
		return
	}
	if old, err := r.Cookie(cookieName); err == nil {
		delete(web.sessions, old.Value)
	}
	web.sessions[value] = browserSession{expires: now.Add(12 * time.Hour), hash: hash, userID: userID, epoch: epoch}
	delete(web.attempts, ip)
	web.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: value, Path: "/", HttpOnly: true, Secure: web.secureRequest(r), SameSite: http.SameSiteStrictMode, MaxAge: 12 * 60 * 60})
	web.store.logEvent("", ip, "帐号已登录："+input.Username)
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (web *Web) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		web.mu.Lock()
		delete(web.sessions, cookie.Value)
		web.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", HttpOnly: true, Secure: web.secureRequest(r), SameSite: http.SameSiteStrictMode, MaxAge: -1})
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (web *Web) me(w http.ResponseWriter, r *http.Request) {
	_, port, _ := net.SplitHostPort(web.sshAddress)
	username := adminUsername
	id := web.sessionID(r)
	if id != "" {
		if err := web.store.db.QueryRowContext(r.Context(), `SELECT username FROM users WHERE id=?`, id).Scan(&username); err != nil {
			apiError(w, 401, "请重新登录")
			return
		}
	}
	jsonResponse(w, 200, map[string]any{"id": id, "username": username, "is_admin": id == "", "source_ip": sourceIP(r), "ssh_port": port, "host_fingerprint": ssh.FingerprintSHA256(web.signer.PublicKey())})
}

func (web *Web) put(w http.ResponseWriter, r *http.Request) {
	var input PutInput
	if err := decodeJSON(w, r, &input); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	if r.Method == http.MethodPut {
		input.ID = r.PathValue("id")
		if input.Revision < 1 {
			apiError(w, 400, "更新配置必须提供读取时的 revision")
			return
		}
		if _, err := web.store.Get(r.Context(), input.ID); err != nil {
			apiError(w, 404, "目标不存在")
			return
		}
	} else if _, err := web.store.Get(r.Context(), input.ID); err == nil {
		apiError(w, 409, "目标 ID 已存在")
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		apiError(w, 500, "读取目标失败")
		return
	}
	result, err := web.store.PutTarget(r.Context(), input)
	if err != nil {
		var conflict *machineError
		if errors.As(err, &conflict) {
			machineFailure(w, err)
			return
		}
		apiError(w, 400, "保存失败："+err.Error())
		return
	}
	web.store.logEvent(result.ID, sourceIP(r), "目标配置已保存")
	jsonResponse(w, 200, result)
}

func (web *Web) remove(w http.ResponseWriter, r *http.Request) {
	if err := web.store.Delete(r.Context(), r.PathValue("id")); err != nil {
		apiError(w, 404, "目标不存在或删除失败")
		return
	}
	web.store.logEvent(r.PathValue("id"), sourceIP(r), "目标已删除")
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (web *Web) test(w http.ResponseWriter, r *http.Request) {
	if err := web.store.CheckTarget(r.Context(), r.PathValue("id")); err != nil {
		web.store.logEvent(r.PathValue("id"), sourceIP(r), "目标连接测试失败")
		apiError(w, 400, "连接测试失败："+err.Error())
		return
	}
	web.store.logEvent(r.PathValue("id"), sourceIP(r), "目标连接测试成功")
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (web *Web) probe(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := decodeJSON(w, r, &input); err != nil || input.Host == "" || input.Port < 1 || input.Port > 65535 {
		apiError(w, 400, "请填写有效的主机和端口")
		return
	}
	fp, err := ProbeFingerprint(r.Context(), net.JoinHostPort(input.Host, fmt.Sprint(input.Port)))
	if err != nil {
		apiError(w, 400, "读取主机指纹失败："+err.Error())
		return
	}
	jsonResponse(w, 200, map[string]string{"fingerprint": fp})
}

type terminalInput struct {
	Type    string `json:"type"`
	Data    string `json:"data"`
	Columns int    `json:"columns"`
	Rows    int    `json:"rows"`
}

type terminalWriter struct {
	ctx context.Context
	ws  *websocket.Conn
}

func (writer terminalWriter) Write(p []byte) (int, error) {
	if err := writer.ws.Write(writer.ctx, websocket.MessageBinary, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (web *Web) terminal(w http.ResponseWriter, r *http.Request) {
	selected, err := web.selectedTerminalConnection(r)
	addr, addrErr := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	if err != nil || addrErr != nil || !selected.Enabled || !web.store.sourceAllowed(r.Context(), selected.Target, addr) {
		web.store.logEvent(r.PathValue("id"), sourceIP(r), "WebSSH 来源不允许或目标已禁用")
		apiError(w, 403, "当前来源 IP 不允许访问，或目标已禁用")
		return
	}
	select {
	case web.terminals <- struct{}{}:
		defer func() { <-web.terminals }()
	default:
		apiError(w, 429, "网页终端数量已达上限")
		return
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(64 << 10)
	ctx, cancel := context.WithCancel(web.ctx)
	defer cancel()
	send := func(kind, message string) {
		data, _ := json.Marshal(TerminalEvent{Type: kind, Message: message})
		writeCtx, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		ws.Write(writeCtx, websocket.MessageText, data)
	}
	inputs := make(chan terminalInput, 32)
	go func() {
		defer cancel()
		for {
			kind, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var input terminalInput
			if kind != websocket.MessageText || json.Unmarshal(data, &input) != nil {
				return
			}
			select {
			case inputs <- input:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		server := NewServer(web.store, web.signer)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !server.unchanged(ctx, selected, addr) || !web.canAccess(r.WithContext(ctx), selected.ID) {
					send("error", "配置或登录状态已变化，连接已断开")
					cancel()
					return
				}
			}
		}
	}()
	writer := terminalWriter{ctx: ctx, ws: ws}
	endpoint, err := web.terminalEndpoint(r)
	if err != nil {
		send("error", err.Error())
		return
	}
	send("connection", terminalConnectionInfo(selected, endpoint))
	term, err := web.store.openTerminal(ctx, selected.ID, addr, writer, endpoint, r.URL.Query().Get("connection"))
	if err != nil {
		send("error", "连接目标失败："+err.Error())
		web.store.logEvent(selected.ID, sourceIP(r), "WebSSH 目标连接失败")
		return
	}
	defer term.Close()
	sharedID, unshare := web.shareTerminal(r, ctx, term.client, selected.ID, r.URL.Query().Get("connection"))
	defer unshare()
	send("connection", terminalConnectionInfo(selected, endpoint, sharedID))
	web.store.logEvent(selected.ID, sourceIP(r), "WebSSH 已连接")
	defer web.store.logEvent(selected.ID, sourceIP(r), "WebSSH 已断开")
	send("ready", "已连接")
	go func() {
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case input := <-inputs:
				switch input.Type {
				case "input":
					if err := term.Write(input.Data); err != nil {
						return
					}
				case "resize":
					if input.Columns < 1 || input.Columns > 500 || input.Rows < 1 || input.Rows > 300 {
						return
					}
					if err := term.Resize(input.Columns, input.Rows); err != nil {
						return
					}
				default:
					return
				}
			}
		}
	}()
	err = term.Wait()
	message := "终端会话已结束"
	if err != nil && ctx.Err() == nil {
		message = "终端已退出：" + err.Error()
	}
	send("closed", message)
}
