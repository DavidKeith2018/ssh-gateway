package gateway

import (
	"context"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"testing"

	"golang.org/x/crypto/ssh"
)

// 人工或桌面自动化验收时，在明确指定的临时目录录入一个测试目标。
func TestNativeFixture(t *testing.T) {
	dir := os.Getenv("GATEWAY_NATIVE_FIXTURE")
	if dir == "" {
		t.Skip("仅用于原生窗口验收")
	}
	addr, signer, _ := startBackend(t)
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	in := testInput(t)
	in.ID = "native-test"
	in.Name = "桌面终端测试"
	in.RelayUser = "native-test"
	in.Host, _, _ = net.SplitHostPort(addr.String())
	_, port, _ := net.SplitHostPort(addr.String())
	in.Port, _ = strconv.Atoi(port)
	in.HostFingerprint = ssh.FingerprintSHA256(signer.PublicKey())
	if _, err := store.Put(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	t.Log("原生窗口测试目标已就绪")
	<-ctx.Done()
}
