package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// 真 TCP、SSH 通道与远程监听，不使用运行状态模拟替代转发测试。
func startMappingBackend(t *testing.T, policy string) (net.Addr, ssh.Signer) {
	t.Helper()
	signer := testSigner(t)
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
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
				conn, channels, requests, err := ssh.NewServerConn(raw, cfg)
				if err != nil {
					return
				}
				defer conn.Close()
				var peers sync.WaitGroup
				peers.Add(1)
				go func() {
					defer peers.Done()
					listeners := map[string]net.Listener{}
					defer func() {
						for _, l := range listeners {
							l.Close()
						}
					}()
					for req := range requests {
						var p struct {
							Host string
							Port uint32
						}
						_ = ssh.Unmarshal(req.Payload, &p)
						key := fmt.Sprintf("%s:%d", p.Host, p.Port)
						switch req.Type {
						case "tcpip-forward":
							if policy == "deny" {
								req.Reply(false, nil)
								continue
							}
							host := p.Host
							if policy == "wildcard" {
								host = "0.0.0.0"
							}
							if policy == "loopback" {
								host = "127.0.0.1"
							}
							l, e := net.Listen("tcp4", net.JoinHostPort(host, fmt.Sprint(p.Port)))
							if e != nil {
								req.Reply(false, nil)
								continue
							}
							listeners[key] = l
							req.Reply(true, nil)
							peers.Add(1)
							go func() {
								defer peers.Done()
								for {
									c, e := l.Accept()
									if e != nil {
										return
									}
									peers.Add(1)
									go func() {
										defer peers.Done()
										defer c.Close()
										stop := context.AfterFunc(ctx, func() { c.Close() })
										defer stop()
										origin := c.RemoteAddr().(*net.TCPAddr)
										payload := struct {
											Host       string
											Port       uint32
											Origin     string
											OriginPort uint32
										}{p.Host, p.Port, origin.IP.String(), uint32(origin.Port)}
										ch, rs, e := conn.OpenChannel("forwarded-tcpip", ssh.Marshal(payload))
										if e != nil {
											return
										}
										go ssh.DiscardRequests(rs)
										testMappingBridge(ch, c)
									}()
								}
							}()
						case "cancel-tcpip-forward":
							if l := listeners[key]; l != nil {
								l.Close()
								delete(listeners, key)
								req.Reply(true, nil)
							} else {
								req.Reply(false, nil)
							}
						default:
							req.Reply(false, nil)
						}
					}
				}()
				for incoming := range channels {
					switch incoming.ChannelType() {
					case "direct-tcpip":
						var p struct {
							Host       string
							Port       uint32
							Origin     string
							OriginPort uint32
						}
						if ssh.Unmarshal(incoming.ExtraData(), &p) != nil {
							incoming.Reject(ssh.ConnectionFailed, "无效地址")
							continue
						}
						c, e := net.DialTimeout("tcp", net.JoinHostPort(p.Host, fmt.Sprint(p.Port)), time.Second)
						if e != nil {
							incoming.Reject(ssh.ConnectionFailed, "服务不可达")
							continue
						}
						ch, reqs, e := incoming.Accept()
						if e != nil {
							c.Close()
							continue
						}
						go ssh.DiscardRequests(reqs)
						peers.Add(1)
						go func() { defer peers.Done(); testMappingBridge(ch, c) }()
					case "session":
						ch, reqs, e := incoming.Accept()
						if e != nil {
							continue
						}
						peers.Add(1)
						go func() {
							defer peers.Done()
							defer ch.Close()
							for req := range reqs {
								var p struct{ Command string }
								if req.Type != "exec" || ssh.Unmarshal(req.Payload, &p) != nil || p.Command != mappingScopeCommand {
									req.Reply(false, nil)
									continue
								}
								req.Reply(true, nil)
								a, e1 := os.ReadFile("/proc/net/tcp")
								b, e2 := os.ReadFile("/proc/net/tcp6")
								ch.Write(a)
								ch.Write(b)
								var code uint32
								if e1 != nil || e2 != nil {
									code = 1
								}
								ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{code}))
								return
							}
						}()
					default:
						incoming.Reject(ssh.Prohibited, "不支持的通道")
					}
				}
				conn.Close()
				peers.Wait()
			}()
		}
	}()
	t.Cleanup(func() {
		cancel()
		listener.Close()
		done := make(chan struct{})
		go func() { workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("转发测试服务器没有退出")
		}
	})
	return listener.Addr(), signer
}
func testMappingBridge(ch ssh.Channel, c net.Conn) {
	defer ch.Close()
	defer c.Close()
	done := make(chan struct{})
	go func() { io.Copy(ch, c); ch.CloseWrite(); close(done) }()
	io.Copy(c, ch)
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		cw.CloseWrite()
	}
	<-done
}
func mappingStore(t *testing.T, policy string) (*Store, *MappingManager, PutInput) {
	t.Helper()
	addr, key := startMappingBackend(t, policy)
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	in := testInput(t)
	in.Host, _, _ = net.SplitHostPort(addr.String())
	in.Port = addr.(*net.TCPAddr).Port
	in.HostFingerprint = ssh.FingerprintSHA256(key.PublicKey())
	if _, err = s.Put(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	return s, s.mappingManager(context.Background()), in
}
func unusedMappingPort(t *testing.T) int {
	t.Helper()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}
func testMapping(t *testing.T, target, direction, scope string) Mapping {
	return Mapping{TargetID: target, Name: "转发测试", Direction: direction, Scope: scope, ServiceHost: "127.0.0.1", ServicePort: unusedMappingPort(t), ListenPort: unusedMappingPort(t)}
}
func awaitMapping(t *testing.T, m *MappingManager, id, status string) MappingView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last MappingView
	for time.Now().Before(deadline) {
		views, err := m.Views(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range views {
			if v.ID == id {
				last = v
				if v.Status == status {
					return v
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("映射未达到 %s：%+v", status, last)
	return last
}
func startMappingEcho(t *testing.T, halfClose bool) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				stop := context.AfterFunc(ctx, func() { c.Close() })
				defer stop()
				if halfClose {
					body, _ := io.ReadAll(c)
					c.Write(append([]byte("回复:"), body...))
				} else {
					io.Copy(c, c)
				}
			}()
		}
	}()
	t.Cleanup(func() { cancel(); l.Close(); wg.Wait() })
	return l.Addr().(*net.TCPAddr).Port
}
func saveTestMapping(t *testing.T, m *MappingManager, r Mapping) Mapping {
	t.Helper()
	r, err := m.Save(context.Background(), r, "127.0.0.1", true)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestMappingsForwardBothDirectionsAndScopes(t *testing.T) {
	for _, direction := range []string{"local", "reverse"} {
		for _, scope := range []string{"loopback", "shared"} {
			t.Run(direction+"/"+scope, func(t *testing.T) {
				_, m, in := mappingStore(t, "")
				r := testMapping(t, in.ID, direction, scope)
				r.ServicePort = startMappingEcho(t, true)
				r = saveTestMapping(t, m, r)
				if err := m.Start(context.Background(), r.ID, "127.0.0.1"); err != nil {
					t.Fatal(err)
				}
				v := awaitMapping(t, m, r.ID, "running")
				if runtime.GOOS == "linux" && !v.ScopeVerified {
					t.Fatalf("实际监听未核验：%+v", v)
				}
				host := "127.0.0.1"
				if scope == "shared" {
					ips := localMappingIPs()
					if len(ips) > 0 {
						host = ips[0]
					}
				}
				c, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(r.ListenPort)), time.Second)
				if err != nil {
					t.Fatal(err)
				}
				c.SetDeadline(time.Now().Add(3 * time.Second))
				payload := bytes.Repeat([]byte("双向数据"), 10000)
				c.Write(payload)
				c.(*net.TCPConn).CloseWrite()
				got, err := io.ReadAll(c)
				c.Close()
				if err != nil || !bytes.Equal(got, append([]byte("回复:"), payload...)) {
					t.Fatalf("双向转发或半关闭失败：%v，长度 %d", err, len(got))
				}
				m.Stop(r.ID)
				awaitMapping(t, m, r.ID, "stopped")
				// 反向监听由远端 SSH 服务异步回收，等待实际端口释放。
				deadline := time.Now().Add(5 * time.Second)
				for {
					l, err := net.Listen("tcp4", r.bindAddress())
					if err == nil {
						l.Close()
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("停止未释放端口：%v", err)
					}
					time.Sleep(20 * time.Millisecond)
				}
			})
		}
	}
}
func TestMappingsRevocationAndActiveConnections(t *testing.T) {
	for _, direction := range []string{"local", "reverse"} {
		t.Run(direction, func(t *testing.T) {
			s, m, in := mappingStore(t, "")
			r := testMapping(t, in.ID, direction, "loopback")
			r.ServicePort = startMappingEcho(t, false)
			r = saveTestMapping(t, m, r)
			if err := m.Start(context.Background(), r.ID, "127.0.0.1"); err != nil {
				t.Fatal(err)
			}
			awaitMapping(t, m, r.ID, "running")
			c, err := net.Dial("tcp", r.bindAddress())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(4 * time.Second))
			c.Write([]byte("ok"))
			buf := make([]byte, 2)
			if _, err = io.ReadFull(c, buf); err != nil {
				t.Fatal(err)
			}
			in.Enabled = false
			if _, err = s.Put(context.Background(), in); err != nil {
				t.Fatal(err)
			}
			awaitMapping(t, m, r.ID, "error")
			if _, err = c.Read(buf); err == nil {
				t.Fatal("撤销后连接仍可用")
			}
			if err = m.Start(context.Background(), r.ID, "127.0.0.1"); err == nil {
				t.Fatal("禁用目标仍能启动映射")
			}
		})
	}
}
func TestMappingsRemotePolicyAndServiceFailure(t *testing.T) {
	for _, tc := range []struct{ policy, scope string }{{"deny", "shared"}, {"wildcard", "loopback"}, {"loopback", "shared"}} {
		t.Run(tc.policy, func(t *testing.T) {
			if runtime.GOOS != "linux" && tc.policy != "deny" {
				t.Skip("监听核验使用 Linux proc")
			}
			_, m, in := mappingStore(t, tc.policy)
			r := saveTestMapping(t, m, testMapping(t, in.ID, "reverse", tc.scope))
			if err := m.Start(context.Background(), r.ID, "127.0.0.1"); err != nil {
				t.Fatal(err)
			}
			v := awaitMapping(t, m, r.ID, "error")
			if v.Error == "" {
				t.Fatal("缺少错误原因")
			}
			l, e := net.Listen("tcp4", r.bindAddress())
			if e != nil {
				t.Fatal("失败未释放监听", e)
			}
			l.Close()
		})
	}
	t.Run("服务不可达", func(t *testing.T) {
		_, m, in := mappingStore(t, "")
		r := saveTestMapping(t, m, testMapping(t, in.ID, "local", "loopback"))
		m.Start(context.Background(), r.ID, "127.0.0.1")
		awaitMapping(t, m, r.ID, "running")
		c, e := net.Dial("tcp", r.bindAddress())
		if e != nil {
			t.Fatal(e)
		}
		c.SetReadDeadline(time.Now().Add(time.Second))
		_, _ = c.Read(make([]byte, 1))
		c.Close()
		v := awaitMapping(t, m, r.ID, "running")
		if !strings.Contains(v.LastError, "服务连接失败") {
			t.Fatalf("没有报告服务不可达：%+v", v)
		}
	})
}
func TestMappingsStorageConflictRevisionAndRestart(t *testing.T) {
	s, m, in := mappingStore(t, "")
	ctx := context.Background()
	r := testMapping(t, in.ID, "local", "loopback")
	r.AutoStart = true
	r.ServicePort = startMappingEcho(t, false)
	r = saveTestMapping(t, m, r)
	duplicate := r
	duplicate.ID = ""
	duplicate.Scope = "shared"
	if _, err := m.Save(ctx, duplicate, "127.0.0.1", true); err == nil {
		t.Fatal("同一监听端口范围重叠未拒绝")
	}
	reverse := r
	reverse.ID = ""
	reverse.Direction = "reverse"
	reverse.AutoStart = false
	reverse = saveTestMapping(t, m, reverse)
	alias := in
	alias.ID = "alias"
	alias.RelayUser = "alias"
	if _, err := s.Put(ctx, alias); err != nil {
		t.Fatal(err)
	}
	duplicate = reverse
	duplicate.ID = ""
	duplicate.TargetID = alias.ID
	if _, err := m.Save(ctx, duplicate, "127.0.0.1", true); err == nil {
		t.Fatal("同一远程主机的别名配置绕过冲突")
	}
	old := r
	r.Name = "已更新"
	var err error
	r, err = m.Save(ctx, r, "127.0.0.1", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Save(ctx, old, "127.0.0.1", false); err == nil {
		t.Fatal("过时修订覆盖配置")
	}
	dir := s.dir
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	next := reopened.mappingManager(ctx)
	awaitMapping(t, next, r.ID, "running")
	if got, err := reopened.mapping(ctx, r.ID); err != nil || got.Name != "已更新" {
		t.Fatal("重启未恢复配置")
	}
	if err = reopened.Delete(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for next.Active() > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if next.Active() != 0 {
		t.Fatal("删除目标未关闭映射")
	}
	views, err := next.Views(ctx)
	if err != nil || len(views) != 0 {
		t.Fatal("删除目标后规则未清理", err)
	}
}
func TestMappingsHTTPAndDesktopAdapter(t *testing.T) {
	s, _, _ := mappingStore(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.SetAdminPassword(ctx, testAdminPassword)
	web := NewWeb(ctx, s, testSigner(t), "127.0.0.1:2222")
	server := httptest.NewServer(web.Handler())
	defer server.Close()
	response, err := http.Get(server.URL + "/api/mappings")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("未登录可读取映射")
	}
	f := &webFixture{fixture: &fixture{store: s}, web: web, http: server}
	f.login(t)
	targets, _ := s.List(ctx)
	r := testMapping(t, targets[0].ID, "local", "loopback")
	response = f.request(t, "POST", "/api/mappings", r, nil)
	var saved Mapping
	if response.StatusCode != 200 {
		body, _ := io.ReadAll(response.Body)
		t.Fatal(response.StatusCode, string(body))
	}
	json.NewDecoder(response.Body).Decode(&saved)
	response = f.request(t, "GET", "/api/mappings", nil, nil)
	var list struct {
		Items []MappingView `json:"items"`
	}
	json.NewDecoder(response.Body).Decode(&list)
	if len(list.Items) != 1 || list.Items[0].Status != "stopped" {
		t.Fatal("保存后不应自动启动")
	}
	if got := f.request(t, "POST", "/api/mappings/"+saved.ID+"/start", map[string]bool{}, nil).StatusCode; got != 202 {
		t.Fatal(got)
	}
	if got := f.request(t, "DELETE", "/api/mappings/"+saved.ID, map[string]bool{}, nil).StatusCode; got != 200 {
		t.Fatal(got)
	}
	if got := f.request(t, "GET", "/api/mappings", nil, map[string]string{"Origin": "https://other.invalid"}).StatusCode; got != 403 {
		t.Fatal("跨站读取未被拒绝")
	}
	d, e := OpenDesktop(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	reply, e := d.Call("POST", "/desktop/setup", `{"password":"test-admin-password-only"}`)
	if e != nil || reply.Status != 200 {
		t.Fatal(e, reply)
	}
	reply, e = d.Call("POST", "/login", `{"username":"ssh-admin","password":"test-admin-password-only"}`)
	if e != nil || reply.Status != 200 {
		t.Fatal(e, reply)
	}
	reply, e = d.Call("GET", "/mappings", "")
	if e != nil || reply.Status != 200 || !bytes.Contains(reply.Data, []byte(`"local_desktop":true`)) {
		t.Fatal("桌面适配器未返回正确运行位置", e, string(reply.Data))
	}
}
func TestMappingScopeParsing(t *testing.T) {
	for _, tc := range []struct {
		data, scope   string
		verified, bad bool
	}{{"0: 0100007F:1F90 00000000:0000 0A", "loopback", true, false}, {"0: 00000000:1F90 00000000:0000 0A", "loopback", false, true}, {"0: 0100007F:1F90 00000000:0000 0A", "shared", false, true}, {"0: 00000000000000000000000000000000:1F90 0:0 0A", "shared", true, false}, {"", "loopback", false, false}} {
		v, _, err := parseMappingScope(tc.data, Mapping{ListenPort: 8080, Scope: tc.scope})
		if v != tc.verified || (err != nil) != tc.bad {
			t.Fatalf("监听解析错误：%+v %v %v", tc, v, err)
		}
	}
}

func TestMappingsOccupiedPortCancellationAndSource(t *testing.T) {
	s, m, in := mappingStore(t, "")
	ctx := context.Background()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	r := testMapping(t, in.ID, "local", "loopback")
	r.ListenPort = listener.Addr().(*net.TCPAddr).Port
	r = saveTestMapping(t, m, r)
	if err = m.Start(ctx, r.ID, "203.0.113.55"); err == nil {
		t.Fatal("未授权来源启动了映射")
	}
	if err = m.Start(ctx, r.ID, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	awaitMapping(t, m, r.ID, "error")
	listener.Close()
	if err = m.Start(ctx, r.ID, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	awaitMapping(t, m, r.ID, "running")
	if ips := localMappingIPs(); len(ips) > 0 {
		conn, e := net.DialTimeout("tcp4", net.JoinHostPort(ips[0], fmt.Sprint(r.ListenPort)), 300*time.Millisecond)
		if e == nil {
			conn.Close()
			t.Fatal("回环监听允许了外部网卡连接")
		}
	}
	in.AllowedSources = []string{"192.0.2.1"}
	if _, err = s.Put(ctx, in); err != nil {
		t.Fatal(err)
	}
	awaitMapping(t, m, r.ID, "error")
}
