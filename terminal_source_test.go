package gateway

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestTerminalRelayPreservesSourceAndRevocation(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	in := f.input
	in.AllowedSources = []string{"192.0.2.10"}
	if _, err := f.store.PutTarget(ctx, in); err != nil {
		t.Fatal(err)
	}
	// 真实 TCP 拨号来自回环地址；普通 SSH 不能借用网页客户端的授权。
	if client, err := f.dial(in.RelayUser, in.RelayPassword); err == nil {
		client.Close()
		t.Fatal("普通 SSH 绕过了来源限制")
	}
	endpoint := &TerminalEndpoint{Address: f.address, Fingerprint: ssh.FingerprintSHA256(f.signer.PublicKey())}
	if term, err := f.store.openTerminal(ctx, in.ID, &net.TCPAddr{IP: net.ParseIP("192.0.2.11")}, io.Discard, endpoint); err == nil {
		term.Close()
		t.Fatal("未授权网页来源被接受")
	}
	term, err := f.store.openTerminal(ctx, in.ID, &net.TCPAddr{IP: net.ParseIP("192.0.2.10")}, io.Discard, endpoint)
	if err != nil {
		t.Fatalf("已授权网页来源被中转拒绝：%v", err)
	}
	defer term.Close()
	f.store.terminalSourceMu.Lock()
	remaining := len(f.store.terminalSources)
	f.store.terminalSourceMu.Unlock()
	if remaining != 0 {
		t.Fatal("成功握手后一次性凭据仍残留")
	}
	in.AllowedSources = []string{"192.0.2.11"}
	if _, err := f.store.PutTarget(ctx, in); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- term.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("原始来源撤权后终端未关闭")
	}
}

func TestTerminalSourceTicketAuthentication(t *testing.T) {
	for _, scenario := range []string{"正常及重放", "伪造", "账号不匹配", "密码错误", "过期", "请求取消", "配置变更", "来源未授权", "握手取消清理"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			selected, err := f.store.connectionRecord(ctx, f.input.ID, "default")
			if err != nil {
				t.Fatal(err)
			}
			source := &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}
			peer := &net.TCPAddr{IP: net.ParseIP("192.0.2.99")}
			password, release, err := f.store.terminalSourcePassword(ctx, selected, source, f.input.RelayPassword)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			key, _, _ := strings.Cut(strings.TrimPrefix(password, terminalSourcePrefix), ":")
			user := selected.RelayUser
			switch scenario {
			case "伪造":
				password = terminalSourcePrefix + strings.Repeat("0", 64) + ":" + f.input.RelayPassword
			case "账号不匹配":
				user = "another-user"
			case "密码错误":
				password = terminalSourcePrefix + key + ":wrong-password"
			case "过期", "来源未授权":
				f.store.terminalSourceMu.Lock()
				ticket := f.store.terminalSources[key]
				if scenario == "过期" {
					ticket.expires = time.Now().Add(-time.Second)
				} else {
					ticket.source = peer
				}
				f.store.terminalSources[key] = ticket
				f.store.terminalSourceMu.Unlock()
			case "请求取消":
				cancel()
			case "握手取消清理":
				release()
			case "配置变更":
				in := f.input
				in.Name = "配置已更新"
				if _, err := f.store.PutTarget(ctx, in); err != nil {
					t.Fatal(err)
				}
			}
			r, original, err := f.store.authenticateTerminalSource(context.Background(), user, []byte(password), peer)
			if scenario == "正常及重放" {
				if err != nil || r.ID != selected.ID || original.String() != source.String() {
					t.Fatalf("原始来源未恢复：%v", err)
				}
				if _, _, err := f.store.authenticateTerminalSource(ctx, user, []byte(password), peer); err == nil {
					t.Fatal("一次性凭据可重放")
				}
			} else if err == nil {
				t.Fatal("无效来源凭据被接受")
			}
		})
	}
}
