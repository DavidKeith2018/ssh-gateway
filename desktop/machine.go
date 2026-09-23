package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	gateway "ssh-gateway"
)

func (a *App) machineCore() (*gateway.Desktop, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.core == nil {
		return nil, fmt.Errorf("服务未初始化")
	}
	return a.core, nil
}
func (a *App) MachineCall(id, target, operation, body string) (gateway.DesktopReply, error) {
	return a.MachineCallConnection(id, target, operation, body, "")
}
func (a *App) MachineCallConnection(id, target, operation, body, query string) (gateway.DesktopReply, error) {
	ctx, done, err := a.jobs.Start(a.ctx, id)
	if err != nil {
		return gateway.DesktopReply{}, err
	}
	defer done()
	core, err := a.machineCore()
	if err != nil {
		return gateway.DesktopReply{}, err
	}
	return core.MachineCall(ctx, target, operation, body, query)
}
func (a *App) CancelWork(id string) { a.jobs.Cancel(id) }

type progressReader struct {
	input        io.Reader
	count, total int64
	emit         func(int)
	last         time.Time
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.input.Read(p)
	r.count += int64(n)
	if time.Since(r.last) > 200*time.Millisecond && r.total > 0 {
		r.emit(int(r.count * 100 / r.total))
		r.last = time.Now()
	}
	return n, err
}
func (a *App) TransferFile(id, target, mode, remote string) error {
	return a.TransferFileConnection(id, target, mode, remote, "")
}
func (a *App) TransferFileConnection(id, target, mode, remote, query string) error {
	ctx, done, err := a.jobs.Start(a.ctx, id)
	if err != nil {
		return err
	}
	defer done()
	core, err := a.machineCore()
	if err != nil {
		return err
	}
	if mode == "upload" {
		local, err := a.application.Dialog.OpenFile().SetTitle(a.text("选择上传文件（最大 1 GiB）")).AttachToWindow(a.window).PromptForSingleSelection()
		if err != nil {
			return err
		}
		if local == "" {
			return fmt.Errorf("已取消上传")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := os.Open(local)
		if err != nil {
			return err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() || st.Size() > 1<<30 {
			return fmt.Errorf("请选择不超过 1 GiB 的普通文件")
		}
		// 先检查远端重名，再等待共享中文对话框明确确认覆盖。
		destination := path.Join(remote, filepath.Base(local))
		body, _ := json.Marshal(map[string]string{"op": "list", "path": remote})
		reply, err := core.MachineCall(ctx, target, "files", string(body), query)
		if err != nil {
			return err
		}
		if reply.Status >= 400 {
			return replyError(reply)
		}
		var listing struct {
			Entries []struct {
				Name string `json:"name"`
			} `json:"entries"`
		}
		json.Unmarshal(reply.Data, &listing)
		overwrite := false
		for _, entry := range listing.Entries {
			if entry.Name == filepath.Base(local) {
				if err := a.overwrite.wait(ctx, id, func() {
					a.application.Event.Emit("file:confirm-overwrite", gateway.TerminalEvent{ID: id, Data: destination})
				}); err != nil {
					return err
				}
				overwrite = true
			}
		}
		reader := &progressReader{input: f, total: st.Size(), emit: func(percent int) {
			a.application.Event.Emit("file-progress", gateway.TerminalEvent{ID: id, Data: fmt.Sprint(percent)})
		}}
		err = core.Transfer(ctx, target, destination, true, overwrite, reader, io.Discard, query)
		if err == nil {
			a.application.Event.Emit("file-progress", gateway.TerminalEvent{ID: id, Data: "100"})
		}
		return err
	}
	if mode != "download" {
		return fmt.Errorf("不支持的传输操作")
	}
	dialog := a.application.Dialog.SaveFile()
	dialog.SetOptions(&application.SaveFileDialogOptions{Title: a.text("保存下载文件"), Filename: path.Base(remote), Window: a.window, CanCreateDirectories: true})
	local, err := dialog.PromptForSingleSelection()
	if err != nil {
		return err
	}
	if local == "" {
		return fmt.Errorf("已取消下载")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// 先写同目录临时文件，失败或取消不会留下不完整的目标文件。
	f, err := os.CreateTemp(filepath.Dir(local), ".ssh-gateway-download-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	err = core.Transfer(ctx, target, remote, false, false, nil, f, query)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(tmp, local)
}
func replyError(reply gateway.DesktopReply) error {
	var body struct {
		Error string `json:"error"`
	}
	json.Unmarshal(reply.Data, &body)
	return fmt.Errorf("%s", body.Error)
}

// OpenMachineWindow opens WebSSH in a separate application window.
func (a *App) OpenMachineWindow(target string) error {
	return a.openMachineWindow(target, "")
}

// OpenMachineConnectionWindow preserves the selected account in the new window.
func (a *App) OpenMachineConnectionWindow(target, selection string) error {
	return a.openMachineWindow(target, selection)
}

func (a *App) openMachineWindow(target, selection string) error {
	core, err := a.machineCore()
	if err != nil {
		return err
	}
	reply, err := core.Call("GET", "/targets/"+url.PathEscape(target), "")
	if err != nil {
		return err
	}
	if reply.Status != 200 {
		return replyError(reply)
	}
	var machine struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(reply.Data, &machine); err != nil {
		return err
	}
	address, err := core.MachineWindowURL(target, selection)
	if err != nil {
		return err
	}
	// Keep the one-use login ticket on the loopback server; never send it to an external browser.
	a.application.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: machine.Name + " — WebSSH", URL: a.windowLanguage(address),
		Width: 1440, Height: 960, MinWidth: 800, MinHeight: 600,
	})
	return nil
}

// OpenExternalURL 将网页交给系统浏览器，保留桌面工作区。
func (a *App) OpenExternalURL(address string) error {
	u, err := url.ParseRequestURI(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return fmt.Errorf("仅支持 HTTP 或 HTTPS 网页地址")
	}
	return a.application.Browser.OpenURL(u.String())
}

func (a *App) ConfirmOverwrite(id string, confirmed bool) { a.overwrite.reply(id, confirmed) }
