package gateway

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

type MappingView struct {
	Mapping
	Status          string   `json:"status"`
	Error           string   `json:"error"`
	LastError       string   `json:"last_error"`
	Warning         string   `json:"warning"`
	ScopeVerified   bool     `json:"scope_verified"`
	Connections     int      `json:"connections"`
	BindAddress     string   `json:"bind_address"`
	AccessAddresses []string `json:"access_addresses"`
}
type mappingRun struct {
	config                              Mapping
	cancel                              context.CancelFunc
	done                                chan struct{}
	status, failure, lastError, warning string
	verified                            bool
	connections                         int
}

// MappingManager 的生命周期属于应用，不属于网页或终端会话。
type MappingManager struct {
	store   *Store
	ctx     context.Context
	cancel  context.CancelFunc
	opMu    sync.Mutex
	mu      sync.Mutex
	runs    map[string]*mappingRun
	closed  bool
	workers sync.WaitGroup
	streams chan struct{}
}

func (s *Store) mappingManager(ctx context.Context) *MappingManager {
	s.mappingMu.Lock()
	defer s.mappingMu.Unlock()
	if s.mappings != nil {
		return s.mappings
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &MappingManager{store: s, ctx: ctx, cancel: cancel, runs: map[string]*mappingRun{}, streams: make(chan struct{}, 256)}
	s.mappings = m
	items, err := s.mappingRecords(ctx)
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		select {
		case <-s.unlocked:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
		for _, r := range items {
			if r.AutoStart {
				if startErr := m.Start(ctx, r.ID, r.source); startErr != nil {
					done := make(chan struct{})
					close(done)
					m.mu.Lock()
					if m.runs[r.ID] == nil {
						m.runs[r.ID] = &mappingRun{config: r.Mapping, cancel: func() {}, done: done, status: "error", failure: "自动启动失败：" + startErr.Error()}
					}
					m.mu.Unlock()
				}
			}
		}
	}()
	return m
}
func (m *MappingManager) Close() {
	m.opMu.Lock()
	m.closed = true
	m.cancel()
	m.opMu.Unlock()
	m.workers.Wait()
}
func (m *MappingManager) Active() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, r := range m.runs {
		if r.status == "running" || r.status == "connecting" {
			n++
		}
	}
	return n
}
func (m *MappingManager) stopLocked(id string) {
	m.mu.Lock()
	r := m.runs[id]
	if r != nil {
		r.cancel()
	}
	m.mu.Unlock()
	if r != nil {
		<-r.done
		m.mu.Lock()
		r.status = "stopped"
		r.failure = ""
		r.lastError = ""
		r.connections = 0
		r.verified = false
		r.warning = ""
		m.mu.Unlock()
	}
}
func (m *MappingManager) Stop(id string) { m.opMu.Lock(); defer m.opMu.Unlock(); m.stopLocked(id) }
func (m *MappingManager) Save(ctx context.Context, in Mapping, source string, create bool) (Mapping, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if m.closed {
		return in, fail(503, "转发管理器已停止")
	}
	saved, err := m.store.putMapping(ctx, in, source, create)
	if err != nil {
		return saved, err
	}
	m.stopLocked(saved.ID)
	return saved, nil
}
func (m *MappingManager) Delete(ctx context.Context, id string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if _, err := m.store.mapping(ctx, id); err != nil {
		return fail(404, "映射不存在")
	}
	if _, err := m.store.db.ExecContext(ctx, `DELETE FROM mappings WHERE id=?`, id); err != nil {
		return err
	}
	m.stopLocked(id)
	m.mu.Lock()
	delete(m.runs, id)
	m.mu.Unlock()
	return nil
}
func (m *MappingManager) Start(ctx context.Context, id, source string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return fail(503, "转发管理器已停止")
	}
	config, err := m.store.mapping(ctx, id)
	if err != nil {
		return fail(404, "映射不存在")
	}
	selected, err := m.store.get(ctx, "id", config.TargetID)
	addr := &net.TCPAddr{IP: net.ParseIP(source)}
	if err != nil || addr.IP == nil || !selected.Enabled || !m.store.sourceAllowed(ctx, selected.Target, addr) {
		return fail(403, "当前来源不允许访问，或目标已禁用")
	}
	m.mu.Lock()
	old := m.runs[id]
	active := old != nil && (old.status == "running" || old.status == "connecting")
	m.mu.Unlock()
	if active {
		return nil
	}
	if m.Active() >= 32 {
		return fail(429, "同时运行的映射最多 32 条")
	}
	// 手动启动更新自启动授权来源；客户端无法在 JSON 中伪造此值。
	if _, err = m.store.db.ExecContext(ctx, `UPDATE mappings SET source_ip=? WHERE id=?`, source, id); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(m.ctx)
	r := &mappingRun{config: config.Mapping, cancel: cancel, done: make(chan struct{}), status: "connecting"}
	m.mu.Lock()
	m.runs[id] = r
	m.mu.Unlock()
	m.workers.Add(1)
	go func() { defer m.workers.Done(); defer close(r.done); m.run(runCtx, r, selected, addr) }()
	return nil
}
func (m *MappingManager) Views(ctx context.Context) ([]MappingView, error) {
	records, err := m.store.mappingRecords(ctx)
	if err != nil {
		return nil, err
	}
	targets, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	hosts := map[string]string{}
	for _, t := range targets {
		hosts[t.ID] = t.Host
	}
	ips := localMappingIPs()
	out := make([]MappingView, 0, len(records))
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range records {
		v := MappingView{Mapping: item.Mapping, Status: "stopped", BindAddress: item.bindAddress(), AccessAddresses: []string{}}
		if r := m.runs[item.ID]; r != nil && r.config.Revision == item.Revision {
			v.Status = r.status
			v.Error = r.failure
			v.LastError = r.lastError
			v.Warning = r.warning
			v.ScopeVerified = r.verified
			v.Connections = r.connections
		}
		if item.Scope == "loopback" {
			v.AccessAddresses = append(v.AccessAddresses, net.JoinHostPort("127.0.0.1", fmt.Sprint(item.ListenPort)))
		} else if item.Direction == "reverse" {
			v.AccessAddresses = append(v.AccessAddresses, net.JoinHostPort(hosts[item.TargetID], fmt.Sprint(item.ListenPort)))
		} else {
			for _, ip := range ips {
				v.AccessAddresses = append(v.AccessAddresses, net.JoinHostPort(ip, fmt.Sprint(item.ListenPort)))
			}
		}
		out = append(out, v)
	}
	return out, nil
}
func localMappingIPs() []string {
	out := []string{}
	seen := map[string]bool{}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		ip, _, err := net.ParseCIDR(a.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsUnspecified() && !seen[ip.String()] {
			out = append(out, ip.String())
			seen[ip.String()] = true
		}
	}
	return out
}
func (m *MappingManager) run(ctx context.Context, r *mappingRun, selected record, source net.Addr) {
	var failure error
	var startupExpired atomic.Bool
	var client *ssh.Client
	var listener net.Listener
	var streams sync.WaitGroup
	// 所有路径先断开 SSH，再关闭远程监听，避免无响应的取消转发请求阻塞退出。
	defer func() {
		r.cancel()
		if startupExpired.Load() && failure == nil {
			failure = fmt.Errorf("建立映射超时，请检查 SSH 连接和转发策略")
		}
		if client != nil {
			client.Close()
		}
		if listener != nil {
			listener.Close()
		}
		streams.Wait()
		m.mu.Lock()
		r.connections = 0
		if failure != nil {
			r.status = "error"
			r.failure = failure.Error()
		} else {
			r.status = "stopped"
		}
		m.mu.Unlock()
		if failure != nil {
			m.store.logEvent(r.config.TargetID, "", "端口映射停止："+r.config.Name+"："+failure.Error())
		}
	}()
	startup := time.AfterFunc(20*time.Second, func() { startupExpired.Store(true); r.cancel() })
	defer startup.Stop()
	var err error
	client, err = m.store.dial(ctx, selected)
	if err != nil {
		if ctx.Err() == nil {
			failure = fmt.Errorf("SSH 连接失败：%w", err)
		}
		return
	}
	if r.config.Direction == "local" {
		listener, err = net.Listen("tcp4", r.config.bindAddress())
	} else {
		listener, err = client.ListenTCP(&net.TCPAddr{IP: net.ParseIP(r.config.bindHost()), Port: r.config.ListenPort})
	}
	if err != nil {
		if ctx.Err() == nil {
			failure = fmt.Errorf("无法建立映射入口（端口占用、权限或 SSH 转发策略）：%w", err)
		}
		return
	}
	verified := true
	warning := ""
	if r.config.Direction == "reverse" {
		verified, warning, err = verifyMappingScope(client, r.config)
		if err != nil {
			failure = err
			return
		}
	}
	if !NewServer(m.store, nil).unchanged(ctx, selected, source) {
		failure = fmt.Errorf("目标配置或访问授权已变更")
		return
	}
	startup.Stop()
	if ctx.Err() != nil {
		return
	}
	m.mu.Lock()
	r.status = "running"
	r.verified = verified
	r.warning = warning
	m.mu.Unlock()
	m.store.logEvent(selected.ID, "", "端口映射已启动："+r.config.Name+"（"+r.config.Direction+"，"+r.config.bindAddress()+"）")
	// Accept 独立运行；主循环负责撤销检查和连接存活。
	accepted := make(chan net.Conn)
	acceptErr := make(chan error, 1)
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				acceptErr <- e
				return
			}
			select {
			case accepted <- c:
			case <-ctx.Done():
				c.Close()
				return
			}
		}
	}()
	disconnected := make(chan error, 1)
	go func() { disconnected <- client.Wait() }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	slots := make(chan struct{}, 32)
	for {
		select {
		case <-ctx.Done():
			return
		case <-disconnected:
			if ctx.Err() == nil {
				failure = fmt.Errorf("SSH 连接已断开，请重试")
			}
			return
		case err := <-acceptErr:
			if ctx.Err() == nil {
				failure = fmt.Errorf("映射入口已关闭：%w", err)
			}
			return
		case <-ticker.C:
			config, e := m.store.mapping(ctx, r.config.ID)
			if e != nil || config.Revision != r.config.Revision || !NewServer(m.store, nil).unchanged(ctx, selected, source) {
				failure = fmt.Errorf("映射、目标配置或访问授权已变更")
				return
			}
		case conn := <-accepted:
			// 即使服务器错误地扩大回环绑定，也不把外部来源送入仅自身的反向服务。
			if r.config.Scope == "loopback" {
				host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
				if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
					conn.Close()
					continue
				}
			}
			select {
			case slots <- struct{}{}:
			default:
				conn.Close()
				continue
			}
			select {
			case m.streams <- struct{}{}:
			default:
				<-slots
				conn.Close()
				continue
			}
			streams.Add(1)
			m.mu.Lock()
			r.connections++
			m.mu.Unlock()
			go func(c net.Conn) {
				defer streams.Done()
				defer func() { <-slots; <-m.streams; m.mu.Lock(); r.connections--; m.mu.Unlock() }()
				m.pipe(ctx, r, client, c)
			}(conn)
		}
	}
}
func (m *MappingManager) pipe(ctx context.Context, r *mappingRun, client *ssh.Client, in net.Conn) {
	defer in.Close()
	stopIn := context.AfterFunc(ctx, func() { in.Close() })
	defer stopIn()
	address := net.JoinHostPort(r.config.ServiceHost, strconv.Itoa(r.config.ServicePort))
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out net.Conn
	var err error
	if r.config.Direction == "local" {
		out, err = client.DialContext(dialCtx, "tcp", address)
	} else {
		out, err = (&net.Dialer{}).DialContext(dialCtx, "tcp", address)
	}
	if err != nil {
		if ctx.Err() == nil {
			m.mu.Lock()
			r.lastError = "服务连接失败：" + err.Error()
			m.mu.Unlock()
		}
		return
	}
	defer out.Close()
	stopOut := context.AfterFunc(ctx, func() { out.Close() })
	defer stopOut()
	m.mu.Lock()
	r.lastError = ""
	m.mu.Unlock()
	copyMappingStream(in, out)
}
func copyMappingStream(a, b net.Conn) {
	done := make(chan struct{})
	copyOne := func(dst, src net.Conn) {
		_, err := io.Copy(dst, src)
		if c, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		} else {
			dst.Close()
		}
		if err != nil {
			a.Close()
			b.Close()
		}
	}
	go func() { copyOne(b, a); close(done) }()
	copyOne(a, b)
	<-done
}

