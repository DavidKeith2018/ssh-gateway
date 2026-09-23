package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const editLimit = 2 << 20
const transferLimit = 1 << 30

type machineTargetKey struct{}

type machineError struct {
	code    int
	message string
}

func (e *machineError) Error() string     { return e.message }
func fail(code int, message string) error { return &machineError{code, message} }
func machineFailure(w http.ResponseWriter, err error) {
	code := 502
	var e *machineError
	switch {
	case errors.As(err, &e):
		code = e.code
	case os.IsNotExist(err):
		code = 404
	case os.IsPermission(err):
		code = 403
	case os.IsExist(err):
		code = 409
	case errors.Is(err, context.Canceled):
		code = 408
	}
	apiError(w, code, err.Error())
}

// 每项操作检查当前授权；共享终端的请求仅拥有操作通道，取消不会关闭终端。
func (web *Web) machineClient(r *http.Request, timeout time.Duration) (*ssh.Client, context.Context, func(), error) {
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	stop := context.AfterFunc(web.ctx, cancel)
	cleanup := func() { stop(); cancel() }
	selected, err := web.selectedConnection(r.WithContext(ctx))
	addr, addrErr := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	if err != nil || addrErr != nil || !web.canAccess(r, selected.ID) || !selected.Enabled || !web.store.sourceAllowed(ctx, selected.Target, addr) {
		cleanup()
		return nil, ctx, func() {}, fail(403, "当前来源不允许访问，或目标已禁用")
	}
	ctx = context.WithValue(ctx, machineTargetKey{}, selected)
	select {
	case web.machineSlots <- struct{}{}:
	default:
		cleanup()
		return nil, ctx, func() {}, fail(429, "机器操作繁忙，请稍后重试")
	}
	release := func() { cleanup(); <-web.machineSlots }
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !NewServer(web.store, nil).unchanged(ctx, selected, addr) || !web.canAccess(r.WithContext(ctx), selected.ID) {
					cancel()
					return
				}
			}
		}
	}()
	var client *ssh.Client
	if r.URL.Query().Has("shared") {
		client, err = web.borrowMachine(r, ctx)
	} else {
		client, err = web.store.dial(ctx, selected)
	}
	if err != nil {
		release()
		return nil, ctx, func() {}, err
	}
	return client, ctx, func() { client.Close(); release() }, nil
}

// 取消后仅在原授权和目标配置仍有效时清理临时文件；共享模式继续使用原连接。
// 网络不可达或授权已撤销时不绕过限制，临时文件可能需要管理员之后清理。
func (web *Web) temporaryCleanup(r *http.Request, operation context.Context) func(string) {
	selected, ok := operation.Value(machineTargetKey{}).(record)
	addr, _ := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	return func(tmp string) {
		if !ok || addr == nil {
			return
		}
		ctx, cancel := context.WithTimeout(web.ctx, 3*time.Second)
		defer cancel()
		if !web.canAccess(r.WithContext(ctx), selected.ID) || !NewServer(web.store, nil).unchanged(ctx, selected, addr) {
			return
		}
		var client *ssh.Client
		var err error
		if r.URL.Query().Has("shared") {
			client, err = web.borrowMachine(r, ctx)
		} else {
			client, err = web.store.dial(ctx, selected)
		}
		if err != nil {
			return
		}
		defer client.Close()
		c, err := sftp.NewClient(client)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.Remove(tmp)
	}
}

