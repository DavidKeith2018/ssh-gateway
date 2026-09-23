package gateway

import (
	"net"
	"testing"
)

func TestSourcePolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sources []string
		remote  string
		want    bool
	}{
		{"精确地址", []string{"192.168.1.2"}, "192.168.1.2:4000", true},
		{"拒绝相邻地址", []string{"192.168.1.2"}, "192.168.1.3:4000", false},
		{"内网网段", []string{"192.168.1.0/24"}, "192.168.1.10:4000", true},
		{"其他私网不自动允许", []string{"192.168.1.0/24"}, "10.0.0.1:4000", false},
		{"IPv6", []string{"2001:db8::/64"}, "[2001:db8::5]:4000", true},
		{"映射地址", []string{"127.0.0.1"}, "[::ffff:127.0.0.1]:4000", true},
		{"空列表拒绝", nil, "127.0.0.1:4000", false},
		{"非法配置拒绝", []string{"127.0.0.1", "错误"}, "127.0.0.1:4000", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := net.ResolveTCPAddr("tcp", tc.remote)
			if err != nil {
				t.Fatal(err)
			}
			if got := allowedSource(tc.sources, addr); got != tc.want {
				t.Fatalf("来源判断=%v，预期=%v", got, tc.want)
			}
		})
	}
	for _, source := range []string{"localhost", "192.168.*", "::ffff:127.0.0.0/120", "fe80::1%eth0"} {
		if _, err := ParseSources([]string{source}); err == nil {
			t.Fatalf("不应接受 %q", source)
		}
	}
}
