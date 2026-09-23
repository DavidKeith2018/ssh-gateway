package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/pkg/sftp"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// 使用真实 TCP 和 SSH 握手模拟目标服务器，不需要系统 sshd 或真实机器凭证。
func startBackend(t *testing.T, allowedKeys ...ssh.PublicKey) (net.Addr, ssh.Signer, *atomic.Int64) {
	t.Helper()
	signer := testSigner(t)
	remoteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteDir, "app.yaml"), []byte("server:\n  port: 8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteDir, "README.md"), []byte("# 测试服务器\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// 演示环境使用内存文件系统，文件浏览和写入与宿主机隔离。
	var demoFiles *sftp.Handlers
	if os.Getenv("GATEWAY_DEMO_FIXTURE") == "1" {
		handlers := sftp.InMemHandler()
		for _, dir := range []string{"/etc", "/var", "/var/log", "/var/log/app", "/srv", "/srv/app", "/home", "/home/demo", "/opt", "/tmp"} {
			if err := handlers.FileCmd.Filecmd(&sftp.Request{Method: "Mkdir", Filepath: dir}); err != nil {
				t.Fatal(err)
			}
		}
		demoFiles = &handlers
	}
	var authCount atomic.Int64
	config := &ssh.ServerConfig{PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		authCount.Add(1)
		if meta.User() != "target-user" || string(password) != "target-secret-value" {
			return nil, errors.New("目标密码不匹配")
		}
		return nil, nil
	}}
	if len(allowedKeys) > 0 {
		config.PasswordCallback = nil
		config.PublicKeyCallback = func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			authCount.Add(1)
			for _, allowed := range allowedKeys {
				if meta.User() == "target-user" && bytes.Equal(key.Marshal(), allowed.Marshal()) {
					return nil, nil
				}
			}
			return nil, errors.New("目标公钥不匹配")
		}
	}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		var workers sync.WaitGroup
		defer workers.Wait()
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer raw.Close()
				stop := context.AfterFunc(ctx, func() { raw.Close() })
				defer stop()
				conn, channels, requests, err := ssh.NewServerConn(raw, config)
				if err != nil {
					return
				}
				defer conn.Close()
				go ssh.DiscardRequests(requests)
				var sessions sync.WaitGroup
				defer sessions.Wait()
				for incoming := range channels {
					if incoming.ChannelType() != "session" {
						incoming.Reject(ssh.Prohibited, "仅支持会话")
						continue
					}
					channel, reqs, err := incoming.Accept()
					if err != nil {
						continue
					}
					sessions.Add(1)
					go func() { defer sessions.Done(); backendSession(channel, reqs, remoteDir, demoFiles) }()
				}
			}()
		}
	}()
	t.Cleanup(func() {
		cancel()
		listener.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("目标测试服务器未关闭")
		}
	})
	return listener.Addr(), signer, &authCount
}

