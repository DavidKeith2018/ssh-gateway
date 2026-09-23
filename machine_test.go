package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// SFTP 使用斜杠绝对路径；夹具落盘检查需转换为测试宿主机的路径。
func localSFTPFixturePath(p string) string {
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

func machineFile(t *testing.T, f *webFixture, body any, status int) map[string]any {
	t.Helper()
	r := f.request(t, "POST", "/api/targets/test/files", body, nil)
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != status {
		t.Fatalf("文件操作 %v: %d %s", body, r.StatusCode, b)
	}
	var out map[string]any
	json.Unmarshal(b, &out)
	return out
}
func TestMachineFilesRoundTripConflictAndLinks(t *testing.T) {
	f := newWebFixture(t)
	machineFile(t, f, map[string]string{"op": "list"}, 401)
	f.login(t)
	root := machineFile(t, f, map[string]string{"op": "list"}, 200)["path"].(string)
	p := path.Join(root, "中文 空格.yaml")
	machineFile(t, f, map[string]any{"op": "create", "path": p}, 200)
	machineFile(t, f, map[string]any{"op": "create", "path": p}, 409)
	read := machineFile(t, f, map[string]string{"op": "read", "path": p}, 200)
	saved := machineFile(t, f, map[string]any{"op": "write", "path": p, "content": "port: 22\n", "version": read["version"]}, 200)
	machineFile(t, f, map[string]any{"op": "write", "path": p, "content": "stale", "version": read["version"]}, 409)
	if b, _ := os.ReadFile(localSFTPFixturePath(p)); string(b) != "port: 22\n" {
		t.Fatal("冲突覆盖了文件")
	}
	machineFile(t, f, map[string]any{"op": "write", "path": p, "content": strings.Repeat("x", 70000), "version": saved["version"]}, 200)
	machineFile(t, f, map[string]string{"op": "delete", "path": "/"}, 400)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "keep"), []byte("保留"), 0600)
	dir := path.Join(root, "remove")
	os.Mkdir(localSFTPFixturePath(dir), 0700)
	if err := os.Symlink(outside, localSFTPFixturePath(path.Join(dir, "link"))); err == nil {
		machineFile(t, f, map[string]string{"op": "delete", "path": dir}, 200)
		if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
			t.Fatal("递归删除跟随了链接")
		}
	}
	machineFile(t, f, map[string]string{"op": "delete", "path": p}, 200)
	machineFile(t, f, map[string]string{"op": "read", "path": p}, 404)
	files, _ := os.ReadDir(localSFTPFixturePath(root))
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".ssh-gateway-") {
			t.Fatal("遗留临时文件")
		}
	}
}
func TestMachineTransferAndAuthorization(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	root := machineFile(t, f, map[string]string{"op": "list"}, 200)["path"].(string)
	p := path.Join(root, "二进制 file.bin")
	data := bytes.Repeat([]byte{0, 1, 2, 255}, 40000)
	transfer := func(method string, body io.Reader) *http.Response {
		r, _ := http.NewRequest(method, f.http.URL+"/api/targets/test/transfer?path="+url.QueryEscape(p), body)
		r.AddCookie(f.cookie)
		r.Header.Set("Content-Type", "application/octet-stream")
		res, err := f.http.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}
	if res := transfer("PUT", bytes.NewReader(data)); res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("上传失败 %s", b)
	}
	if res := transfer("PUT", bytes.NewReader([]byte("overwrite"))); res.StatusCode != 409 {
		t.Fatal("未确认覆盖也成功")
	}
	res := transfer("GET", nil)
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !bytes.Equal(b, data) {
		t.Fatal("下载内容不一致")
	}
	machineFile(t, f, map[string]string{"op": "read", "path": p}, 422)
	if res := f.request(t, "GET", "/api/targets/test/resources", nil, nil); res.StatusCode != 200 {
		t.Fatal("采集失败")
	}
	in := f.input
	in.AllowedSources = []string{"192.0.2.1"}
	if _, err := f.store.Put(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	machineFile(t, f, map[string]string{"op": "list"}, 403)
	if transfer("GET", nil).StatusCode != 403 {
		t.Fatal("下载绕过来源规则")
	}
	if f.request(t, "GET", "/api/targets/test/resources", nil, nil).StatusCode != 403 {
		t.Fatal("采集绕过来源规则")
	}
}
func TestResourceParser(t *testing.T) {
	sample, err := parseResources("Linux\ncpu 10 20 30 40 5 6 7 8 100 100\nMemTotal: 100 kB\nMemAvailable: 40 kB\n")
	if err != nil || !sample.CPUReady || sample.Total != 126 || sample.Idle != 45 || sample.MemoryTotal != 102400 || !sample.MemoryReady {
		t.Fatalf("采样口径错误 %+v %v", sample, err)
	}
	sample, _ = parseResources("Linux\ncpu broken\nMemTotal: 100 kB\n")
	if sample.CPUReady || sample.MemoryReady {
		t.Fatal("缺失指标被当作有效")
	}
	if _, err := parseResources("Darwin\n"); err == nil {
		t.Fatal("不支持环境未报告")
	}
}
func TestResourceDiskParser(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		ready      bool
		usage      float64
	}{
		{"正常", "/dev/root 100000 42000 58000 42% /", true, 42},
		{"已满", "/dev/root 100000 100000 0 100% /", true, 100},
		{"空盘", "/dev/root 100000 0 100000 0% /", true, 0},
		{"缺失", "", false, 0},
		{"损坏", "/dev/root 100000 0 0 invalid /", false, 0},
		{"其他分区", "/dev/data 100000 42000 58000 42% /data", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sample, err := parseResources("Linux\n__GW_DISK__\nFilesystem 1024-blocks Used Available Capacity Mounted on\n" + tc.data)
			if err != nil || sample.DiskReady != tc.ready || sample.DiskUsage != tc.usage {
				t.Fatalf("硬盘采样错误: %+v %v", sample, err)
			}
		})
	}
}
func TestMachineJobsCancelBeforeStart(t *testing.T) {
	var jobs MachineJobs
	jobs.Cancel("early")
	ctx, done, err := jobs.Start(context.Background(), "early")
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if ctx.Err() == nil {
		t.Fatal("未处理先到的取消")
	}
}
func TestDesktopMachineCallAndTransfer(t *testing.T) {
	d := freshDesktop(t)
	loginDesktop(t, d)
	address, signer, _ := startBackend(t)
	in := testInput(t)
	host, portText, _ := net.SplitHostPort(address.String())
	port, _ := strconv.Atoi(portText)
	in.Host = host
	in.Port = port
	in.HostFingerprint = ssh.FingerprintSHA256(signer.PublicKey())
	if reply := desktopCall(t, d, "POST", "/targets", in); reply.Status != 200 {
		t.Fatal(string(reply.Data))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reply, err := d.MachineCall(ctx, "test", "files", `{"op":"list"}`)
	if err != nil || reply.Status != 200 {
		t.Fatalf("桌面文件 %v %s", err, reply.Data)
	}
	var listing struct {
		Path string `json:"path"`
	}
	json.Unmarshal(reply.Data, &listing)
	hardware, err := d.MachineCall(ctx, "test", "hardware", "")
	if err != nil || hardware.Status != 200 || !bytes.Contains(hardware.Data, []byte("Fixture CPU")) {
		t.Fatalf("桌面硬件信息失败：%v %s", err, hardware.Data)
	}
	notes, err := d.MachineCall(ctx, "test", "notes", `{"op":"read"}`)
	if err != nil || notes.Status != 200 || !bytes.Contains(notes.Data, []byte("version")) {
		t.Fatalf("桌面本机笔记失败：%v %s", err, notes.Data)
	}

	p := path.Join(listing.Path, "desktop.bin")
	var sink bytes.Buffer
	if err := d.Transfer(ctx, "test", p, true, false, strings.NewReader("桌面流式传输"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := d.Transfer(ctx, "test", p, false, false, nil, &sink); err != nil || sink.String() != "桌面流式传输" {
		t.Fatalf("桌面下载 %v %s", err, sink.String())
	}
	desktopCall(t, d, "POST", "/logout", nil)
	reply, err = d.MachineCall(ctx, "test", "resources", "")
	if err != nil || reply.Status != 401 {
		t.Fatal("桌面采集绕过登录")
	}
}

// 上传在写入过程中撤销授权：旧文件不能被部分内容覆盖，槽位必须释放。
func TestMachineUploadRevocationPreservesOriginal(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	root := machineFile(t, f, map[string]string{"op": "list"}, 200)["path"].(string)
	p := path.Join(root, "keep.txt")
	if err := os.WriteFile(localSFTPFixturePath(p), []byte("原文件"), 0600); err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "PUT", f.http.URL+"/api/targets/test/transfer?overwrite=true&path="+url.QueryEscape(p), reader)
	request.AddCookie(f.cookie)
	request.Header.Set("Content-Type", "application/octet-stream")
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, _ := f.http.Client().Do(request)
		if response != nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
	}()
	if _, err := writer.Write(bytes.Repeat([]byte("x"), 64<<10)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(f.web.machineSlots) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	in := f.input
	in.Enabled = false
	if _, err := f.store.Put(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	// 等待已有的一秒授权观察周期，期间上传保持未完成。
	time.Sleep(1200 * time.Millisecond)
	// 请求体关闭结束上传。
	writer.Close()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		cancel()
		t.Fatal("撤销后上传未结束")
	}
	data, _ := os.ReadFile(localSFTPFixturePath(p))
	if string(data) != "原文件" {
		t.Fatal("失败的上传损坏了旧文件")
	}
	deadline = time.Now().Add(3 * time.Second)
	for len(f.web.machineSlots) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(f.web.machineSlots) != 0 {
		t.Fatal("上传留下活动操作")
	}
}

func TestMachineUploadCancelCleansTemporaryFile(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	root := machineFile(t, f, map[string]string{"op": "list"}, 200)["path"].(string)
	p := path.Join(root, "canceled.txt")
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "PUT", f.http.URL+"/api/targets/test/transfer?path="+url.QueryEscape(p), reader)
	request.AddCookie(f.cookie)
	request.Header.Set("Content-Type", "application/octet-stream")
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, _ := f.http.Client().Do(request)
		if response != nil {
			response.Body.Close()
		}
	}()
	if _, err := writer.Write(bytes.Repeat([]byte("x"), 64<<10)); err != nil {
		t.Fatal(err)
	}
	hasTemp := func() bool {
		files, _ := os.ReadDir(localSFTPFixturePath(root))
		for _, file := range files {
			if strings.HasPrefix(file.Name(), ".ssh-gateway-") {
				return true
			}
		}
		return false
	}
	deadline := time.Now().Add(3 * time.Second)
	for !hasTemp() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !hasTemp() {
		t.Fatal("上传未创建临时文件")
	}
	cancel()
	writer.Close()
	<-done
	deadline = time.Now().Add(5 * time.Second)
	for (hasTemp() || len(f.web.machineSlots) > 0) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if hasTemp() || len(f.web.machineSlots) > 0 {
		t.Fatal("正常取消未清理临时文件和连接")
	}
	if _, err := os.Stat(localSFTPFixturePath(p)); !os.IsNotExist(err) {
		t.Fatal("取消上传留下不完整最终文件")
	}
}
