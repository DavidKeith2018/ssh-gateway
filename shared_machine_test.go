package gateway

import (
	"context"
	"encoding/json"
	"golang.org/x/crypto/ssh"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestMachineOperationsReuseTerminal(t *testing.T) {
	for _, selection := range []string{"default", "server:default", ""} {
		t.Run(selection, func(t *testing.T) {
			f := newWebFixture(t)
			f.login(t)
			if selection == "" {
				in := f.input
				in.LoginInputs = []TargetLoginInput{{TargetLogin: TargetLogin{ID: "login-a", User: in.User, AuthType: "password"}, TargetPassword: in.TargetPassword}}
				in.RelayInputs = []TargetRelayInput{{TargetRelay: TargetRelay{ID: "relay-a", LoginID: "login-a", Enabled: true}}}
				if _, err := f.store.PutTarget(context.Background(), in); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			header := http.Header{"Cookie": {f.cookie.String()}, "Origin": {f.http.URL}}
			ws, _, err := websocket.Dial(ctx, strings.Replace(f.http.URL, "http:", "ws:", 1)+"/api/targets/test/terminal?connection="+selection, &websocket.DialOptions{HTTPHeader: header})
			if err != nil {
				t.Fatal(err)
			}
			defer ws.CloseNow()
			shared := ""
			for {
				_, data, err := ws.Read(ctx)
				if err != nil {
					t.Fatal(err)
				}
				var event struct{ Type, Message string }
				json.Unmarshal(data, &event)
				if event.Type == "connection" {
					var info map[string]string
					json.Unmarshal([]byte(event.Message), &info)
					if info["shared_id"] != "" {
						shared = info["shared_id"]
					}
				}
				if event.Type == "ready" {
					break
				}
			}
			if shared == "" {
				t.Fatal("未返回共享连接标识")
			}
			before := f.count.Load()
			query := "?connection=" + selection + "&shared=" + shared
			for _, operation := range []string{"files", "hardware", "resources"} {
				method := "GET"
				var body any
				if operation == "files" {
					method = "POST"
					body = map[string]string{"op": "list", "path": "/"}
				}
				response := f.request(t, method, "/api/targets/test/"+operation+query, body, nil)
				response.Body.Close()
				if response.StatusCode != 200 {
					t.Fatalf("%s 未复用连接: %d", operation, response.StatusCode)
				}
			}
			if got := f.count.Load(); got != before {
				t.Fatalf("额外建立了目标连接: %d -> %d", before, got)
			}
			// 取消操作通道后，原终端仍然能读取资源。
			request, _ := http.NewRequest("GET", f.http.URL+"/api/targets/test/resources"+query, nil)
			request.SetPathValue("id", "test")
			request.RemoteAddr = "127.0.0.1:0"
			request.AddCookie(f.cookie)
			operationCtx, stop := context.WithCancel(ctx)
			client, err := f.web.borrowMachine(request, operationCtx)
			if err != nil {
				t.Fatal(err)
			}
			session, err := client.NewSession()
			if err != nil {
				t.Fatal(err)
			}
			if err := session.Start("cat"); err != nil {
				t.Fatal(err)
			}
			stop()
			client.Close()
			done := make(chan error, 1)
			go func() { done <- session.Wait() }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("操作取消未释放通道")
			}
			response := f.request(t, "GET", "/api/targets/test/resources"+query, nil, nil)
			response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatal("取消操作误关终端")
			}
			response = f.request(t, "GET", "/api/targets/test/hardware?shared="+shared+"&connection=wrong", nil, nil)
			response.Body.Close()
			if response.StatusCode < 400 {
				t.Fatal("允许使用错误连接参数")
			}
			ws.CloseNow()
			deadline := time.Now().Add(2 * time.Second)
			for {
				f.web.mu.Lock()
				_, exists := f.web.sharedMachines[shared]
				f.web.mu.Unlock()
				if !exists {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("关闭终端未释放共享连接")
				}
				time.Sleep(10 * time.Millisecond)
			}
			response = f.request(t, "GET", "/api/targets/test/hardware"+query, nil, nil)
			response.Body.Close()
			if response.StatusCode != 409 {
				t.Fatalf("失效连接应拒绝，不能回退直连: %d", response.StatusCode)
			}
			if got := f.count.Load(); got != before {
				t.Fatal("共享连接失效后重新拨号")
			}
		})
	}
}

func TestDesktopMachineUsesTerminalConnection(t *testing.T) {
	d := freshDesktop(t)
	loginDesktop(t, d)
	address, signer, count := startBackend(t)
	in := testInput(t)
	in.Host = "127.0.0.1"
	in.Port = address.(*net.TCPAddr).Port
	in.HostFingerprint = ssh.FingerprintSHA256(signer.PublicKey())
	if reply := desktopCall(t, d, "POST", "/targets", in); reply.Status != 200 {
		t.Fatal(string(reply.Data))
	}
	for _, selection := range []string{"default", "server:default"} {
		shared := ""
		terminalID := "shared-test-" + strings.ReplaceAll(selection, ":", "-")
		if err := d.OpenTerminal(terminalID, in.ID, func(event TerminalEvent) {
			if event.Type == "connection" {
				var info map[string]string
				json.Unmarshal([]byte(event.Message), &info)
				if info["shared_id"] != "" {
					shared = info["shared_id"]
				}
			}
		}, selection); err != nil {
			t.Fatal(err)
		}
		if shared == "" {
			t.Fatal("桌面终端未提供共享标识")
		}
		before := count.Load()
		query := "connection=" + selection + "&shared=" + shared
		reply, err := d.MachineCall(context.Background(), in.ID, "files", `{"op":"list","path":"/"}`, query)
		if err != nil || reply.Status != 200 {
			t.Fatalf("桌面目录未复用: %v %s", err, reply.Data)
		}
		reply, err = d.MachineCall(context.Background(), in.ID, "hardware", "", query)
		if err != nil || reply.Status != 200 {
			t.Fatalf("桌面硬件未复用: %v %s", err, reply.Data)
		}
		if count.Load() != before {
			t.Fatal("桌面机器操作额外拨号")
		}
		d.CloseTerminal(terminalID)
	}
}