func backendSession(ch ssh.Channel, requests <-chan *ssh.Request, remoteDir string, demoFiles *sftp.Handlers) {
	defer ch.Close()
	finish := func(code uint32) {
		ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{code}))
	}
	for req := range requests {
		switch req.Type {
		case "subsystem":
			var payload struct{ Name string }
			if ssh.Unmarshal(req.Payload, &payload) != nil || payload.Name != "sftp" {
				req.Reply(false, nil)
				continue
			}
			req.Reply(true, nil)
			if demoFiles != nil {
				server := sftp.NewRequestServer(ch, *demoFiles, sftp.WithStartDirectory("/srv/app"))
				defer server.Close()
				server.Serve()
				return
			}
			server, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(remoteDir))
			if err != nil {
				return
			}
			defer server.Close()
			server.Serve()
			return
		case "exec":
			var payload struct{ Command string }
			if ssh.Unmarshal(req.Payload, &payload) != nil {
				req.Reply(false, nil)
				continue
			}
			req.Reply(true, nil)
			switch payload.Command {
			case hardwareCommand:
				io.WriteString(ch, "Linux\n__GW_HOST__\nfixture-host\n__GW_ARCH__\nx86_64\n__GW_KERNEL__\n6.8.0-test\n__GW_OS__\nPRETTY_NAME=\"测试 Linux 1.0\"\n__GW_CPU__\nprocessor : 0\nmodel name : Fixture CPU\nprocessor : 1\nmodel name : Fixture CPU\n__GW_MEMORY__\nMemTotal: 16777216 kB\n__GW_PRODUCT__\nTest Virtual Machine\n__GW_DISK__\nFilesystem 1024-blocks Used Available Capacity Mounted on\n/dev/root 104857600 44040192 60817408 42% /\n__GW_END__\n")
				finish(0)
				return
			case resourceCommand:
				tick := time.Now().UnixMilli() / 10
				fmt.Fprintf(ch, "Linux\ncpu %d 0 %d %d 0 0 0 0 0 0\nMemTotal: 16777216 kB\nMemAvailable: 10485760 kB\n__GW_DISK__\nFilesystem 1024-blocks Used Available Capacity Mounted on\n/dev/root 100000 42000 58000 42%% /\n", tick, tick, tick*4)
				finish(0)
				return
			case "result":
				io.WriteString(ch, "标准输出\n")
				io.WriteString(ch.Stderr(), "标准错误\n")
				finish(7)
				return
			case "large":
				io.WriteString(ch, strings.Repeat("o", 1<<20))
				io.WriteString(ch.Stderr(), strings.Repeat("e", 1<<19))
				finish(0)
				return
			case "cat":
				io.Copy(ch, ch)
				finish(0)
				return
			case "wait":
				// 等待客户端关闭，验证撤销能切断正在运行的会话。
			}
		case "pty-req":
			req.Reply(true, nil)
		case "shell":
			req.Reply(true, nil)
			io.WriteString(ch, "ready\n")
			go func() { io.Copy(ch, ch); finish(0); ch.Close() }()
		case "window-change":
			var size struct{ Columns, Rows, Width, Height uint32 }
			if ssh.Unmarshal(req.Payload, &size) == nil {
				fmt.Fprintf(ch, "%dx%d\n", size.Columns, size.Rows)
			}
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

type fixture struct {
	store   *Store
	dir     string
	input   PutInput
	address string
	signer  ssh.Signer
	cancel  context.CancelFunc
	done    chan error
	count   *atomic.Int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	address, targetSigner, count := startBackend(t)
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	in := testInput(t)
	in.Host, _, _ = net.SplitHostPort(address.String())
	_, port, _ := net.SplitHostPort(address.String())
	in.Port, _ = strconv.Atoi(port)
	in.HostFingerprint = ssh.FingerprintSHA256(targetSigner.PublicKey())
	if _, err := store.Put(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	signer, err := LoadHostKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{store: store, dir: dir, input: in, address: listener.Addr().String(), signer: signer, cancel: cancel, done: make(chan error, 1), count: count}
	go func() { f.done <- NewServer(store, f.signer).Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-f.done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("中转服务未关闭")
		}
	})
	return f
}

func (f *fixture) dial(user, password string) (*ssh.Client, error) {
	raw, err := net.DialTimeout("tcp", f.address, 3*time.Second)
	if err != nil {
		return nil, err
	}
	raw.SetDeadline(time.Now().Add(10 * time.Second))
	conn, channels, requests, err := ssh.NewClientConn(raw, f.address, &ssh.ClientConfig{
		User: user, Auth: []ssh.AuthMethod{ssh.Password(password)}, HostKeyCallback: ssh.FixedHostKey(f.signer.PublicKey()),
	})
	if err != nil {
		raw.Close()
		return nil, err
	}
	return ssh.NewClient(conn, channels, requests), nil
}

