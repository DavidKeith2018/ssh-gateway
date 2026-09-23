package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func startLoginBackend(t *testing.T, key ssh.PublicKey) (net.Addr, ssh.Signer) {
	t.Helper()
	hostKey := testSigner(t)
	config := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if meta.User() == "ubuntu" && string(password) == "ubuntu-password" {
				return nil, nil
			}
			return nil, fmt.Errorf("目标账号密码不匹配")
		},
		PublicKeyCallback: func(meta ssh.ConnMetadata, public ssh.PublicKey) (*ssh.Permissions, error) {
			if meta.User() == "root" && bytes.Equal(public.Marshal(), key.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("目标账号密钥不匹配")
		},
	}
	config.AddHostKey(hostKey)
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
				for incoming := range channels {
					channel, requests, err := incoming.Accept()
					if err != nil {
						continue
					}
					for request := range requests {
						if request.Type == "pty-req" || request.Type == "window-change" {
							request.Reply(true, nil)
							continue
						}
						if request.Type == "shell" {
							request.Reply(true, nil)
							io.WriteString(channel, conn.User()+"\n")
							go func() { io.Copy(channel, channel); channel.Close() }()
							continue
						}
						if request.Type == "exec" {
							request.Reply(true, nil)
							io.WriteString(channel, conn.User()+"\n")
							channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{0}))
							channel.Close()
							break
						}
						request.Reply(false, nil)
					}
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
			t.Error("测试服务器未关闭")
		}
	})
	return listener.Addr(), hostKey
}

func multiLoginInput(t *testing.T) PutInput {
	t.Helper()
	key, public := testPrivateKey(t, "root-key-password")
	addr, hostKey := startLoginBackend(t, public)
	in := testInput(t)
	in.Host = "127.0.0.1"
	in.Port = addr.(*net.TCPAddr).Port
	in.HostFingerprint = ssh.FingerprintSHA256(hostKey.PublicKey())
	in.TargetPassword = ""
	in.RelayPassword = ""
	in.DefaultLoginID = "ubuntu-login"
	in.LoginInputs = []TargetLoginInput{
		{TargetLogin: TargetLogin{ID: "ubuntu-login", User: "ubuntu", AuthType: "password"}, TargetPassword: "ubuntu-password"},
		{TargetLogin: TargetLogin{ID: "root-login", User: "root", AuthType: "private_key"}, TargetPrivateKey: key, TargetKeyPassphrase: "root-key-password"},
	}
	in.RelayInputs = []TargetRelayInput{
		{TargetRelay: TargetRelay{ID: "ubuntu-relay", LoginID: "ubuntu-login", Enabled: true}},
		{TargetRelay: TargetRelay{ID: "root-relay", LoginID: "root-login", Enabled: true}},
	}
	return in
}

func TestMultipleTargetLoginsAndRelayBinding(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	in := multiLoginInput(t)
	result, err := f.store.PutTarget(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Credentials) != 2 || result.Target == nil {
		t.Fatal("未返回全部自动创建的凭证")
	}
	for i, credential := range result.Credentials {
		if len(credential.Password) != 32 || !strings.HasPrefix(credential.Username, "relay-") {
			t.Fatal("自动生成的中转凭证无效")
		}
		client, err := f.dial(credential.Username, credential.Password)
		if err != nil {
			t.Fatal(err)
		}
		for attempt := 0; attempt < 30; attempt++ {
			session, err := client.NewSession()
			if err != nil {
				t.Fatal(err)
			}
			output, err := session.Output("whoami")
			session.Close()
			expected := []string{"ubuntu\n", "root\n"}[i]
			if err != nil || string(output) != expected {
				client.Close()
				t.Fatalf("中转账号 %s 第 %d 次命令失败：%q，%v", strings.TrimSpace(expected), attempt+1, output, err)
			}
		}
		client.Close()
	}
	target, err := f.store.Get(ctx, in.ID)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(target)
	for _, secret := range []string{"ubuntu-password", "root-key-password", in.LoginInputs[1].TargetPrivateKey, result.Credentials[0].Password, "target_password", "target_private_key"} {
		if bytes.Contains(public, []byte(secret)) {
			t.Fatal("查询接口泄露凭证")
		}
	}
	// 空白更新保留全部凭证，默认账号可以切换到密钥登录。
	update := PutInput{Target: target}
	update.DefaultLoginID = "root-login"
	if _, err = f.store.PutTarget(ctx, update); err != nil {
		t.Fatal(err)
	}
	selected, err := f.store.get(ctx, "id", in.ID)
	if err != nil {
		t.Fatal(err)
	}
	client, err := f.store.dial(ctx, selected)
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	output, err := session.Output("whoami")
	session.Close()
	client.Close()
	if err != nil || string(output) != "root\n" {
		t.Fatalf("默认账号切换失败：%q %v", output, err)
	}
	// 禁用单组凭证应拒绝新连接，并使已有绑定失效，另一组继续可用。
	source := &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}
	original, err := f.store.authenticate(ctx, result.Credentials[1].Username, []byte(result.Credentials[1].Password), source)
	if err != nil {
		t.Fatal(err)
	}
	target, _ = f.store.Get(ctx, in.ID)
	update = PutInput{Target: target}
	for _, relay := range target.Relays {
		update.RelayInputs = append(update.RelayInputs, TargetRelayInput{TargetRelay: relay})
	}
	update.RelayInputs[1].Enabled = false
	if _, err = f.store.PutTarget(ctx, update); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.authenticate(ctx, result.Credentials[1].Username, []byte(result.Credentials[1].Password), source); err == nil {
		t.Fatal("无效凭证仍可认证")
	}
	if NewServer(f.store, nil).unchanged(ctx, original, source) {
		t.Fatal("被禁用凭证的旧连接未失效")
	}
	if _, err = f.store.authenticate(ctx, result.Credentials[0].Username, []byte(result.Credentials[0].Password), source); err != nil {
		t.Fatal("禁用一组误伤其他凭证")
	}
	target, _ = f.store.Get(ctx, in.ID)
	target.Enabled = false
	if _, err = f.store.PutTarget(ctx, PutInput{Target: target}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.authenticate(ctx, result.Credentials[0].Username, []byte(result.Credentials[0].Password), source); err == nil {
		t.Fatal("无效机器仍可连接")
	}
}

