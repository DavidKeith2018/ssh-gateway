package gateway

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestConnectionDiagnosticStages(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	web := NewWeb(ctx, f.store, f.signer, f.address)
	for _, tc := range []struct {
		name, selection, want string
		change                func()
	}{
		{"直连", "server:default", "ok", func() {}},
		{"中转", "default", "ok", func() {}},
		{"指纹", "server:default", "fingerprint_mismatch", func() {
			in := f.input
			in.HostFingerprint = testInput(t).HostFingerprint
			if _, err := f.store.Put(ctx, in); err != nil {
				t.Fatal(err)
			}
		}},
		{"认证", "server:default", "authentication", func() {
			in := f.input
			in.TargetPassword = "wrong-password"
			if _, err := f.store.Put(ctx, in); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.change()
			job := &diagnosticJob{}
			web.runDiagnostic(ctx, job, f.input.ID, tc.selection, &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
			steps := job.report.Steps
			if len(steps) == 0 || steps[len(steps)-1].Code != tc.want {
				t.Fatalf("步骤：%+v", steps)
			}
		})
	}
}
func TestDiagnosticCancelClosesConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	closed := make(chan struct{})
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		for {
			if _, e = conn.Read(buf); e != nil {
				close(closed)
				return
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	job := &diagnosticJob{}
	diagnoseSSH(ctx, job, "127.0.0.1", listener.Addr().(*net.TCPAddr).Port, "user", nil, "")
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("取消后连接未释放")
	}
	if job.report.Steps[len(job.report.Steps)-1].Code != "timeout" {
		t.Fatalf("取消结果：%+v", job.report.Steps)
	}
}

func TestConnectionDiagnosticRejectsDisallowedSource(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	input := f.input
	input.AllowedSources = []string{"192.0.2.1/32"}
	input.SourceMode = "custom"
	if _, err := f.store.Put(ctx, input); err != nil {
		t.Fatal(err)
	}
	web := NewWeb(ctx, f.store, f.signer, f.address)
	for _, selection := range []string{"server:default", "default"} {
		j := &diagnosticJob{}
		web.runDiagnostic(ctx, j, input.ID, selection, &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
		if len(j.report.Steps) != 1 || j.report.Steps[0].Code != "source_denied" || j.report.Steps[0].Status != "failed" {
			t.Fatalf("%s 未在联网前阻止未授权来源：%+v", selection, j.report.Steps)
		}
	}
}
