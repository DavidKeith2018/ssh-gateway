package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

const handshakeTimeout = 10 * time.Second

type Server struct {
	store  *Store
	signer ssh.Signer
	active atomic.Int64
}

func (s *Server) Active() int64 { return s.active.Load() }

func NewServer(store *Store, signer ssh.Signer) *Server {
	return &Server{store: store, signer: signer}
}

// Serve 接管监听器；取消上下文时关闭监听器及全部已有连接。
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer workers.Wait()
	defer cancel()
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	defer listener.Close()
	slots := make(chan struct{}, 64)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { <-slots }()
				s.handle(ctx, conn)
			}()
		default:
			conn.Close()
		}
	}
}

func (s *Server) handle(parent context.Context, conn net.Conn) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetDeadline(time.Now().Add(handshakeTimeout))
	var selected record
	source := conn.RemoteAddr()
	config := &ssh.ServerConfig{
		MaxAuthTries: 3,
		PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			r, originalSource, err := s.store.authenticateTerminalSource(ctx, meta.User(), password, meta.RemoteAddr())
			if err != nil {
				return nil, ErrDenied
			}
			selected = r
			source = originalSource
			return &ssh.Permissions{}, nil
		},
	}
	config.AddHostKey(s.signer)
	down, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		s.store.logEvent("", conn.RemoteAddr().String(), "SSH 认证或握手失败")
		return
	}
	defer down.Close()
	conn.SetDeadline(time.Time{})
	go func() { down.Wait(); cancel() }()
	go ssh.DiscardRequests(requests) // 禁止远程端口转发等全局请求。

	// 认证后重新检查一次，避免握手期间修改配置后仍能连接。
	if !s.unchanged(ctx, selected, source) {
		return
	}
	go s.watch(ctx, cancel, selected, source)
	up, err := s.store.dial(ctx, selected)
	if err != nil {
		slog.Info("目标 SSH 连接失败", "目标", selected.ID, "错误", err)
		s.store.logEvent(selected.ID, source.String(), "目标 SSH 连接失败")
		return
	}
	defer up.Close()
	go func() { up.Wait(); cancel() }()
	s.active.Add(1)
	untrack := s.store.trackSSHConnection(selected.ID)
	defer untrack()
	defer s.active.Add(-1)
	s.store.logEvent(selected.ID, source.String(), "SSH 中转已连接")
	defer s.store.logEvent(selected.ID, source.String(), "SSH 中转已断开")

	var sessions sync.WaitGroup
	defer sessions.Wait()
	// 必须先关闭两端，才能等待可能阻塞于读写的会话。
	defer down.Close()
	defer up.Close()
	slots := make(chan struct{}, 16)
	for channel := range channels {
		if channel.ChannelType() != "session" {
			channel.Reject(ssh.Prohibited, "仅允许 SSH 会话")
			continue
		}
		select {
		case slots <- struct{}{}:
			sessions.Add(1)
			go func() {
				defer sessions.Done()
				defer func() { <-slots }()
				bridge(channel, up)
			}()
		default:
			channel.Reject(ssh.ResourceShortage, "会话数量已达上限")
		}
	}
}

// SQLite 允许命令行与服务分别运行；配置变更最多一秒内断开旧连接。
func (s *Server) watch(ctx context.Context, cancel context.CancelFunc, selected record, source net.Addr) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.unchanged(ctx, selected, source) {
				cancel()
				return
			}
		}
	}
}

func (s *Server) unchanged(ctx context.Context, selected record, source net.Addr) bool {
	current, err := s.store.get(ctx, "id", selected.ID)
	if err == nil && selected.relayID != "" {
		secrets, e := s.store.savedSecrets(current)
		if e != nil {
			return false
		}
		enabled := false
		for _, relay := range current.Relays {
			if relay.ID == selected.relayID && relay.Active() && relay.LoginID == selected.loginID {
				enabled = true
				current.hash = secrets.Relays[relay.ID]
			}
		}
		if !enabled {
			return false
		}
	}
	// 哈希还用于区分删除后用同一 ID 重建的目标，避免修订号重新从 1 开始。
	return err == nil && (!selected.isRelay || selected.relayID != "" || current.RelayExpiresAt == nil || time.Now().Before(*current.RelayExpiresAt)) && current.Enabled && current.Revision == selected.Revision && bytes.Equal(current.hash, selected.hash) && s.store.sourceAllowed(ctx, current.Target, source)
}

