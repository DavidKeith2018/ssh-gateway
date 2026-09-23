package gateway

import (
	"context"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestTerminalUsesSelectedRoute(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	source := &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}
	endpoint := &TerminalEndpoint{Address: f.address, Fingerprint: ssh.FingerprintSHA256(f.signer.PublicKey())}
	term, err := f.store.openTerminal(ctx, f.input.ID, source, io.Discard, endpoint, "default")
	if err != nil {
		t.Fatal(err)
	}
	term.Close()
	// 中转入口不可用时禁止绕过中转，目标认证次数也不应增加。
	before := f.count.Load()
	bad := &TerminalEndpoint{Address: "127.0.0.1:0", Fingerprint: endpoint.Fingerprint}
	if term, err := f.store.openTerminal(ctx, f.input.ID, source, io.Discard, bad, "default"); err == nil {
		term.Close()
		t.Fatal("中转失败后不应直连目标")
	}
	if f.count.Load() != before {
		t.Fatal("中转失败后仍连接了目标")
	}
	// 服务器模式明确直连，不依赖中转入口。
	term, err = f.store.openTerminal(ctx, f.input.ID, source, io.Discard, bad, "server:default")
	if err != nil {
		t.Fatal(err)
	}
	term.Close()
	// 中转入口必须校验自己的主机指纹，不能误用目标指纹或跳过校验。
	wrongKey := &TerminalEndpoint{Address: f.address, Fingerprint: f.input.HostFingerprint}
	if term, err := f.store.openTerminal(ctx, f.input.ID, source, io.Discard, wrongKey, "default"); err == nil {
		term.Close()
		t.Fatal("接受了错误中转指纹")
	}
}

func TestDefaultMultiAccountTerminal(t *testing.T) {
	d := freshDesktop(t)
	loginDesktop(t, d)
	in := multiLoginInput(t)
	if _, err := d.store.PutTarget(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"ubuntu", "root"} {
		var connection string
		if err := d.OpenTerminal("default-multi", in.ID, func(event TerminalEvent) {
			if event.Type == "connection" {
				connection = event.Message
			}
		}); err != nil {
			t.Fatalf("默认账号 %s 连接失败：%v", user, err)
		}
		d.CloseTerminal("default-multi")
		selected, err := d.store.terminalRecord(context.Background(), in.ID, "", true)
		if err != nil || selected.User != user || !strings.Contains(connection, selected.RelayUser) {
			t.Fatalf("默认账号与连接信息不匹配：%+v %s %v", selected.Target, connection, err)
		}
		// 依次禁用首个可用账号；后续连接应选择下一个，全部禁用则拒绝。
		target, _ := d.store.Get(context.Background(), in.ID)
		update := PutInput{Target: target}
		for _, relay := range target.Relays {
			if relay.ID == selected.relayID {
				relay.Enabled = false
			}
			update.RelayInputs = append(update.RelayInputs, TargetRelayInput{TargetRelay: relay})
		}
		if _, err := d.store.PutTarget(context.Background(), update); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.OpenTerminal("default-multi", in.ID, func(TerminalEvent) {}); err == nil {
		d.CloseTerminal("default-multi")
		t.Fatal("全部中转账号禁用后仍可默认连接")
	}
	// 原有直连接口仍使用 DefaultLoginID，不依赖中转账号是否启用。
	term, err := d.store.OpenTerminal(context.Background(), in.ID, &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	term.Close()
}
