package gateway

import (
	"fmt"
	"net"
	"net/netip"
)

// ParseSources 将单个 IP 或 CIDR 转成精确的来源白名单。空列表不允许访问。
func ParseSources(sources []string) ([]netip.Prefix, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("至少配置一个允许的来源 IP 或网段")
	}
	prefixes := make([]netip.Prefix, 0, len(sources))
	for _, source := range sources {
		if addr, err := netip.ParseAddr(source); err == nil && addr.Zone() == "" {
			addr = addr.Unmap()
			prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		prefix, err := netip.ParsePrefix(source)
		if err != nil || prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("无效的来源 IP 或网段：%q", source)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func allowedSource(sources []string, remote net.Addr) bool {
	prefixes, err := ParseSources(sources)
	if err != nil || remote == nil {
		return false
	}
	host, _, err := net.SplitHostPort(remote.String())
	if err != nil {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func privatePrefix(prefix netip.Prefix) bool {
	for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7", "127.0.0.0/8", "::1/128"} {
		parent := netip.MustParsePrefix(cidr)
		if prefix.Bits() >= parent.Bits() && parent.Contains(prefix.Masked().Addr()) {
			return true
		}
	}
	return false
}
