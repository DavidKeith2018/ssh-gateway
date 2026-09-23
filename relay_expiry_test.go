package gateway

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestRelayExpiryAuthenticationAndRevocation(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	in := multiLoginInput(t)
	future := time.Now().Add(time.Hour)
	in.RelayInputs[0].ExpiresAt = &future
	result, err := s.PutTarget(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	relay := result.Target.Relays[0]
	selected, err := s.connectionRecord(ctx, result.ID, relay.ID)
	if err != nil {
		t.Fatal(err)
	}
	source := &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}
	if !NewServer(s, nil).unchanged(ctx, selected, source) {
		t.Fatal("有效凭证不可用")
	}
	// 只推进时间字段，不改变修订号，确保到期由运行时复核而非配置变更触发。
	past := time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	if _, err = s.db.Exec(`UPDATE targets SET config=json_set(config,'$.relays[0].expires_at',?) WHERE id=?`, past, result.ID); err != nil {
		t.Fatal(err)
	}
	if NewServer(s, nil).unchanged(ctx, selected, source) {
		t.Fatal("到期连接未失效")
	}
	if _, err = s.connectionRecord(ctx, result.ID, relay.ID); err == nil {
		t.Fatal("到期凭证仍可选择")
	}
	if _, err = s.connectionRecord(ctx, result.ID, "server:"+relay.LoginID); err != nil {
		t.Fatal("原始账号受影响")
	}
	if len(result.Target.Relays) > 1 {
		if _, err = s.connectionRecord(ctx, result.ID, result.Target.Relays[1].ID); err != nil {
			t.Fatal("其他凭证受影响")
		}
	}
}

func TestRelayExpiryClosesLiveSSH(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	expiry := time.Now().Add(2 * time.Second)
	in := f.input
	in.RelayExpiresAt = &expiry
	if _, err := f.store.Put(ctx, in); err != nil {
		t.Fatal(err)
	}
	client, err := f.dial(in.RelayUser, in.RelayPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	done := make(chan error, 1)
	go func() { done <- client.Wait() }()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("到期后活动 SSH 未关闭")
	}
	if _, err = f.dial(in.RelayUser, in.RelayPassword); err == nil {
		t.Fatal("到期后仍可新建连接")
	}
	f.store.Close()
	reopened, err := OpenStore(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err = reopened.authenticate(ctx, in.RelayUser, []byte(in.RelayPassword), &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}); err == nil {
		t.Fatal("重启后到期时间丢失")
	}
}
