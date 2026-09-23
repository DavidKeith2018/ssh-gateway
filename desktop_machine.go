package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// 机器请求不持有桌面管理锁，避免文件传输阻塞终端 ACK。
func (d *Desktop) machineRequest(parent context.Context, target, operation string, body io.Reader) (*http.Request, http.Handler, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, nil, fmt.Errorf("应用已退出")
	}
	if !identifier.MatchString(target) {
		return nil, nil, fmt.Errorf("目标 ID 无效")
	}
	method := "POST"
	if operation == "resources" || operation == "hardware" {
		method = "GET"
	}
	r, err := http.NewRequestWithContext(parent, method, "http://localhost/api/targets/"+target+"/"+operation, body)
	if err != nil {
		return nil, nil, err
	}
	r.RemoteAddr = "127.0.0.1:0"
	r.Header.Set("Content-Type", "application/json")
	if d.cookie != nil {
		r.AddCookie(d.cookie)
	}
	d.workers.Add(1)
	return r, d.handler, nil
}
func (d *Desktop) MachineCall(ctx context.Context, target, operation, body string, queries ...string) (DesktopReply, error) {
	if operation != "favorites" && operation != "notes" && operation != "files" && operation != "resources" && operation != "hardware" {
		return DesktopReply{}, fmt.Errorf("不支持的机器操作")
	}
	if len(body) > 12<<20 {
		return DesktopReply{}, fmt.Errorf("请求内容过大")
	}
	r, handler, err := d.machineRequest(ctx, target, operation, strings.NewReader(body))
	if err != nil {
		return DesktopReply{}, err
	}
	defer d.workers.Done()
	if len(queries) > 0 {
		r.URL.RawQuery = queries[0]
	}
	response := &nativeResponse{header: make(http.Header)}
	handler.ServeHTTP(response, r)
	if !json.Valid(response.Bytes()) {
		return DesktopReply{}, fmt.Errorf("机器响应无效")
	}
	return DesktopReply{response.code, append(json.RawMessage(nil), response.Bytes()...)}, nil
}

type transferResponse struct {
	header     http.Header
	code       int
	output     io.Writer
	failure    strings.Builder
	written    int64
	writeError error
}

func (w *transferResponse) Header() http.Header { return w.header }
func (w *transferResponse) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}
func (w *transferResponse) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = 200
	}
	if w.code >= 400 {
		return w.failure.Write(b)
	}
	n, err := w.output.Write(b)
	w.written += int64(n)
	w.writeError = err
	return n, err
}
func (d *Desktop) Transfer(ctx context.Context, target, remote string, upload, overwrite bool, input io.Reader, output io.Writer, queries ...string) error {
	r, handler, err := d.machineRequest(ctx, target, "transfer", input)
	if err != nil {
		return err
	}
	defer d.workers.Done()
	r.Method = "GET"
	if upload {
		r.Method = "PUT"
		r.Header.Set("Content-Type", "application/octet-stream")
	}
	q := url.Values{}
	if len(queries) > 0 {
		q, _ = url.ParseQuery(queries[0])
	}
	q.Set("path", remote)
	q.Set("overwrite", fmt.Sprint(overwrite))
	r.URL.RawQuery = q.Encode()
	w := &transferResponse{header: make(http.Header), output: output}
	handler.ServeHTTP(w, r)
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.writeError != nil {
		return w.writeError
	}
	if !upload && w.code < 400 {
		expected, err := strconv.ParseInt(w.header.Get("Content-Length"), 10, 64)
		if err != nil || expected != w.written {
			return fmt.Errorf("下载中断，文件未保存")
		}
	}
	if w.code >= 400 {
		var body struct {
			Error string `json:"error"`
		}
		json.Unmarshal([]byte(w.failure.String()), &body)
		return fmt.Errorf("%s", body.Error)
	}
	return nil
}

// 桌面包装层使用请求 ID 取消网络调用；取消先于启动到达也会被记住。
type MachineJobs struct {
	mu       sync.Mutex
	jobs     map[string]context.CancelFunc
	canceled map[string]bool
}

func (j *MachineJobs) Start(parent context.Context, id string) (context.Context, func(), error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.jobs == nil {
		j.jobs = make(map[string]context.CancelFunc)
		if j.canceled == nil {
			j.canceled = make(map[string]bool)
		}
	}
	if id == "" || len(id) > 100 || len(j.jobs) >= 32 {
		return nil, nil, fmt.Errorf("请求无效或操作过多")
	}
	if _, ok := j.jobs[id]; ok {
		return nil, nil, fmt.Errorf("重复请求")
	}
	ctx, cancel := context.WithCancel(parent)
	if j.canceled[id] {
		cancel()
		delete(j.canceled, id)
	}
	j.jobs[id] = cancel
	return ctx, func() { cancel(); j.mu.Lock(); delete(j.jobs, id); j.mu.Unlock() }, nil
}
func (j *MachineJobs) Cancel(id string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if cancel := j.jobs[id]; cancel != nil {
		cancel()
	} else {
		if j.canceled == nil {
			j.canceled = make(map[string]bool)
		}
		if len(j.canceled) < 128 {
			j.canceled[id] = true
		}
	}
}