type fileRequest struct {
	Op        string `json:"op"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	Version   string `json:"version"`
	Overwrite bool   `json:"overwrite"`
	Directory bool   `json:"directory"`
}
type fileEntry struct {
	Link     bool      `json:"link,omitempty"`
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Kind     string    `json:"kind"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func cleanRemote(p string) (string, error) {
	if strings.ContainsRune(p, 0) || len(p) > 4096 {
		return "", fail(400, "文件路径无效")
	}
	if p == "" {
		return ".", nil
	}
	if !path.IsAbs(p) {
		return "", fail(400, "请使用绝对路径")
	}
	return path.Clean(p), nil
}
func version(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func readText(c *sftp.Client, p string) ([]byte, error) {
	f, err := c.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fail(422, "仅支持查看普通文件")
	}
	if st.Size() > editLimit {
		return nil, fail(413, "文件超过 2 MiB，请下载查看")
	}
	b, err := io.ReadAll(io.LimitReader(f, editLimit+1))
	if err != nil {
		return nil, err
	}
	if len(b) > editLimit {
		return nil, fail(413, "文件超过 2 MiB，请下载查看")
	}
	if !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
		return nil, fail(422, "文件不是 UTF-8 文本，请下载查看")
	}
	return b, nil
}

// 临时文件与最终文件在同一目录，禁止沿符号链接覆盖文件。
func atomicRemote(c *sftp.Client, p string, src io.Reader, overwrite bool, expected string, cleanup func(string)) error {
	st, err := c.Lstat(p)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if exists {
		if !st.Mode().IsRegular() {
			return fail(422, "不能覆盖目录或符号链接")
		}
		if !overwrite {
			return fail(409, "同名文件已存在")
		}
		if _, ok := c.HasExtension("posix-rename@openssh.com"); !ok {
			return fail(422, "目标不支持安全替换现有文件")
		}
	}
	tmp := path.Join(path.Dir(p), ".ssh-gateway-"+uuid.NewString()+".tmp")
	f, err := c.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	defer func() {
		if err := c.Remove(tmp); err != nil && !os.IsNotExist(err) && cleanup != nil {
			cleanup(tmp)
		}
	}()
	mode := os.FileMode(0600)
	if exists {
		mode = st.Mode().Perm()
		original, ok := st.Sys().(*sftp.FileStat)
		if !ok {
			f.Close()
			return fail(422, "无法读取原文件属主，已取消覆盖")
		}
		created, statErr := f.Stat()
		if statErr != nil {
			f.Close()
			return statErr
		}
		owner, ok := created.Sys().(*sftp.FileStat)
		if !ok {
			f.Close()
			return fail(422, "无法读取临时文件属主，已取消覆盖")
		}
		if owner.UID != original.UID || owner.GID != original.GID {
			if err := f.Chown(int(original.UID), int(original.GID)); err != nil {
				f.Close()
				return fmt.Errorf("无法保留原文件属主和属组，已取消覆盖：%w", err)
			}
		}
	}
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	n, err := io.Copy(f, io.LimitReader(src, transferLimit+1))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n > transferLimit {
		return fail(413, "上传超过 1 GiB 限制")
	}
	if expected != "" {
		data, err := readText(c, p)
		if err != nil {
			return err
		}
		if version(data) != expected {
			return fail(409, "远程文件已变化，请重新读取或确认覆盖")
		}
	}
	if exists {
		return c.PosixRename(tmp, p)
	}
	return c.Rename(tmp, p)
}
func removeRemote(ctx context.Context, c *sftp.Client, p string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 128 {
		return fail(422, "目录层级过深")
	}
	st, err := c.Lstat(p)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return c.Remove(p)
	}
	entries, err := c.ReadDirContext(ctx, p)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := removeRemote(ctx, c, path.Join(p, entry.Name()), depth+1); err != nil {
			return err
		}
	}
	return c.RemoveDirectory(p)
}
func (web *Web) files(w http.ResponseWriter, r *http.Request) {
	var in fileRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		apiError(w, 400, "文件请求无效")
		return
	}
	if dec.Decode(new(any)) != io.EOF {
		apiError(w, 400, "只允许一个请求对象")
		return
	}
	p, err := cleanRemote(in.Path)
	if err != nil {
		machineFailure(w, err)
		return
	}
	client, ctx, done, err := web.machineClient(r, 30*time.Second)
	if err != nil {
		machineFailure(w, err)
		return
	}
	defer done()
	c, err := sftp.NewClient(client)
	if err != nil {
		machineFailure(w, fmt.Errorf("无法启动目标 SFTP：%w", err))
		return
	}
	defer c.Close()
	var result any = map[string]bool{"ok": true}
	switch in.Op {
	case "list":
		p, err = c.RealPath(p)
		if err != nil {
			break
		}
		var entries []os.FileInfo
		entries, err = c.ReadDirContext(ctx, p)
		if err != nil {
			break
		}
		items := make([]fileEntry, 0, len(entries))
		for _, st := range entries {
			kind := "file"
			if st.IsDir() {
				kind = "directory"
			} else if st.Mode()&os.ModeSymlink != 0 {
				kind = "link"
				if target, statErr := c.Stat(path.Join(p, st.Name())); statErr == nil && target.IsDir() {
					kind = "directory"
				}
			}
			items = append(items, fileEntry{st.Mode()&os.ModeSymlink != 0, st.Name(), path.Join(p, st.Name()), kind, st.Size(), st.ModTime()})
		}
		sort.Slice(items, func(i, j int) bool {
			if (items[i].Kind == "directory") != (items[j].Kind == "directory") {
				return items[i].Kind == "directory"
			}
			return items[i].Name < items[j].Name
		})
		result = map[string]any{"path": p, "entries": items}
	case "read":
		p, err = c.RealPath(p)
		if err != nil {
			break
		}
		var b []byte
		b, err = readText(c, p)
		if err == nil {
			result = map[string]any{"path": p, "content": string(b), "version": version(b)}
		}
	case "write":
		if len(in.Content) > editLimit {
			err = fail(413, "文件超过 2 MiB")
			break
		}
		if !utf8.ValidString(in.Content) || strings.ContainsRune(in.Content, 0) {
			err = fail(422, "仅支持 UTF-8 文本")
			break
		}
		if in.Version == "" && !in.Overwrite {
			err = fail(400, "保存缺少文件版本")
			break
		}
		expected := in.Version
		if in.Overwrite {
			expected = ""
		}
		err = writeRemote(ctx, c, p, strings.NewReader(in.Content), true, expected, web.temporaryCleanup(r, ctx))
		result = map[string]string{"version": version([]byte(in.Content))}
	case "create":
		if _, checkErr := c.Lstat(p); checkErr == nil {
			err = fail(409, "同名文件或目录已存在")
			break
		} else if !os.IsNotExist(checkErr) {
			err = checkErr
			break
		}
		if p == "." || p == "/" {
			err = fail(400, "不能新建根目录")
			break
		}
		if in.Directory {
			err = c.Mkdir(p)
		} else {
			var f *sftp.File
			f, err = c.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
			if err == nil {
				err = f.Close()
			}
		}
	case "delete":
		if p == "." || p == "/" {
			err = fail(400, "不能删除根目录")
			break
		}
		err = removeRemote(ctx, c, p, 0)
	default:
		err = fail(400, "未知文件操作")
	}
	if err != nil {
		machineFailure(w, err)
		return
	}
	jsonResponse(w, 200, result)
}
func (web *Web) transfer(w http.ResponseWriter, r *http.Request) {
	p, err := cleanRemote(r.URL.Query().Get("path"))
	if err != nil || p == "." || p == "/" {
		apiError(w, 400, "请选择文件绝对路径")
		return
	}
	client, ctx, done, err := web.machineClient(r, 30*time.Minute)
	if err != nil {
		machineFailure(w, err)
		return
	}
	defer done()
	// 授权撤销或超时时也中止客户端请求体读取，不能被慢上传占住操作槽。
	stopRead := context.AfterFunc(ctx, func() {
		_ = http.NewResponseController(w).SetReadDeadline(time.Now())
		if r.Body != nil {
			_ = r.Body.Close()
		}
	})
	defer stopRead()
	c, err := sftp.NewClient(client)
	if err != nil {
		machineFailure(w, err)
		return
	}
	defer c.Close()
	if r.Method == "PUT" {
		err = writeRemote(ctx, c, p, http.MaxBytesReader(w, r.Body, transferLimit+1), r.URL.Query().Get("overwrite") == "true", "", web.temporaryCleanup(r, ctx))
		if err != nil {
			machineFailure(w, err)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	f, err := c.Open(p)
	if err != nil {
		machineFailure(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		machineFailure(w, err)
		return
	}
	if !st.Mode().IsRegular() {
		apiError(w, 422, "仅支持下载普通文件")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(p)}))
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	io.Copy(w, f)
}

const resourceCommand = "uname -s; head -n 1 /proc/stat; cat /proc/meminfo; printf '\n__GW_DISK__\n'; LC_ALL=C df -Pk / 2>/dev/null; true"

type resourceSample struct {
	Time            time.Time `json:"time"`
	Total           uint64    `json:"cpu_total"`
	Idle            uint64    `json:"cpu_idle"`
	MemoryTotal     uint64    `json:"memory_total"`
	MemoryAvailable uint64    `json:"memory_available"`
	CPUReady        bool      `json:"cpu_ready"`
	MemoryReady     bool      `json:"memory_ready"`
	DiskUsage       float64   `json:"disk_usage"`
	DiskReady       bool      `json:"disk_ready"`
}

func parseResources(data string) (resourceSample, error) {
	out := resourceSample{Time: time.Now().UTC()}
	lines := strings.Split(data, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "Linux" {
		return out, fail(422, "暂不支持此目标的资源采集（首版支持 Linux）")
	}
	diskSection := false
	for _, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if f[0] == "__GW_DISK__" {
			diskSection = true
			continue
		}
		if diskSection {
			if len(f) >= 6 && f[len(f)-1] == "/" {
				usage, err := strconv.ParseFloat(strings.TrimSuffix(f[len(f)-2], "%"), 64)
				if err == nil && usage >= 0 && usage <= 100 {
					out.DiskUsage = usage
					out.DiskReady = true
				}
			}
			continue
		}
		if f[0] == "cpu" && len(f) >= 9 {
			valid := true
			for i := 1; i <= 8; i++ {
				n, e := strconv.ParseUint(f[i], 10, 64)
				if e != nil {
					valid = false
					break
				}
				out.Total += n
				if i == 4 || i == 5 {
					out.Idle += n
				}
			}
			out.CPUReady = valid
		}
		if len(f) >= 2 && (f[0] == "MemTotal:" || f[0] == "MemAvailable:") {
			n, e := strconv.ParseUint(f[1], 10, 64)
			if e == nil {
				if f[0] == "MemTotal:" {
					out.MemoryTotal = n * 1024
				} else {
					out.MemoryAvailable = n * 1024
					out.MemoryReady = true
				}
			}
		}
	}
	out.MemoryReady = out.MemoryReady && out.MemoryTotal > 0 && out.MemoryAvailable <= out.MemoryTotal
	return out, nil
}
func (web *Web) resources(w http.ResponseWriter, r *http.Request) {
	client, _, done, err := web.machineClient(r, 4*time.Second)
	if err != nil {
		machineFailure(w, err)
		return
	}
	defer done()
	session, err := client.NewSession()
	if err != nil {
		machineFailure(w, err)
		return
	}
	defer session.Close()
	// 固定命令，不拼接用户路径，不读取进程或环境凭证。
	out, err := session.Output(resourceCommand)
	if err != nil {
		machineFailure(w, fail(422, "无法读取目标 Linux 资源信息"))
		return
	}
	sample, err := parseResources(string(out))
	if err != nil {
		machineFailure(w, err)
		return
	}
	jsonResponse(w, 200, sample)
}