func TestMultipleTargetPersistenceAndValidation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := multiLoginInput(t)
	in.ID = ""
	result, err := s.PutTarget(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !identifier.MatchString(result.ID) || result.ID == "" {
		t.Fatal("没有自动生成机器编号")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.CheckTarget(ctx, result.ID); err != nil {
		t.Fatal(err)
	}
	target, err := s.Get(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 重名检查覆盖同机器、跨机器，以及原来的单账号写入路径。
	duplicate := testInput(t)
	duplicate.ID = "collision"
	duplicate.RelayUser = target.Relays[1].Username
	if _, err = s.Put(ctx, duplicate); err == nil {
		t.Fatal("旧接口抢占了第二组中转用户名")
	}
	for _, change := range []func(*PutInput){
		func(i *PutInput) { i.RelayInputs[0].LoginID = "missing" },
		func(i *PutInput) { i.RelayInputs[1].Username = i.RelayInputs[0].Username },
		func(i *PutInput) { i.RelayInputs[0].Password = "short" },
		func(i *PutInput) { i.LoginInputs = []TargetLoginInput{} },
		func(i *PutInput) { i.DefaultLoginID = "missing" },
	} {
		bad := PutInput{Target: target}
		for _, relay := range target.Relays {
			bad.RelayInputs = append(bad.RelayInputs, TargetRelayInput{TargetRelay: relay})
		}
		change(&bad)
		if _, err = s.PutTarget(ctx, bad); err == nil {
			t.Fatal("无效多账号配置未被拒绝")
		}
		current, _ := s.Get(ctx, target.ID)
		if current.Revision != target.Revision {
			t.Fatal("失败保存不应部分修改机器")
		}
	}
	if err = s.CheckTarget(ctx, result.ID); err != nil {
		t.Fatal("失败更新破坏了原凭证")
	}
}

func TestLegacyTargetBecomesMultipleLogins(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	target, err := f.store.Get(ctx, f.input.ID)
	if err != nil {
		t.Fatal(err)
	}
	in := PutInput{Target: target, LoginInputs: []TargetLoginInput{{TargetLogin: TargetLogin{ID: "default", User: target.User, AuthType: target.AuthType}}}, RelayInputs: []TargetRelayInput{{TargetRelay: TargetRelay{ID: "default", Username: target.RelayUser, LoginID: "default", Enabled: true}}}}
	result, err := f.store.PutTarget(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Credentials) != 0 {
		t.Fatal("编辑旧机器不应重设中转密码")
	}
	if err = f.store.CheckTarget(ctx, target.ID); err != nil {
		t.Fatal("迁移后目标认证失败")
	}
	if _, err = f.store.authenticate(ctx, f.input.RelayUser, []byte(f.input.RelayPassword), &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}); err != nil {
		t.Fatal("迁移后中转认证失败")
	}
}