func (f *fixture) connect(t *testing.T) *ssh.Client {
	t.Helper()
	client, err := f.dial(f.input.RelayUser, f.input.RelayPassword)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestSSHBridge(t *testing.T) {
	f := newFixture(t)
	client := f.connect(t)
	t.Run("输出与退出码", func(t *testing.T) {
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		var stdout, stderr bytes.Buffer
		session.Stdout, session.Stderr = &stdout, &stderr
		err = session.Run("result")
		var exit *ssh.ExitError
		if !errors.As(err, &exit) || exit.ExitStatus() != 7 || stdout.String() != "标准输出\n" || stderr.String() != "标准错误\n" {
			t.Fatalf("转发结果不完整：%v，%q，%q", err, stdout.String(), stderr.String())
		}
	})
	t.Run("大输出不截断", func(t *testing.T) {
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		var stdout, stderr bytes.Buffer
		session.Stdout, session.Stderr = &stdout, &stderr
		if err := session.Run("large"); err != nil {
			t.Fatal(err)
		}
		if stdout.String() != strings.Repeat("o", 1<<20) || stderr.String() != strings.Repeat("e", 1<<19) {
			t.Fatal("大输出被截断或损坏")
		}
	})
	t.Run("标准输入半关闭", func(t *testing.T) {
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		session.Stdin = strings.NewReader("输入内容\n")
		out, err := session.Output("cat")
		if err != nil || string(out) != "输入内容\n" {
			t.Fatalf("输入转发失败：%v，%q", err, out)
		}
	})
	t.Run("交互终端与窗口变化", func(t *testing.T) {
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		stdin, _ := session.StdinPipe()
		stdout, _ := session.StdoutPipe()
		if err := session.RequestPty("xterm", 24, 80, ssh.TerminalModes{ssh.ECHO: 0}); err != nil {
			t.Fatal(err)
		}
		if err := session.Shell(); err != nil {
			t.Fatal(err)
		}
		read := func(want string) {
			t.Helper()
			buf := make([]byte, len(want))
			if _, err := io.ReadFull(stdout, buf); err != nil || string(buf) != want {
				t.Fatalf("终端返回=%q，预期=%q，错误=%v", buf, want, err)
			}
		}
		read("ready\n")
		if err := session.WindowChange(40, 120); err != nil {
			t.Fatal(err)
		}
		read("120x40\n")
		io.WriteString(stdin, "hello\n")
		read("hello\n")
		stdin.Close()
		if err := session.Wait(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("拒绝转发与未知子系统", func(t *testing.T) {
		if channel, _, err := client.OpenChannel("direct-tcpip", nil); err == nil {
			channel.Close()
			t.Fatal("不应允许端口转发")
		}
		if ok, _, err := client.SendRequest("tcpip-forward", true, nil); err != nil || ok {
			t.Fatalf("应拒绝远程转发：%v", err)
		}
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		if err := session.RequestSubsystem("unsupported"); err == nil {
			t.Fatal("不应允许未知子系统")
		}
		if ok, err := session.SendRequest("auth-agent-req@openssh.com", true, nil); err != nil || ok {
			t.Fatalf("应拒绝 Agent 转发：%v", err)
		}
	})
}

func TestSSHAuthenticationAndSourceRestriction(t *testing.T) {
	f := newFixture(t)
	for _, credentials := range [][2]string{{f.input.RelayUser, "wrong"}, {"unknown", f.input.RelayPassword}} {
		if client, err := f.dial(credentials[0], credentials[1]); err == nil {
			client.Close()
			t.Fatal("错误凭证通过了认证")
		}
	}
	f.input.AllowedSources = []string{"192.0.2.1"}
	if _, err := f.store.Put(context.Background(), f.input); err != nil {
		t.Fatal(err)
	}
	if client, err := f.dial(f.input.RelayUser, f.input.RelayPassword); err == nil {
		client.Close()
		t.Fatal("不在白名单的来源通过了认证")
	}
	if f.count.Load() != 0 {
		t.Fatal("未授权客户端不应触发目标密码认证")
	}
}

func TestTargetFingerprintAndProbe(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	address := net.JoinHostPort(f.input.Host, strconv.Itoa(f.input.Port))
	fp, err := ProbeFingerprint(ctx, address)
	if err != nil || fp != f.input.HostFingerprint || f.count.Load() != 0 {
		t.Fatalf("指纹探测失败或触发密码认证：%v", err)
	}
	if err := f.store.CheckTarget(ctx, f.input.ID); err != nil {
		t.Fatal(err)
	}
	before := f.count.Load()
	f.input.HostFingerprint = ssh.FingerprintSHA256(testSigner(t).PublicKey())
	if _, err := f.store.Put(ctx, f.input); err != nil {
		t.Fatal(err)
	}
	if err := f.store.CheckTarget(ctx, f.input.ID); err == nil {
		t.Fatal("不应接受错误的目标指纹")
	}
	client := f.connect(t)
	if session, err := client.NewSession(); err == nil {
		session.Close()
		t.Fatal("指纹不匹配时不应建立目标会话")
	}
	if f.count.Load() != before {
		t.Fatal("指纹不匹配时不应发送目标密码")
	}
}

func TestLiveConfigurationRevocation(t *testing.T) {
	for _, action := range []string{"修改来源", "修改密码", "禁用", "删除", "删除重建"} {
		t.Run(action, func(t *testing.T) {
			f := newFixture(t)
			client := f.connect(t)
			session, err := client.NewSession()
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if err := session.Start("wait"); err != nil {
				t.Fatal(err)
			}
			// 独立的 Store 模拟另一个命令行进程更新数据库。
			other, err := OpenStore(f.dir)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			ctx := context.Background()
			switch action {
			case "修改来源":
				f.input.AllowedSources = []string{"192.0.2.1"}
			case "修改密码":
				f.input.RelayPassword = "changed-relay-password"
			case "禁用":
				f.input.Enabled = false
			case "删除", "删除重建":
				if err := other.Delete(ctx, f.input.ID); err != nil {
					t.Fatal(err)
				}
			}
			if action != "删除" {
				if _, err := other.Put(ctx, f.input); err != nil {
					t.Fatal(err)
				}
			}
			ended := make(chan error, 1)
			go func() { ended <- session.Wait() }()
			select {
			case err := <-ended:
				if err == nil {
					t.Fatal("撤销应中断会话")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("配置更新后旧会话未断开")
			}
			if action == "修改密码" || action == "删除重建" {
				f.connect(t)
			}
		})
	}
}

func TestShutdownClosesActiveAndUnauthenticatedConnections(t *testing.T) {
	f := newFixture(t)
	client := f.connect(t)
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.Start("wait"); err != nil {
		t.Fatal(err)
	}
	raw, err := net.Dial("tcp", f.address)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	f.cancel()
	ended := make(chan error, 1)
	go func() { ended <- session.Wait() }()
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("停止服务后活动会话仍未关闭")
	}
	raw.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, err = io.Copy(io.Discard, raw)
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("停止服务后未认证连接仍未关闭")
	}
}
