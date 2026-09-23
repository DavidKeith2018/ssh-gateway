package gateway

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

type beforeBody struct {
	io.Reader
	before func()
}

func (b *beforeBody) Read(p []byte) (int, error) {
	if b.before != nil {
		f := b.before
		b.before = nil
		f()
	}
	return b.Reader.Read(p)
}

func TestNotesRecheckAuthorizationAfterBody(t *testing.T) {
	for _, op := range []string{"read", "write"} {
		for _, revoke := range []string{"grant", "password", "logout"} {
			t.Run(op+"/"+revoke, func(t *testing.T) {
				f := newWebFixture(t)
				in := accountInput{Username: "notes-user", Enabled: true, TargetIDs: []string{"test"}}
				id := addTestAccount(t, f.store, in)
				cookie := userLogin(t, f, "notes-user")
				body := &beforeBody{Reader: strings.NewReader(`{"op":"` + op + `","content":"不应写入","overwrite":true}`), before: func() {
					switch revoke {
					case "grant":
						in.TargetIDs = nil
					case "password":
						in.Password = "replacement-password"
					case "logout":
						f.web.mu.Lock()
						delete(f.web.sessions, cookie.Value)
						f.web.mu.Unlock()
					}
					if revoke != "logout" {
						if _, err := f.store.saveAccount(context.Background(), id, in); err != nil {
							t.Fatal(err)
						}
					}
					if _, err := f.store.recordNote(context.Background(), "test", "write", "撤权后笔记", "", true); err != nil {
						t.Fatal(err)
					}
				}}
				r := httptest.NewRequest("POST", "http://localhost/api/targets/test/notes", body)
				r.AddCookie(cookie)
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				f.web.Handler().ServeHTTP(w, r)
				if w.Code != 403 {
					t.Fatalf("撤权后仍可访问：%d %s", w.Code, w.Body.String())
				}
				note, err := f.store.recordNote(context.Background(), "test", "read", "", "", false)
				if err != nil || note["content"] != "撤权后笔记" {
					t.Fatalf("笔记被修改：%v %v", note, err)
				}
			})
		}
	}
}

func TestTargetRejectsStaleAndMissingRevision(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	original, err := f.store.Get(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	disabled := original
	disabled.Enabled = false
	requireStatus(t, f, "PUT", "/api/targets/test", disabled, 200)
	stale := original
	stale.Name = "旧窗口修改名称"
	requireStatus(t, f, "PUT", "/api/targets/test", stale, 409)
	stale.Revision = 0
	requireStatus(t, f, "PUT", "/api/targets/test", stale, 400)
	current, err := f.store.Get(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if current.Enabled || current.Name != original.Name {
		t.Fatal("旧快照覆盖了新配置")
	}
	current.Name = "最新窗口修改名称"
	requireStatus(t, f, "PUT", "/api/targets/test", current, 200)
}

func TestConcurrentRemoteWritesDetectConflictAndCancel(t *testing.T) {
	f := newFixture(t)
	selected, err := f.store.get(context.Background(), "id", "test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), machineTargetKey{}, selected), 10*time.Second)
	defer cancel()
	client, err := f.store.dial(ctx, selected)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	c, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	root, err := c.RealPath(".")
	if err != nil {
		t.Fatal(err)
	}
	p := root + "/concurrent.txt"
	if err := writeRemote(ctx, c, p, strings.NewReader("original"), false, "", nil); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- writeRemote(ctx, c, p, &beforeBody{Reader: strings.NewReader("first"), before: func() { close(started); <-release }}, true, version([]byte("original")), nil)
	}()
	<-started
	waiting, stop := context.WithCancel(ctx)
	stop()
	if err := writeRemote(waiting, c, p, strings.NewReader("canceled"), true, "", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("等待取消无效：%v", err)
	}
	second := make(chan error, 1)
	go func() {
		second <- writeRemote(ctx, c, p, strings.NewReader("second"), true, version([]byte("original")), nil)
	}()
	select {
	case err := <-second:
		t.Errorf("并发写入未等待：%v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-second:
		var conflict *machineError
		if !errors.As(err, &conflict) || conflict.code != 409 {
			t.Fatalf("应检测版本冲突：%v", err)
		}
	case <-ctx.Done():
		t.Fatal("写入未结束")
	}
	data, err := readText(c, p)
	if err != nil || string(data) != "first" {
		t.Fatalf("先前保存丢失：%s %v", data, err)
	}
}
