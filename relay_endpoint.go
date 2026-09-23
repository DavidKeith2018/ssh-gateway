package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// RelayEndpoint 是客户端访问中转服务的全局地址，不改变服务监听。
type RelayEndpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func (s *Store) relayEndpoint(ctx context.Context) (RelayEndpoint, error) {
	var value []byte
	var endpoint RelayEndpoint
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE name='relay_endpoint'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return endpoint, nil
	}
	if err != nil {
		return endpoint, err
	}
	err = json.Unmarshal(value, &endpoint)
	return endpoint, err
}

func (e *RelayEndpoint) validate() error {
	e.Host = strings.TrimSpace(e.Host)
	if strings.HasPrefix(e.Host, "[") && strings.HasSuffix(e.Host, "]") {
		e.Host = strings.TrimSuffix(strings.TrimPrefix(e.Host, "["), "]")
	}
	if e.Host == "" && e.Port == 0 {
		return nil
	}
	if e.Host == "" || e.Port < 1 || e.Port > 65535 {
		return fmt.Errorf("请同时填写中转访问域名/IP 和 1–65535 范围内的端口")
	}
	if net.ParseIP(e.Host) != nil {
		return nil
	}
	if len(e.Host) > 253 {
		return fmt.Errorf("域名过长")
	}
	for _, label := range strings.Split(strings.TrimSuffix(e.Host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("请输入有效域名或 IP，不要包含协议、路径或端口")
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return fmt.Errorf("请输入有效域名或 IP，不要包含协议、路径或端口")
			}
		}
	}
	return nil
}

func (web *Web) relayEndpointSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		endpoint, err := web.store.relayEndpoint(r.Context())
		if err != nil {
			apiError(w, 500, "读取中转访问设置失败")
			return
		}
		jsonResponse(w, 200, endpoint)
		return
	}
	var endpoint RelayEndpoint
	if err := decodeJSON(w, r, &endpoint); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	if err := endpoint.validate(); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	value, _ := json.Marshal(endpoint)
	if _, err := web.store.db.ExecContext(r.Context(), `INSERT INTO settings(name,value) VALUES('relay_endpoint',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value`, value); err != nil {
		apiError(w, 500, "保存中转访问设置失败")
		return
	}
	jsonResponse(w, 200, endpoint)
}

func (web *Web) relayAccessEndpoint(ctx context.Context) (string, string, error) {
	endpoint, err := web.store.relayEndpoint(ctx)
	if err != nil {
		return "", "", err
	}
	if endpoint.Host != "" {
		return endpoint.Host, strconv.Itoa(endpoint.Port), nil
	}
	_, port, _ := net.SplitHostPort(web.sshAddress)
	return web.relayHost, port, nil
}

func (web *Web) terminalEndpoint(r *http.Request) (*TerminalEndpoint, error) {
	if strings.HasPrefix(r.URL.Query().Get("connection"), "server:") {
		return nil, nil
	}
	host, port, err := web.relayAccessEndpoint(r.Context())
	if err != nil {
		return nil, err
	}
	if host == "" {
		host = r.URL.Hostname()
		if host == "" {
			host = r.Host
			if h, _, e := net.SplitHostPort(host); e == nil {
				host = h
			}
		}
	}
	if host == "" || port == "" {
		return nil, fmt.Errorf("中转访问地址或端口未配置")
	}
	return &TerminalEndpoint{Address: net.JoinHostPort(strings.Trim(host, "[]"), port), Fingerprint: ssh.FingerprintSHA256(web.signer.PublicKey())}, nil
}
