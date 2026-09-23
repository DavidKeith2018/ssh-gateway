package gateway

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// 可选的系统 OpenSSH 验收，不修改系统配置、用户密钥或已有 sshd。
func TestMappingsOpenSSH(t *testing.T) {
	if os.Getenv("GATEWAY_OPENSSH_TEST") != "1" {
		t.Skip("设置 GATEWAY_OPENSSH_TEST=1 验证系统 OpenSSH")
	}
	binary, err := exec.LookPath("sshd")
	if err != nil {
		t.Fatal(err)
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	hostPrivate, hostPublic := testPrivateKey(t, "")
	clientPrivate, clientPublic := testPrivateKey(t, "")
	for name, data := range map[string]string{"host_key": hostPrivate, "authorized_keys": string(ssh.MarshalAuthorizedKey(clientPublic))} {
		if err = os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	port := unusedMappingPort(t)
	config := fmt.Sprintf("Port %d\nListenAddress 127.0.0.1\nHostKey %s\nPidFile %s\nAuthorizedKeysFile %s\nStrictModes no\nUsePAM no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPubkeyAuthentication yes\nAuthenticationMethods publickey\nAllowTcpForwarding yes\nGatewayPorts clientspecified\nUseDNS no\nLogLevel ERROR\n", port, filepath.Join(dir, "host_key"), filepath.Join(dir, "sshd.pid"), filepath.Join(dir, "authorized_keys"))
	path := filepath.Join(dir, "sshd_config")
	if err = os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(dir, "sshd.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.Command(binary, "-D", "-e", "-f", path)
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	ready := false
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); {
		conn, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if e == nil {
			conn.Close()
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		data, _ := os.ReadFile(filepath.Join(dir, "sshd.log"))
		t.Fatalf("临时 OpenSSH 未启动：%s", data)
	}
	store, err := OpenStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	in := testInput(t)
	in.Host = "127.0.0.1"
	in.Port = port
	in.User = account.Username
	in.AuthType = "private_key"
	in.TargetPassword = ""
	in.TargetPrivateKey = clientPrivate
	in.HostFingerprint = ssh.FingerprintSHA256(hostPublic)
	if _, err = store.Put(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	manager := store.mappingManager(context.Background())
	service := startMappingEcho(t, false)
	for _, direction := range []string{"local", "reverse"} {
		for _, scope := range []string{"loopback", "shared"} {
			t.Run(direction+"/"+scope, func(t *testing.T) {
				r := testMapping(t, in.ID, direction, scope)
				r.ServicePort = service
				r = saveTestMapping(t, manager, r)
				if err := manager.Start(context.Background(), r.ID, "127.0.0.1"); err != nil {
					t.Fatal(err)
				}
				v := awaitMapping(t, manager, r.ID, "running")
				if !v.ScopeVerified {
					t.Fatalf("OpenSSH 监听范围未核验：%+v", v)
				}
				host := "127.0.0.1"
				if scope == "shared" {
					if ips := localMappingIPs(); len(ips) > 0 {
						host = ips[0]
					}
				}
				c, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(r.ListenPort)), time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(3 * time.Second))
				c.Write([]byte("OpenSSH实际转发"))
				reply := make([]byte, len("OpenSSH实际转发"))
				if _, err = io.ReadFull(c, reply); err != nil || string(reply) != "OpenSSH实际转发" {
					t.Fatal("OpenSSH 转发失败", err)
				}
				manager.Stop(r.ID)
				l, err := net.Listen("tcp4", r.bindAddress())
				if err != nil {
					t.Fatal("OpenSSH 端口未释放", err)
				}
				l.Close()
			})
		}
	}
}
