package gateway

import (
	"context"
	"net/http"
	"strings"
)

// connectionRecord 从已保存的配置选择账号，不接受客户端提供的地址或凭证。
func (s *Store) connectionRecord(ctx context.Context, id, selection string) (record, error) {
	r, err := s.get(ctx, "id", id)
	if err != nil || selection == "" {
		return r, err
	}
	return s.selectConnectionRecord(r, selection)
}

func (s *Store) selectConnectionRecord(r record, selection string) (record, error) {
	if len(r.Logins) == 0 {
		if selection == "default" || selection == "server:default" {
			if selection == "default" {
				r.isRelay = true
				if !(TargetRelay{Enabled: true, ExpiresAt: r.RelayExpiresAt}).Active() {
					return record{}, ErrDenied
				}
			}
			return r, nil
		}
		return record{}, ErrDenied
	}
	loginID := strings.TrimPrefix(selection, "server:")
	if !strings.HasPrefix(selection, "server:") {
		found := false
		for _, relay := range r.Relays {
			if relay.ID == selection && relay.Active() {
				secrets, e := s.savedSecrets(r)
				if e != nil {
					return record{}, e
				}
				r.isRelay = true
				r.relayID, r.hash, r.RelayUser = relay.ID, secrets.Relays[relay.ID], relay.Username
				loginID = relay.LoginID
				found = true
				break
			}
		}
		if !found {
			return record{}, ErrDenied
		}
	}
	for _, login := range r.Logins {
		if login.ID == loginID {
			r.loginID, r.User, r.AuthType = login.ID, login.User, login.AuthType
			return r, nil
		}
	}
	return record{}, ErrDenied
}

func (web *Web) selectedConnection(r *http.Request) (record, error) {
	if r.URL.Query().Has("shared") {
		return web.selectedTerminalConnection(r)
	}
	selection := r.URL.Query().Get("connection")
	if strings.HasPrefix(selection, "server:") && !web.isAdmin(r) {
		return record{}, ErrDenied
	}
	return web.store.connectionRecord(r.Context(), r.PathValue("id"), selection)
}

// 仅中转终端的空选择跟随列表默认账号；独立机器操作和直连接口保留默认登录账号。
func (s *Store) terminalRecord(ctx context.Context, id, selection string, relay bool) (record, error) {
	if selection != "" || !relay {
		return s.connectionRecord(ctx, id, selection)
	}
	r, err := s.get(ctx, "id", id)
	if err != nil || len(r.Logins) == 0 {
		r.isRelay = relay
		if err == nil && relay && !(TargetRelay{Enabled: true, ExpiresAt: r.RelayExpiresAt}).Active() {
			return record{}, ErrDenied
		}
		return r, err
	}
	for _, account := range r.Relays {
		if account.Active() {
			return s.selectConnectionRecord(r, account.ID)
		}
	}
	return record{}, ErrDenied
}

func (web *Web) selectedTerminalConnection(r *http.Request) (record, error) {
	selection := r.URL.Query().Get("connection")
	if strings.HasPrefix(selection, "server:") && !web.isAdmin(r) {
		return record{}, ErrDenied
	}
	return web.store.terminalRecord(r.Context(), r.PathValue("id"), selection, true)
}
