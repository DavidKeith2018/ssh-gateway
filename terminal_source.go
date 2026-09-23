package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"strings"
	"time"
)

const terminalSourcePrefix = "ssh-gateway-terminal-v1:"

type terminalSourceTicket struct {
	ctx      context.Context
	selected record
	source   net.Addr
	expires  time.Time
}

// 来源仅从同一 Store 的内存中取得，不接受 SSH 客户端自行声明的地址。
// 一次性凭据与原中转密码通过已校验主机指纹的 SSH 连接传递。
func (s *Store) terminalSourcePassword(ctx context.Context, selected record, source net.Addr, password string) (string, func(), error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", nil, err
	}
	key := hex.EncodeToString(secret[:])
	s.terminalSourceMu.Lock()
	if s.terminalSources == nil {
		s.terminalSources = make(map[string]terminalSourceTicket)
	}
	s.terminalSources[key] = terminalSourceTicket{ctx, selected, source, time.Now().Add(handshakeTimeout)}
	s.terminalSourceMu.Unlock()
	remove := func() {
		s.terminalSourceMu.Lock()
		delete(s.terminalSources, key)
		s.terminalSourceMu.Unlock()
	}
	stop := context.AfterFunc(ctx, remove)
	timer := time.AfterFunc(handshakeTimeout, remove)
	return terminalSourcePrefix + key + ":" + password, func() { stop(); timer.Stop(); remove() }, nil
}

func (s *Store) authenticateTerminalSource(ctx context.Context, user string, password []byte, peer net.Addr) (record, net.Addr, error) {
	// 普通中转密码最多 72 字节；内部格式更长，不占用任何合法密码取值。
	if len(password) <= 72 || !strings.HasPrefix(string(password), terminalSourcePrefix) {
		r, err := s.authenticate(ctx, user, password, peer)
		return r, peer, err
	}
	key, originalPassword, ok := strings.Cut(strings.TrimPrefix(string(password), terminalSourcePrefix), ":")
	if !ok || len(key) != 64 {
		return record{}, nil, ErrDenied
	}
	s.terminalSourceMu.Lock()
	ticket, found := s.terminalSources[key]
	delete(s.terminalSources, key)
	s.terminalSourceMu.Unlock()
	if !found || ticket.ctx.Err() != nil || !time.Now().Before(ticket.expires) || ticket.selected.RelayUser != user || !NewServer(s, nil).unchanged(ctx, ticket.selected, ticket.source) {
		return record{}, nil, ErrDenied
	}
	r, err := s.authenticate(ctx, user, []byte(originalPassword), ticket.source)
	if err != nil || ticket.ctx.Err() != nil || r.ID != ticket.selected.ID || r.Revision != ticket.selected.Revision {
		return record{}, nil, ErrDenied
	}
	return r, ticket.source, nil
}
