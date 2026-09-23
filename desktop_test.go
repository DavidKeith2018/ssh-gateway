package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/ssh"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func desktopCall(t *testing.T, d *Desktop, method, path string, body any) DesktopReply {
	t.Helper()
	data, _ := json.Marshal(body)
	reply, err := d.Call(method, path, string(data))
	if err != nil {
		t.Fatal(err)
	}
	return reply
}
func freshDesktop(t *testing.T) *Desktop {
	t.Helper()
	d, err := OpenDesktop(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d
}
func loginDesktop(t *testing.T, d *Desktop) {
	t.Helper()
	if d.Info().NeedsSetup {
		reply := desktopCall(t, d, "POST", "/desktop/setup", map[string]string{"password": testAdminPassword})
		if reply.Status != 200 {
			t.Fatal(string(reply.Data))
		}
	}
	if reply := desktopCall(t, d, "POST", "/login", map[string]string{"username": adminUsername, "password": testAdminPassword}); reply.Status != 200 {
		t.Fatal(string(reply.Data))
	}
}
func TestDesktopSetupLockSettingsAndRestart(t *testing.T) {
	d := freshDesktop(t)
	if !d.Info().NeedsSetup || d.Info().Listen != "" {
		t.Fatal("首次启动应等待设置管理员密码")
	}
	if reply := desktopCall(t, d, "GET", "/targets", nil); reply.Status != 401 {
		t.Fatal("桌面管理必须登录")
	}
	if second, err := OpenDesktop(context.Background(), d.dir); err == nil {
		second.Close()
		t.Fatal("重复服务未被拒绝")
	}
	loginDesktop(t, d)
	if _, err := d.Call("POST", "/desktop/setup", `{"password":"another-password"}`); err == nil {
		t.Fatal("不应覆盖已有管理员")
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port
	settings := DesktopSettings{Port: port, ConnectHost: "127.0.0.1"}
	if err := d.SaveSettings(settings, true); err != nil {
		t.Fatal(err)
	}
	if d.Info().Error == "" || d.Info().Listen != "" {
		t.Fatal("应显示端口占用错误")
	}
	occupied.Close()
	if err := d.SaveSettings(settings, true); err != nil || d.Info().Listen == "" {
		t.Fatalf("修改端口后未恢复：%v", err)
	}
	dir := d.dir
	d.Close()
	next, err := OpenDesktop(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.Info().NeedsSetup || next.Info().Settings.Port != port || next.Info().Listen != "" {
		t.Fatal("重启丢失配置或未释放端口")
	}
	if reply := desktopCall(t, next, "POST", "/unlock", map[string]string{"password": testAdminPassword}); reply.Status != 200 || next.Info().Listen == "" {
		t.Fatal("unlock did not start listener")
	}
}

func TestDesktopTerminalAndRevocation(t *testing.T) {
	address, signer, _ := startBackend(t)
	d := freshDesktop(t)
	loginDesktop(t, d)
	in := testInput(t)
	in.Host = "127.0.0.1"
	_, p, _ := net.SplitHostPort(address.String())
	in.Port, _ = strconv.Atoi(p)
	in.HostFingerprint = fmt.Sprint(ssh.FingerprintSHA256(signer.PublicKey()))
	if reply := desktopCall(t, d, "POST", "/targets", in); reply.Status != 200 {
		t.Fatal(string(reply.Data))
	}
	events := make(chan TerminalEvent, 64)
	emit := func(e TerminalEvent) {
		events <- e
		if e.Type == "output" {
			d.AckTerminal(e.ID)
		}
	}
	if err := d.OpenTerminal("desktop-test", in.ID, emit); err != nil {
		t.Fatal(err)
	}
	defer d.CloseTerminal("desktop-test")
	if err := d.TerminalInput("desktop-test", "中文终端\n", 0, 0); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	timeout := time.After(5 * time.Second)
	for !strings.Contains(output.String(), "中文终端") {
		select {
		case event := <-events:
			if event.Type == "output" {
				raw, _ := base64.StdEncoding.DecodeString(event.Data)
				output.Write(raw)
			}
		case <-timeout:
			t.Fatal("终端未返回中文输出")
		}
	}
	if err := d.SaveSettings(d.settings, false); err == nil {
		t.Fatal("活动连接时必须确认重启")
	}
	desktopCall(t, d, "POST", "/logout", nil)
	if err := d.TerminalInput("desktop-test", "不能继续", 0, 0); err == nil {
		t.Fatal("退出后仍可输入")
	}
	if err := d.OpenTerminal("not-auth", in.ID, emit); err == nil {
		t.Fatal("未登录不能打开终端")
	}
	loginDesktop(t, d)
	in.AllowedSources = []string{"192.0.2.1"}
	current, err := d.store.Get(context.Background(), in.ID)
	if err != nil {
		t.Fatal(err)
	}
	in.Revision = current.Revision
	desktopCall(t, d, "PUT", "/targets/"+in.ID, in)
	if err := d.OpenTerminal("wrong-ip", in.ID, emit); err == nil {
		t.Fatal("桌面终端绕过来源限制")
	}
}