// Linux 的内核监听表可区分请求地址和 sshd 实际绑定。其他系统返回待核验，
// 不把 SSH 转发成功伪装为范围已确认；回环模式仍检查每个入口连接的来源。
const mappingScopeCommand = "cat /proc/net/tcp /proc/net/tcp6"

type mappingOutput struct {
	data     []byte
	overflow bool
}

func (b *mappingOutput) Write(p []byte) (int, error) {
	n := len(p)
	left := (256 << 10) - len(b.data)
	if len(p) > left {
		b.overflow = true
		p = p[:left]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func verifyMappingScope(client *ssh.Client, m Mapping) (bool, string, error) {
	session, err := client.NewSession()
	if err != nil {
		return false, "无法核验远程监听范围；请在远程服务器确认实际绑定地址", nil
	}
	defer session.Close()
	var output mappingOutput
	session.Stdout = &output
	session.Stderr = io.Discard
	timer := time.AfterFunc(3*time.Second, func() { session.Close() })
	err = session.Run(mappingScopeCommand)
	timer.Stop()
	if err != nil || output.overflow {
		return false, "无法核验远程监听范围；请在远程服务器确认实际绑定地址", nil
	}
	return parseMappingScope(string(output.data), m)
}
func parseMappingScope(output string, m Mapping) (bool, string, error) {
	found, loopback, wildcard, external := false, false, false, false
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[3] != "0A" {
			continue
		}
		parts := strings.Split(fields[1], ":")
		if len(parts) != 2 {
			continue
		}
		port, e := strconv.ParseUint(parts[1], 16, 16)
		if e != nil || int(port) != m.ListenPort {
			continue
		}
		raw, e := hex.DecodeString(parts[0])
		if e != nil || (len(raw) != 4 && len(raw) != 16) {
			continue
		}
		for i := 0; i < len(raw); i += 4 {
			raw[i], raw[i+3] = raw[i+3], raw[i]
			raw[i+1], raw[i+2] = raw[i+2], raw[i+1]
		}
		ip := net.IP(raw)
		found = true
		if ip.IsUnspecified() {
			wildcard = true
		} else if ip.IsLoopback() {
			loopback = true
		} else {
			external = true
		}
	}
	if !found {
		return false, "未能定位远程监听地址，访问范围待核验", nil
	}
	if m.Scope == "loopback" && (wildcard || external) {
		return false, "", errors.New("远程 SSH 服务扩大了监听范围；仅自身访问未生效，请将 GatewayPorts 配置为 clientspecified 后重试")
	}
	if m.Scope == "shared" && !wildcard {
		return false, "", errors.New("远程 SSH 服务未允许全网卡监听，请检查 GatewayPorts clientspecified 和转发策略")
	}
	return loopback || wildcard, "", nil
}
