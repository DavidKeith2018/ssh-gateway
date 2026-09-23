package gateway

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestGlobalIPValidationAndPersistence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []GlobalIP{
		{"bad", time.Now().Add(time.Hour)},
		{"192.0.2.0/24", time.Now().Add(time.Hour)},
		{"fe80::1%eth0", time.Now().Add(time.Hour)},
		{"192.0.2.1", time.Time{}},
		{"192.0.2.1", time.Now().Add(-time.Second)},
		{"192.0.2.1", time.Now().Add(maxGlobalIPLifetime + time.Minute)},
	} {
		if err := s.PutGlobalIP(ctx, item); err == nil {
			t.Fatalf("不应接受：%+v", item)
		}
	}
	expiry := time.Now().Add(maxGlobalIPLifetime)
	for _, ip := range []string{"::ffff:192.0.2.1", "192.0.2.1", "2001:db8::1"} {
		if err := s.PutGlobalIP(ctx, GlobalIP{ip, expiry}); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	items, err := s.ListGlobalIPs(ctx)
	if err != nil || len(items) != 2 {
		t.Fatalf("持久化或去重失败：%v %v", items, err)
	}
	target := Target{AllowedSources: []string{"127.0.0.1"}}
	for _, ip := range []string{"192.0.2.1", "2001:db8::1", "127.0.0.1"} {
		if !s.sourceAllowed(ctx, target, &net.TCPAddr{IP: net.ParseIP(ip)}) {
			t.Fatalf("拒绝有效来源：%s", ip)
		}
	}
	if s.sourceAllowed(ctx, target, &net.TCPAddr{IP: net.ParseIP("192.0.2.2")}) {
		t.Fatal("错误放行其他来源")
	}
	if err := s.DeleteGlobalIP(ctx, "::ffff:192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	if s.globalSourceAllowed(ctx, &net.TCPAddr{IP: net.ParseIP("192.0.2.1")}) {
		t.Fatal("已删除授权仍然有效")
	}
}

func TestGlobalIPRevocation(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		for _, expire := range []bool{false, true} {
			t.Run(map[bool]string{false: "SSH", true: "终端"}[terminal]+map[bool]string{false: "删除", true: "过期"}[expire], func(t *testing.T) {
				f := newFixture(t)
				ctx := context.Background()
				f.input.AllowedSources = []string{"192.0.2.1"}
				if _, err := f.store.Put(ctx, f.input); err != nil {
					t.Fatal(err)
				}
				if err := f.store.PutGlobalIP(ctx, GlobalIP{"127.0.0.1", time.Now().Add(time.Hour)}); err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.authenticate(ctx, f.input.RelayUser, []byte("错误密码"), &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}); err == nil {
					t.Fatal("全局授权绕过了密码")
				}
				var wait func() error
				if terminal {
					session, err := f.store.OpenTerminal(ctx, f.input.ID, &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}, io.Discard)
					if err != nil {
						t.Fatal(err)
					}
					defer session.Close()
					wait = session.Wait
				} else {
					client := f.connect(t)
					session, err := client.NewSession()
					if err != nil {
						t.Fatal(err)
					}
					defer session.Close()
					if err := session.Start("wait"); err != nil {
						t.Fatal(err)
					}
					wait = session.Wait
				}
				if expire {
					// 将到期时刻推进至下一轮检查前，避免测试依赖握手速度。
					if _, err := f.store.db.Exec(`UPDATE global_ips SET expires_at=?`, time.Now().UnixNano()); err != nil {
						t.Fatal(err)
					}
				} else if err := f.store.DeleteGlobalIP(ctx, "127.0.0.1"); err != nil {
					t.Fatal(err)
				}
				ended := make(chan error, 1)
				go func() { ended <- wait() }()
				select {
				case <-ended:
				case <-time.After(3 * time.Second):
					t.Fatal("撤销授权后连接未断开")
				}
				if _, err := f.store.authenticate(ctx, f.input.RelayUser, []byte(f.input.RelayPassword), &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}); err == nil {
					t.Fatal("撤销后仍可认证")
				}
			})
		}
	}
}

func TestGlobalIPAPI(t *testing.T) {
	f := newWebFixture(t)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		if got := f.request(t, method, "/api/global-ips", map[string]string{}, nil).StatusCode; got != 401 {
			t.Fatalf("未认证的接口状态：%d", got)
		}
	}
	f.login(t)
	for _, hours := range []int{0, -1, 721} {
		if got := f.request(t, "PUT", "/api/global-ips", GlobalIP{"192.0.2.1", time.Now().Add(time.Duration(hours) * time.Hour)}, nil).StatusCode; got != 400 {
			t.Fatalf("非法有效期状态：%d", got)
		}
	}
	for _, method := range []string{"PUT", "GET", "DELETE"} {
		if got := f.request(t, method, "/api/global-ips", GlobalIP{"2001:db8::1", time.Now().Add(time.Hour)}, nil).StatusCode; got != 200 {
			t.Fatalf("%s 失败：%d", method, got)
		}
	}
}
