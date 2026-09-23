package gateway

import (
	"context"
	"encoding/json"
	"golang.org/x/crypto/ssh"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// 只由 Playwright 启动的本机夹具；普通 go test 不启动常驻服务。
func TestBrowserFixture(t *testing.T) {
	path := os.Getenv("GATEWAY_BROWSER_FIXTURE")
	if path == "" {
		t.Skip("仅用于浏览器集成测试")
	}
	f := newFixture(t)
	ctx := context.Background()
	if err := f.store.SetAdminPassword(ctx, testAdminPassword); err != nil {
		t.Fatal(err)
	}
	mappingAddr, mappingKey := startMappingBackend(t, "")
	mappingTarget := f.input
	mappingTarget.ID, mappingTarget.RelayUser, mappingTarget.Name = "browser-mapping-target", "browser-mapping-user", "转发验收服务器"
	mappingTarget.Host = "127.0.0.1"
	mappingTarget.Port = mappingAddr.(*net.TCPAddr).Port
	mappingTarget.HostFingerprint = ssh.FingerprintSHA256(mappingKey.PublicKey())
	mappingServicePort := startMappingEcho(t, false)
	data, err := json.Marshal(map[string]any{"admin_password": testAdminPassword, "multi_target": multiLoginInput(t), "target": f.input, "mapping_target": mappingTarget, "mapping_service_port": mappingServicePort, "key_targets": []PutInput{keyTarget(t, ""), keyTarget(t, "浏览器私钥密码-secret")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	port := os.Getenv("GATEWAY_BROWSER_PORT")
	if port == "" {
		port = "18080"
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: NewWeb(ctx, f.store, f.signer, f.address).Handler()}
	if err := server.Serve(listener); err != nil {
		t.Fatal(err)
	}
}
