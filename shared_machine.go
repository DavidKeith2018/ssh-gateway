package gateway

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

type sharedMachine struct {
	client                            *ssh.Client
	ctx                               context.Context
	target, selection, cookie, source string
}

func (web *Web) shareTerminal(r *http.Request, ctx context.Context, client *ssh.Client, target, selection string) (string, func()) {
	cookie, _ := r.Cookie(cookieName)
	key := uuid.NewString()
	web.mu.Lock()
	if web.sharedMachines == nil {
		web.sharedMachines = make(map[string]sharedMachine)
	}
	web.sharedMachines[key] = sharedMachine{client, ctx, target, selection, cookie.Value, sourceIP(r)}
	web.mu.Unlock()
	return key, func() { web.mu.Lock(); delete(web.sharedMachines, key); web.mu.Unlock() }
}

func (web *Web) borrowMachine(r *http.Request, ctx context.Context) (*ssh.Client, error) {
	web.mu.Lock()
	shared, ok := web.sharedMachines[r.URL.Query().Get("shared")]
	web.mu.Unlock()
	cookie, err := r.Cookie(cookieName)
	if !ok || err != nil || shared.cookie != cookie.Value || shared.target != r.PathValue("id") || shared.selection != r.URL.Query().Get("connection") || shared.source != sourceIP(r) || shared.ctx.Err() != nil {
		return nil, fail(409, "当前终端未连接，请先建立终端连接")
	}
	scope, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(shared.ctx, cancel)
	conn := &borrowedSSH{Conn: shared.client.Conn, ctx: scope, cancel: func() { stop(); cancel() }}
	channels := make(chan ssh.NewChannel)
	close(channels)
	requests := make(chan *ssh.Request)
	close(requests)
	return ssh.NewClient(conn, channels, requests), nil
}

// 请求仅拥有自己的 SSH 通道；请求结束或超时不关闭终端的底层连接。
type borrowedSSH struct {
	ssh.Conn
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func (c *borrowedSSH) Close() error { c.once.Do(c.cancel); return nil }
func (c *borrowedSSH) Wait() error  { <-c.ctx.Done(); return c.ctx.Err() }
func (c *borrowedSSH) SendRequest(string, bool, []byte) (bool, []byte, error) {
	return false, nil, fmt.Errorf("机器操作不支持全局 SSH 请求")
}
func (c *borrowedSSH) OpenChannel(kind string, data []byte) (ssh.Channel, <-chan *ssh.Request, error) {
	type result struct {
		channel  ssh.Channel
		requests <-chan *ssh.Request
		err      error
	}
	ready := make(chan result)
	go func() {
		channel, requests, err := c.Conn.OpenChannel(kind, data)
		if err == nil {
			context.AfterFunc(c.ctx, func() { channel.Close() })
		}
		select {
		case ready <- result{channel, requests, err}:
		case <-c.ctx.Done():
			if channel != nil {
				channel.Close()
			}
		}
	}()
	select {
	case value := <-ready:
		return value.channel, value.requests, value.err
	case <-c.ctx.Done():
		return nil, nil, c.ctx.Err()
	}
}
