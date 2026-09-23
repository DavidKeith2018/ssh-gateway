package gateway

import (
	"context"
	"io"
	"net"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/pkg/sftp"
)

type remoteWriteLock struct {
	token chan struct{}
	users int
}

// 同一进程内的桌面、网页和不同机器记录共享文件写锁。
var remoteWrites = struct {
	sync.Mutex
	locks map[string]*remoteWriteLock
}{locks: make(map[string]*remoteWriteLock)}

func lockRemoteWrite(ctx context.Context, key string) (func(), error) {
	remoteWrites.Lock()
	l := remoteWrites.locks[key]
	if l == nil {
		l = &remoteWriteLock{token: make(chan struct{}, 1)}
		remoteWrites.locks[key] = l
	}
	l.users++
	remoteWrites.Unlock()
	drop := func() {
		remoteWrites.Lock()
		defer remoteWrites.Unlock()
		l.users--
		if l.users == 0 {
			delete(remoteWrites.locks, key)
		}
	}
	select {
	case l.token <- struct{}{}:
		return func() { <-l.token; drop() }, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}

func writeRemote(ctx context.Context, c *sftp.Client, p string, src io.Reader, overwrite bool, expected string, cleanup func(string)) error {
	selected := ctx.Value(machineTargetKey{}).(record)
	dir, err := c.RealPath(path.Dir(p))
	if err != nil {
		return err
	}
	p = path.Join(dir, path.Base(p))
	host := strings.ToLower(selected.Host)
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	}
	unlock, err := lockRemoteWrite(ctx, net.JoinHostPort(host, strconv.Itoa(selected.Port))+"\x00"+p)
	if err != nil {
		return err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return atomicRemote(c, p, src, overwrite, expected, cleanup)
}
