package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"
)

func TestActiveSSHConnectionsLifecycle(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	assertCount := func(want int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			response := f.request(t, "GET", "/api/targets/summary", nil, nil)
			var data struct {
				Active int `json:"active"`
			}
			err := json.NewDecoder(response.Body).Decode(&data)
			response.Body.Close()
			if err != nil || response.StatusCode != 200 {
				t.Fatalf("统计失败：%v", err)
			}
			if data.Active == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("在线数量 %d，预期 %d", data.Active, want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	assertCount(0)
	client, err := f.dial(f.input.RelayUser, f.input.RelayPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	assertCount(1)
	ws, _ := f.terminal(t)
	defer ws.CloseNow()
	assertCount(2)
	client.Close()
	assertCount(1)
	ws.CloseNow()
	assertCount(0)
	direct, err := f.store.OpenTerminal(context.Background(), f.input.ID, &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer direct.Close()
	assertCount(1)
	direct.Close()
	assertCount(0)
}

func TestActiveConnectionsRespectMachineAccess(t *testing.T) {
	f := newWebFixture(t)
	allowed := addTestAccount(t, f.store, accountInput{Username: "active-allowed", Enabled: true, TargetIDs: []string{"test"}})
	denied := addTestAccount(t, f.store, accountInput{Username: "active-denied", Enabled: true})
	stop := f.store.trackSSHConnection("test")
	defer stop()
	for user, want := range map[string]int{"": 1, allowed: 1, denied: 0} {
		got, err := f.store.activeConnections(context.Background(), user)
		if err != nil || got != want {
			t.Fatalf("授权统计错误：%d，预期 %d，%v", got, want, err)
		}
	}
	stop()
	stop()
	got, err := f.store.activeConnections(context.Background(), "")
	if err != nil || got != 0 {
		t.Fatalf("重复关闭错误：%d %v", got, err)
	}
}
