package gateway

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// GlobalIP 是对所有目标生效的临时来源授权。
type GlobalIP struct {
	IP        string    `json:"ip"`
	ExpiresAt time.Time `json:"expires_at"`
}

const maxGlobalIPLifetime = 30 * 24 * time.Hour

func canonicalIP(value string) (string, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || addr.Zone() != "" {
		return "", fmt.Errorf("请填写单个有效的 IPv4 或 IPv6 地址，不支持网段")
	}
	return addr.Unmap().String(), nil
}

func (s *Store) ListGlobalIPs(ctx context.Context) ([]GlobalIP, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ip, expires_at FROM global_ips ORDER BY ip`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]GlobalIP, 0)
	for rows.Next() {
		var item GlobalIP
		var expiry int64
		if err := rows.Scan(&item.IP, &expiry); err != nil {
			return nil, err
		}
		item.ExpiresAt = time.Unix(0, expiry).UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}

// PutGlobalIP 添加或续期；每次保存必须指定未来且不超过 30 天的时间。
func (s *Store) PutGlobalIP(ctx context.Context, item GlobalIP) error {
	ip, err := canonicalIP(item.IP)
	if err != nil {
		return err
	}
	now := time.Now()
	if !item.ExpiresAt.After(now) || item.ExpiresAt.After(now.Add(maxGlobalIPLifetime)) {
		return fmt.Errorf("过期时间必须晚于当前时间，且有效期最长为 30 天（1 个月）")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO global_ips(ip, expires_at) VALUES (?, ?) ON CONFLICT(ip) DO UPDATE SET expires_at=excluded.expires_at`, ip, item.ExpiresAt.UnixNano())
	return err
}

func (s *Store) DeleteGlobalIP(ctx context.Context, value string) error {
	ip, err := canonicalIP(value)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM global_ips WHERE ip=?`, ip)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) globalSourceAllowed(ctx context.Context, remote net.Addr) bool {
	if remote == nil {
		return false
	}
	host, _, err := net.SplitHostPort(remote.String())
	if err != nil {
		return false
	}
	ip, err := canonicalIP(host)
	if err != nil {
		return false
	}
	var expiry int64
	err = s.db.QueryRowContext(ctx, `SELECT expires_at FROM global_ips WHERE ip=?`, ip).Scan(&expiry)
	return err == nil && time.Now().Before(time.Unix(0, expiry))
}

func (web *Web) globalIPs(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		web.globalIPPage(w, r)
		return
	}
	var item GlobalIP
	if err := decodeJSON(w, r, &item); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	var err error
	action := "全局 IP 白名单已保存："
	if r.Method == http.MethodDelete {
		err = web.store.DeleteGlobalIP(r.Context(), item.IP)
		action = "全局 IP 白名单已删除："
	} else {
		err = web.store.PutGlobalIP(r.Context(), item)
	}
	if err != nil {
		apiError(w, 400, "操作失败："+err.Error())
		return
	}
	ip, _ := canonicalIP(item.IP)
	web.store.logEvent("", sourceIP(r), action+ip)
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

// sourceAllowed 合并目标来源与全局临时授权。
func (s *Store) sourceAllowed(ctx context.Context, target Target, remote net.Addr) bool {
	return allowedSource(target.AllowedSources, remote) || s.globalSourceAllowed(ctx, remote)
}
