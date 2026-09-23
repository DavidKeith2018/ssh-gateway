package gateway

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type DiagnosticStep struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Code     string `json:"code"`
	Duration int64  `json:"duration_ms"`
}
type DiagnosticReport struct {
	Done     bool             `json:"done"`
	Steps    []DiagnosticStep `json:"steps"`
	Location string           `json:"location"`
}
type diagnosticJob struct {
	mu      sync.Mutex
	report  DiagnosticReport
	cancel  context.CancelFunc
	expires time.Time
}

func diagnosticCode(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "refused"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	if errors.Is(err, ErrMasterLocked) {
		return "locked"
	}
	return "failed"
}
func (j *diagnosticJob) step(name, status, code string, started time.Time) {
	j.mu.Lock()
	j.report.Steps = append(j.report.Steps, DiagnosticStep{name, status, code, time.Since(started).Milliseconds()})
	j.mu.Unlock()
}
func (web *Web) startDiagnostic(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Connection string `json:"connection"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	id := uuid.NewString()
	ctx, cancel := context.WithTimeout(web.ctx, 30*time.Second)
	job := &diagnosticJob{report: DiagnosticReport{Steps: []DiagnosticStep{}, Location: "gateway"}, cancel: cancel, expires: time.Now().Add(5 * time.Minute)}
	web.mu.Lock()
	if web.diagnostics == nil {
		web.diagnostics = map[string]*diagnosticJob{}
	}
	running := 0
	for key, other := range web.diagnostics {
		if time.Now().After(other.expires) {
			other.cancel()
			delete(web.diagnostics, key)
			continue
		}
		other.mu.Lock()
		if !other.report.Done {
			running++
		}
		other.mu.Unlock()
	}
	if running >= 2 || len(web.diagnostics) >= 100 {
		web.mu.Unlock()
		cancel()
		apiError(w, 429, "诊断任务过多，请稍后重试")
		return
	}
	web.diagnostics[id] = job
	web.mu.Unlock()
	source := &net.TCPAddr{IP: net.ParseIP(sourceIP(r))}
	target := r.PathValue("id")
	go func() {
		defer cancel()
		defer func() { job.mu.Lock(); job.report.Done = true; job.mu.Unlock() }()
		web.runDiagnostic(ctx, job, target, in.Connection, source)
	}()
	jsonResponse(w, 202, map[string]string{"id": id})
}
func (web *Web) diagnosticResult(w http.ResponseWriter, r *http.Request) {
	web.mu.Lock()
	job := web.diagnostics[r.PathValue("job")]
	web.mu.Unlock()
	if job == nil {
		apiError(w, 404, "诊断任务不存在")
		return
	}
	if r.Method == "DELETE" {
		job.cancel()
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, job.report)
}
func (web *Web) runDiagnostic(ctx context.Context, j *diagnosticJob, id, selection string, source net.Addr) {
	start := time.Now()
	relay := !strings.HasPrefix(selection, "server:")
	if relay {
		target, e := web.store.Get(ctx, id)
		if e == nil {
			candidates := target.Relays
			if len(candidates) == 0 {
				candidates = []TargetRelay{{ID: "default", Enabled: true, ExpiresAt: target.RelayExpiresAt}}
			}
			for _, credential := range candidates {
				if credential.ID == selection {
					if !credential.Enabled {
						j.step("configuration", "failed", "relay_disabled", start)
						return
					}
					if !credential.Active() {
						j.step("configuration", "failed", "expired", start)
						return
					}
				}
			}
		}
	}
	rec, err := web.store.terminalRecord(ctx, id, selection, relay)
	if err != nil {
		j.step("configuration", "failed", "unavailable", start)
		return
	}
	if web.store.MasterPasswordStatus().Locked {
		j.step("configuration", "failed", "locked", start)
		return
	}
	if !rec.Enabled {
		j.step("configuration", "failed", "disabled", start)
		return
	}
	if !web.store.sourceAllowed(ctx, rec.Target, source) {
		j.step("configuration", "failed", "source_denied", start)
		return
	}
	auth, err := web.store.targetAuth(rec)
	if err != nil {
		j.step("configuration", "failed", "credentials", start)
		return
	}
	j.step("configuration", "passed", "ok", start)
	if !diagnoseSSH(ctx, j, rec.Host, rec.Port, rec.User, auth, rec.HostFingerprint) {
		return
	}
	if relay {
		start = time.Now()
		host, port, e := web.relayAccessEndpoint(ctx)
		if e != nil {
			j.step("relay", "failed", "endpoint", start)
			return
		}
		if host == "" {
			host = "127.0.0.1"
		}
		passwords, e := web.store.relayPasswords(ctx, rec.ID)
		if e != nil {
			j.step("relay", "failed", "credentials", start)
			return
		}
		rid := rec.relayID
		if rid == "" {
			rid = "default"
		}
		password, release, e := web.store.terminalSourcePassword(ctx, rec, source, passwords[rid])
		if e != nil {
			j.step("relay", "failed", "unavailable", start)
			return
		}
		defer release()
		client, e := dialSSH(ctx, net.JoinHostPort(host, port), rec.RelayUser, ssh.Password(password), ssh.FingerprintSHA256(web.signer.PublicKey()))
		if e != nil {
			j.step("relay", "failed", diagnosticCode(e), start)
			return
		}
		client.Close()
		j.step("relay", "passed", "ok", start)
	}
}
func diagnoseSSH(ctx context.Context, j *diagnosticJob, host string, port int, user string, auth ssh.AuthMethod, fingerprint string) bool {
	start := time.Now()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		j.step("dns", "failed", diagnosticCode(err), start)
		return false
	}
	j.step("dns", "passed", "ok", start)
	start = time.Now()
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		j.step("tcp", "failed", diagnosticCode(err), start)
		return false
	}
	defer conn.Close()
	j.step("tcp", "passed", "ok", start)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	start = time.Now()
	hostSeen := false
	matched := false
	config := &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		hostSeen = true
		j.step("ssh", "passed", "ok", start)
		matched = ssh.FingerprintSHA256(key) == fingerprint
		if !matched {
			j.step("fingerprint", "failed", "fingerprint_mismatch", start)
			return fmt.Errorf("主机指纹不匹配")
		}
		j.step("fingerprint", "passed", "ok", start)
		return nil
	}}
	client, _, _, err := ssh.NewClientConn(conn, net.JoinHostPort(host, strconv.Itoa(port)), config)
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if !hostSeen {
			j.step("ssh", "failed", diagnosticCode(err), start)
		} else if matched {
			code := diagnosticCode(err)
			if strings.Contains(err.Error(), "unable to authenticate") {
				code = "authentication"
			}
			j.step("authentication", "failed", code, start)
		}
		return false
	}
	client.Close()
	j.step("authentication", "passed", "ok", start)
	return true
}
