//go:build linux && native_smoke

package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"golang.org/x/crypto/ssh"
	gateway "ssh-gateway"
)

// 在独立 DBus 会话和 Xvfb 中运行，不访问用户的数据或真实 SSH 目标。
// 使用 go build 而非 go test，确保服务类型的包路径与正式程序 main.App 一致。
func main() {
	if os.Getenv("GATEWAY_SMOKE_SECOND_INSTANCE") == "1" {
		runDesktop()
		return
	}
	if err := nativeSmoke(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("PASS：v3 原生桥接、托盘隐藏与恢复、第二实例、SSH 会话持续、取消退出及端口释放")
}

type smokeWatcher struct{ props *prop.Properties }

func (w *smokeWatcher) RegisterStatusNotifierItem(sender dbus.Sender, path string) *dbus.Error {
	w.props.SetMust("org.kde.StatusNotifierWatcher", "RegisteredStatusNotifierItems", []string{string(sender) + path})
	return nil
}

func nativeSmoke() error {
	dir, err := os.MkdirTemp("", "ssh-gateway-tray-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	bus, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer bus.Close()
	watcher := &smokeWatcher{}
	const iface = "org.kde.StatusNotifierWatcher"
	watcher.props, err = prop.Export(bus, "/StatusNotifierWatcher", map[string]map[string]*prop.Prop{
		iface: {
			"IsStatusNotifierHostRegistered": {Value: true},
			"RegisteredStatusNotifierItems":  {Value: []string{}},
		},
	})
	if err != nil {
		return err
	}
	if err = bus.Export(watcher, "/StatusNotifierWatcher", iface); err != nil {
		return err
	}
	if _, err = bus.RequestName(iface, dbus.NameFlagDoNotQueue); err != nil {
		return err
	}
	app := newDesktopApp(dir)
	result := make(chan error, 1)
	var address string
	go func() {
		check := func() error {
			if err := smokeWait(func() bool { app.exit.mu.Lock(); defer app.exit.mu.Unlock(); return app.exit.frontendReady }); err != nil {
				return fmt.Errorf("前端握手失败：%w", err)
			}
			if err := smokeWait(trayAvailable); err != nil {
				return fmt.Errorf("托盘注册失败：%w", err)
			}
			app.window.ExecJS(`const timer = setInterval(() => {
 const input = document.querySelector('#admin-password');
 if (!input) return;
 clearInterval(timer); input.value = 'native-smoke-password'; input.dispatchEvent(new Event('input', {bubbles:true}));
 const saved = input.closest('form').querySelector('input[type=checkbox][required]'); if(saved && !saved.checked) saved.click();
 input.closest('form').dispatchEvent(new Event('submit', {bubbles:true, cancelable:true}));
}, 100);`)
			if err := smokeWait(func() bool { reply, _ := app.Call("GET", "/me", ""); return reply.Status == 200 }); err != nil {
				return fmt.Errorf("原生绑定设置或登录失败：%w", err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return err
			}
			port := listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			if err := app.SaveSettings(gateway.DesktopSettings{Port: port, ConnectHost: "127.0.0.1"}, true); err != nil {
				return err
			}
			address = app.Info().Listen
			backend, signer, err := smokeBackend()
			if err != nil {
				return err
			}
			defer backend.Close()
			target := gateway.PutInput{Target: gateway.Target{ID: "tray-smoke", Name: "托盘测试", Host: "127.0.0.1", Port: backend.Addr().(*net.TCPAddr).Port, User: "test", RelayUser: "tray-test", HostFingerprint: ssh.FingerprintSHA256(signer.PublicKey()), AllowedSources: []string{"127.0.0.1"}, Enabled: true}, TargetPassword: "backend-password", RelayPassword: "tray-test-password"}
			body, _ := json.Marshal(target)
			reply, err := app.Call("POST", "/targets", string(body))
			if err != nil || reply.Status != 200 {
				return fmt.Errorf("添加测试目标失败：%v %s", err, reply.Data)
			}
			// 仅连接本测试创建的回环端口；不连接任何用户目标。
			client, err := ssh.Dial("tcp", address, &ssh.ClientConfig{User: "tray-test", Auth: []ssh.AuthMethod{ssh.Password("tray-test-password")}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 3 * time.Second})
			if err != nil {
				return err
			}
			defer client.Close()
			session, err := client.NewSession()
			if err != nil {
				return err
			}
			defer session.Close()
			input, err := session.StdinPipe()
			if err != nil {
				return err
			}
			output, err := session.StdoutPipe()
			if err != nil {
				return err
			}
			if err := session.Shell(); err != nil {
				return err
			}
			reader := bufio.NewReader(output)
			echo := func(message string) error {
				if _, err := io.WriteString(input, message+"\n"); err != nil {
					return err
				}
				received := make(chan string, 1)
				go func() { line, _ := reader.ReadString('\n'); received <- line }()
				select {
				case line := <-received:
					if line != message+"\n" {
						return fmt.Errorf("SSH 回显错误：%q", line)
					}
				case <-time.After(3 * time.Second):
					return fmt.Errorf("SSH 回显超时")
				}
				return nil
			}
			if err := echo("隐藏前"); err != nil {
				return err
			}

			// Verify WebSSH opens inside the application and inherits the authenticated session.
			var machineLoaded atomic.Bool
			app.application.Window.OnCreate(func(window application.Window) {
				window.OnWindowEvent(events.Linux.WindowLoadFinished, func(*application.WindowEvent) { machineLoaded.Store(true) })
			})
			if err := app.OpenMachineWindow("tray-smoke"); err != nil {
				return err
			}
			windows := app.application.Window.GetAll()
			if len(windows) != 2 {
				return fmt.Errorf("WebSSH did not create a separate application window")
			}
			for _, window := range windows {
				if window.ID() == app.window.ID() {
					continue
				}
				if err := smokeWait(machineLoaded.Load); err != nil {
					return fmt.Errorf("WebSSH window did not finish loading: %w", err)
				}

				window.Close()
			}
			if err := smokeWait(func() bool { return len(app.application.Window.GetAll()) == 1 }); err != nil {
				return fmt.Errorf("WebSSH close did not release its window: %w", err)
			}
			if err := echo("WebSSH window closed; relay stays connected"); err != nil {
				return err
			}

			app.window.Close()
			if err := smokeWait(func() bool { return !app.window.IsVisible() }); err != nil {
				return fmt.Errorf("关闭未隐藏：%w", err)
			}
			if err := echo("隐藏后连接继续"); err != nil {
				return err
			}
			if app.Info().Listen != address {
				return fmt.Errorf("隐藏后监听发生变化")
			}
			// 通过真实 DBus 托盘点击恢复，而非直接调用窗口方法。
			item := bus.Object(fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid()), "/StatusNotifierItem")
			if err := item.Call("org.kde.StatusNotifierItem.Activate", 0, int32(0), int32(0)).Err; err != nil {
				return err
			}
			if err := smokeWait(app.window.IsVisible); err != nil {
				return fmt.Errorf("托盘点击未恢复：%w", err)
			}
			app.window.Close()
			if err := smokeWait(func() bool { return !app.window.IsVisible() }); err != nil {
				return err
			}
			executable, err := os.Executable()
			if err != nil {
				return err
			}
			launchCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			secondDir, err := os.MkdirTemp("", "ssh-gateway-second-instance-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(secondDir)
			second := exec.CommandContext(launchCtx, executable, "-data", secondDir)
			second.Env = append(os.Environ(), "GATEWAY_SMOKE_SECOND_INSTANCE=1")
			if output, err := second.CombinedOutput(); err != nil {
				return fmt.Errorf("第二实例失败：%v %s", err, output)
			}
			if err := smokeWait(app.window.IsVisible); err != nil {
				return fmt.Errorf("第二实例未恢复窗口：%w", err)
			}
			watcher.props.SetMust(iface, "IsStatusNotifierHostRegistered", false)
			if trayAvailable() {
				return fmt.Errorf("无托盘宿主仍判定为可用")
			}
			app.window.Close()
			if err := smokeWait(func() bool { app.exit.mu.Lock(); defer app.exit.mu.Unlock(); return app.exit.pending }); err != nil {
				return err
			}
			app.window.ExecJS(`const cancelTimer = setInterval(() => { const button = document.querySelector('dialog[open] .dialog-footer button:not(.danger)'); if(button) { clearInterval(cancelTimer); button.click() } }, 100)`)
			if err := smokeWait(func() bool { app.exit.mu.Lock(); defer app.exit.mu.Unlock(); return !app.exit.pending }); err != nil {
				return fmt.Errorf("取消退出失败：%w", err)
			}
			if err := echo("取消退出后连接继续"); err != nil {
				return err
			}
			app.Quit()
			app.window.ExecJS(`const quitTimer = setInterval(() => { const button = document.querySelector('dialog[open] button.danger'); if(button) { clearInterval(quitTimer); button.click() } }, 100)`)
			return nil
		}()
		result <- check
		if check != nil {
			app.ConfirmQuit()
		}
	}()
	if err := app.application.Run(); err != nil {
		return err
	}
	if err := <-result; err != nil {
		return err
	}
	if address != "" {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return fmt.Errorf("退出后端口未释放：%w", err)
		}
		listener.Close()
	}
	return nil
}

func smokeWait(check func() bool) error {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("等待超时")
}

func smokeBackend() (net.Listener, ssh.Signer, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	config := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if string(password) != "backend-password" {
			return nil, fmt.Errorf("测试密码错误")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		server, channels, requests, err := ssh.NewServerConn(conn, config)
		if err != nil {
			conn.Close()
			return
		}
		defer server.Close()
		go ssh.DiscardRequests(requests)
		for channel := range channels {
			stream, requests, err := channel.Accept()
			if err != nil {
				return
			}
			go func() {
				defer stream.Close()
				for request := range requests {
					if request.WantReply {
						request.Reply(true, nil)
					}
					if request.Type == "shell" {
						io.Copy(stream, stream)
						return
					}
				}
			}()
		}
	}()
	return listener, signer, nil
}