func (s *Store) dial(ctx context.Context, r record) (*ssh.Client, error) {
	auth, err := s.targetAuth(r)
	if err != nil {
		return nil, err
	}
	return dialSSH(ctx, net.JoinHostPort(r.Host, strconv.Itoa(r.Port)), r.User, auth, r.HostFingerprint)
}

func dialSSH(ctx context.Context, address, user string, auth ssh.AuthMethod, fingerprint string) (*ssh.Client, error) {
	dialer := net.Dialer{Timeout: handshakeTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(handshakeTimeout))
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{auth},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintSHA256(key) != fingerprint {
				return fmt.Errorf("目标 SSH 主机指纹不匹配")
			}
			return nil
		},
	}
	clientConn, channels, requests, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		stop()
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	client := ssh.NewClient(clientConn, channels, requests)
	go func() { client.Wait(); stop() }()
	return client, nil
}

// CheckTarget 测试目标认证信息和主机指纹，不执行远程命令。
func (s *Store) CheckTarget(ctx context.Context, id string) error {
	r, err := s.get(ctx, "id", id)
	if err != nil {
		return err
	}
	client, err := s.dial(ctx, r)
	if err != nil {
		return err
	}
	return client.Close()
}

func bridge(incoming ssh.NewChannel, client *ssh.Client) {
	up, upRequests, err := client.OpenChannel("session", incoming.ExtraData())
	if err != nil {
		incoming.Reject(ssh.ConnectionFailed, "无法打开目标会话")
		return
	}
	defer up.Close()
	down, downRequests, err := incoming.Accept()
	if err != nil {
		return
	}
	defer down.Close()
	var requestReply sync.Mutex
	var input sync.WaitGroup
	input.Add(2)
	go func() {
		defer input.Done()
		io.Copy(up, down)
		up.CloseWrite()
	}()
	go func() {
		defer input.Done()
		for request := range downRequests {
			requestReply.Lock()
			allowed := false
			switch request.Type {
			case "pty-req", "window-change", "shell", "exec", "signal", "env":
				allowed = true
			case "subsystem":
				var subsystem struct{ Name string }
				allowed = ssh.Unmarshal(request.Payload, &subsystem) == nil && subsystem.Name == "sftp"
			}
			ok := false
			if allowed {
				ok, _ = up.SendRequest(request.Type, request.WantReply, request.Payload)
			}
			if request.WantReply {
				request.Reply(ok, nil)
			}
			requestReply.Unlock()
		}
		// 客户端提前关闭会话时，也关闭对应的目标会话。
		up.Close()
	}()
	var output sync.WaitGroup
	output.Add(3)
	go func() { defer output.Done(); io.Copy(down, up) }()
	go func() { defer output.Done(); io.Copy(down.Stderr(), up.Stderr()) }()
	go func() {
		defer output.Done()
		for request := range upRequests {
			ok := false
			switch request.Type {
			case "exit-status", "exit-signal", "xon-xoff":
				ok, _ = down.SendRequest(request.Type, request.WantReply, request.Payload)
			}
			if request.WantReply {
				request.Reply(ok, nil)
			}
		}
	}()
	// 输出和退出码全部转发后才关闭下游，避免尾部输出丢失。
	output.Wait()
	// 快速命令可能在 exec 确认转发前结束；先发完确认，再关闭客户端通道。
	requestReply.Lock()
	down.Close()
	requestReply.Unlock()
	up.Close()
	input.Wait()
}

// ProbeFingerprint 仅探测主机公钥，在密码认证前断开，不自动信任该公钥。
func ProbeFingerprint(ctx context.Context, address string) (string, error) {
	conn, err := (&net.Dialer{Timeout: handshakeTimeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetDeadline(time.Now().Add(handshakeTimeout))
	var fingerprint string
	observed := errors.New("已读取主机指纹")
	_, _, _, err = ssh.NewClientConn(conn, address, &ssh.ClientConfig{
		User: "fingerprint-probe",
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			fingerprint = ssh.FingerprintSHA256(key)
			return observed
		},
	})
	if fingerprint != "" && errors.Is(err, observed) {
		return fingerprint, nil
	}
	return "", err
}
