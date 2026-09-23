package gateway

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRelayEndpointSettings(t *testing.T) {
	f := newWebFixture(t)
	for _, method := range []string{"GET", "PUT"} {
		if got := f.request(t, method, "/api/relay-endpoint", RelayEndpoint{Host: "ssh.example.com", Port: 443}, nil).StatusCode; got != 401 {
			t.Fatalf("未登录请求未被拒绝：%d", got)
		}
	}
	f.login(t)
	host, port, err := f.web.relayAccessEndpoint(context.Background())
	if err != nil || host != "" || port == "" {
		t.Fatalf("默认地址错误：%q %q %v", host, port, err)
	}
	for _, endpoint := range []RelayEndpoint{{Host: "https://host", Port: 22}, {Host: "host:22", Port: 22}, {Host: "host", Port: 0}, {Port: 22}, {Host: "host", Port: 65536}, {Host: "-bad", Port: 22}} {
		if got := f.request(t, "PUT", "/api/relay-endpoint", endpoint, nil).StatusCode; got != 400 {
			t.Fatalf("无效地址被接受：%+v %d", endpoint, got)
		}
	}
	for _, endpoint := range []RelayEndpoint{{Host: "ssh.example.com", Port: 443}, {Host: "[2001:db8::1]", Port: 2223}} {
		if got := f.request(t, "PUT", "/api/relay-endpoint", endpoint, nil).StatusCode; got != 200 {
			t.Fatalf("保存失败：%d", got)
		}
		endpoint.validate()
		// 使用另一个数据库连接确认配置已持久化。
		reopened, err := OpenStore(f.store.dir)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := reopened.relayEndpoint(context.Background())
		reopened.Close()
		if err != nil || saved != endpoint {
			t.Fatalf("配置未持久化：%+v %v", saved, err)
		}
		response := f.request(t, "POST", "/api/targets/test/relays/default/credentials", map[string]any{}, nil)
		var access relayAccess
		if err := json.NewDecoder(response.Body).Decode(&access); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 || access.SSHHost != endpoint.Host {
			t.Fatalf("连接说明没有使用全局地址：%d %+v", response.StatusCode, access)
		}
	}
	if got := f.request(t, "PUT", "/api/relay-endpoint", RelayEndpoint{}, nil).StatusCode; got != 200 {
		t.Fatal(got)
	}
	resetHost, resetPort, err := f.web.relayAccessEndpoint(context.Background())
	if err != nil || resetHost != host || resetPort != port {
		t.Fatal("恢复自动识别失败")
	}
}
