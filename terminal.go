package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
)

// TerminalSession 共用于浏览器和桌面；调用者负责管理员认证。
type TerminalSession struct {
	session *ssh.Session
	client  *ssh.Client
	input   io.WriteCloser
	cancel  context.CancelFunc
	once    sync.Once
	untrack func()
}

func (s *Store) OpenTerminal(parent context.Context, id string, source net.Addr, output io.Writer, selection ...string) (*TerminalSession, error) {
	return s.openTerminal(parent, id, source, output, nil, selection...)
}

// TerminalEndpoint 指定中转入口；密码只在后端按所选账号解密读取。
type TerminalEndpoint struct {
	Address     string
	Fingerprint string
}

func (s *Store) openTerminal(parent context.Context, id string, source net.Addr, output io.Writer, endpoint *TerminalEndpoint, selection ...string) (*TerminalSession, error) {
	ctx, cancel := context.WithCancel(parent)
	chosen := ""
	if len(selection) > 0 {
		chosen = selection[0]
	}
	r, err := s.terminalRecord(ctx, id, chosen, endpoint != nil)
	if err != nil || !r.Enabled || !s.sourceAllowed(ctx, r.Target, source) {
		cancel()
		return nil, ErrDenied
	}
	go NewServer(s, nil).watch(ctx, cancel, r, source)
	var client *ssh.Client
	if endpoint != nil && !strings.HasPrefix(chosen, "server:") {
		relayID := chosen
		if relayID == "" {
			relayID = r.relayID
		}
		if relayID == "" {
			relayID = "default"
		}
		passwords, passwordErr := s.relayPasswords(ctx, id)
		if passwordErr != nil {
			cancel()
			return nil, passwordErr
		}
		if passwords[relayID] == "" {
			cancel()
			return nil, fmt.Errorf("所选中转账号没有可用密码，请重新保存中转凭证")
		}
		password, release, authErr := s.terminalSourcePassword(ctx, r, source, passwords[relayID])
		if authErr != nil {
			cancel()
			return nil, authErr
		}
		defer release()
		client, err = dialSSH(ctx, endpoint.Address, r.RelayUser, ssh.Password(password), endpoint.Fingerprint)
	} else {
		client, err = s.dial(ctx, r)
	}
	if err != nil {
		cancel()
		return nil, err
	}
	session, err := client.NewSession()
	if err != nil {
		cancel()
		client.Close()
		return nil, err
	}
	input, err := session.StdinPipe()
	if err != nil {
		cancel()
		session.Close()
		client.Close()
		return nil, err
	}
	term := &TerminalSession{session: session, client: client, input: input, cancel: cancel}
	session.Stdout, session.Stderr = output, output
	if err := session.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{ssh.ECHO: 1}); err != nil {
		term.Close()
		return nil, err
	}
	if err := session.Shell(); err != nil {
		term.Close()
		return nil, err
	}
	// 经中转入口的终端已由 SSH 服务端计数，避免同一连接统计两次。
	if endpoint == nil || strings.HasPrefix(chosen, "server:") {
		term.untrack = s.trackSSHConnection(id)
	}
	return term, nil
}
func (t *TerminalSession) Write(data string) error {
	_, err := io.WriteString(t.input, data)
	return err
}
func (t *TerminalSession) Resize(columns, rows int) error {
	if columns < 1 || columns > 500 || rows < 1 || rows > 300 {
		return fmt.Errorf("终端尺寸无效")
	}
	return t.session.WindowChange(rows, columns)
}
func (t *TerminalSession) Wait() error { return t.session.Wait() }
func (t *TerminalSession) Close() {
	t.once.Do(func() {
		t.cancel()
		t.session.Close()
		t.client.Close()
		if t.untrack != nil {
			t.untrack()
		}
	})
}

// 只返回当前拨号的公开信息，不包含密码或私钥。
func terminalConnectionInfo(r record, endpoint *TerminalEndpoint, shared ...string) string {
	mode, user, host, port := "server", r.User, r.Host, strconv.Itoa(r.Port)
	if endpoint != nil {
		mode, user = "relay", r.RelayUser
		host, port, _ = net.SplitHostPort(endpoint.Address)
	}
	info := map[string]string{"mode": mode, "user": user, "host": host, "port": port}
	if len(shared) > 0 {
		info["shared_id"] = shared[0]
	}
	data, _ := json.Marshal(info)
	return string(data)
}
