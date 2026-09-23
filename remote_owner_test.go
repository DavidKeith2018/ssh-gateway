package gateway

import (
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/pkg/sftp"
)

type ownedInfo struct {
	os.FileInfo
	uid, gid uint32
}

func (i ownedInfo) Uid() uint32 { return i.uid }
func (i ownedInfo) Gid() uint32 { return i.gid }

type ownedList struct {
	sftp.ListerAt
	uid, gid uint32
}

func (l ownedList) ListAt(items []os.FileInfo, offset int64) (int, error) {
	n, err := l.ListerAt.ListAt(items, offset)
	for i := 0; i < n; i++ {
		items[i] = ownedInfo{items[i], l.uid, l.gid}
	}
	return n, err
}

type ownershipServer struct {
	sftp.Handlers
	mu     sync.Mutex
	owners map[string][2]uint32
	deny   bool
}

func (s *ownershipServer) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	l, err := s.Handlers.FileList.Filelist(r)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	owner := s.owners[r.Filepath]
	s.mu.Unlock()
	return ownedList{l, owner[0], owner[1]}, nil
}
func (s *ownershipServer) Filecmd(r *sftp.Request) error {
	if r.Method == "Setstat" && r.AttrFlags().UidGid {
		if s.deny {
			return os.ErrPermission
		}
		attrs := r.Attributes()
		s.mu.Lock()
		s.owners[r.Filepath] = [2]uint32{attrs.UID, attrs.GID}
		s.mu.Unlock()
	}
	return s.Handlers.FileCmd.Filecmd(r)
}
func (s *ownershipServer) PosixRename(r *sftp.Request) error {
	if err := s.Handlers.FileCmd.(sftp.PosixRenameFileCmder).PosixRename(r); err != nil {
		return err
	}
	s.mu.Lock()
	s.owners[r.Target] = s.owners[r.Filepath]
	delete(s.owners, r.Filepath)
	s.mu.Unlock()
	return nil
}
func TestRemoteOverwritePreservesOwnership(t *testing.T) {
	for _, deny := range []bool{false, true} {
		t.Run(map[bool]string{false: "保留属主", true: "拒绝变更时保留原文件"}[deny], func(t *testing.T) {
			h := &ownershipServer{Handlers: sftp.InMemHandler(), owners: map[string][2]uint32{"/owned": [2]uint32{1200, 1300}}, deny: deny}
			serverConn, clientConn := net.Pipe()
			server := sftp.NewRequestServer(serverConn, sftp.Handlers{FileGet: h.Handlers.FileGet, FilePut: h.Handlers.FilePut, FileCmd: h, FileList: h})
			defer server.Close()
			go server.Serve()
			c, err := sftp.NewClientPipe(clientConn, clientConn)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			f, err := c.Create("/owned")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(f, "original"); err != nil {
				t.Fatal(err)
			}
			f.Close()
			err = atomicRemote(c, "/owned", strings.NewReader("updated"), true, version([]byte("original")), nil)
			if deny {
				if err == nil {
					t.Fatal("属主保留失败仍覆盖")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			data, err := readText(c, "/owned")
			if err != nil {
				t.Fatal(err)
			}
			expected := "updated"
			if deny {
				expected = "original"
			}
			if string(data) != expected {
				t.Fatalf("内容错误：%s", data)
			}
			info, err := c.Stat("/owned")
			if err != nil {
				t.Fatal(err)
			}
			attrs := info.Sys().(*sftp.FileStat)
			if attrs.UID != 1200 || attrs.GID != 1300 {
				t.Fatalf("属主丢失：%d:%d", attrs.UID, attrs.GID)
			}
		})
	}
}
