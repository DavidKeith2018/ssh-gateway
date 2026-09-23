package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	gateway "ssh-gateway"
	"ssh-gateway/localization"
	"ssh-gateway/updater"
)

type App struct {
	locale       string
	tray         *application.SystemTray
	trayMenu     *application.Menu
	showItem     *application.MenuItem
	quitItem     *application.MenuItem
	updates      updateState
	exit         exitState
	application  *application.App
	window       *application.WebviewWindow
	trayFailed   atomic.Bool
	windowReady  atomic.Bool
	jobs         gateway.MachineJobs
	overwrite    transferDecisions
	mu           sync.RWMutex
	ctx          context.Context
	core         *gateway.Desktop
	dir          string
	startupError string
}

func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.ctx = ctx
	a.open(a.dir)
	return nil
}
func (a *App) open(dir string) {
	core, err := gateway.OpenDesktop(a.ctx, dir)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.startupError = err.Error()
		return
	}
	if a.core != nil {
		a.core.Close()
	}
	a.core = core
	a.dir = dir
	a.startupError = ""
}
func (a *App) ServiceShutdown() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.core != nil {
		a.core.Close()
	}
	return nil
}
func (a *App) showWindow() {
	if !a.windowReady.Load() {
		return
	}
	a.mu.RLock()
	window := a.window
	a.mu.RUnlock()
	if window != nil {
		application.InvokeSync(func() {
			window.Show()
			window.UnMinimise()
			window.Focus()
		})
	}
}
func (a *App) shouldQuit() bool {
	a.mu.RLock()
	hasCore := a.core != nil
	a.mu.RUnlock()
	allow, notify := a.exit.request(hasCore)
	if !allow {
		a.showWindow()
	}
	if notify {
		a.application.Event.Emit("desktop:confirm-quit")
	}
	return allow
}
func (a *App) ConfirmQuit() { a.exit.confirm(); a.application.Quit() }
func (a *App) CancelQuit()  { a.exit.cancel() }

// FrontendReady 在退出事件订阅完成后握手，避免启动期间丢失退出请求。
func (a *App) FrontendReady() {
	if a.exit.ready() {
		a.application.Event.Emit("desktop:confirm-quit")
	}
}
func (a *App) closeWindow(event *application.WindowEvent) {
	event.Cancel()
	if a.exit.hideOnClose(!a.trayFailed.Load() && trayAvailable()) {
		application.InvokeSync(func() { a.window.Hide() })
	} else {
		a.Quit()
	}
}

func (a *App) Info() gateway.DesktopInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.core == nil {
		return gateway.LocalizeDesktopInfo(gateway.DesktopInfo{DataDir: a.dir, Error: a.startupError, Version: updater.Version})
	}
	info := a.core.Info()
	if a.startupError != "" {
		info.Error = a.startupError
	}
	return gateway.LocalizeDesktopInfo(info)
}
func (a *App) Call(method, path, body string) (gateway.DesktopReply, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.core == nil {
		return gateway.DesktopReply{}, fmt.Errorf("请先选择可用的数据目录")
	}
	return a.core.Call(method, path, body)
}
func (a *App) SaveSettings(settings gateway.DesktopSettings, confirmed bool) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.core == nil {
		return fmt.Errorf("服务未初始化")
	}
	return a.core.SaveSettings(settings, confirmed)
}
func (a *App) OpenTerminal(id, target string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.core == nil {
		return fmt.Errorf("服务未初始化")
	}
	return a.core.OpenTerminal(id, target, func(event gateway.TerminalEvent) { a.application.Event.Emit("terminal", event) })
}
func (a *App) OpenTerminalConnection(id, target, selection string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.core == nil {
		return fmt.Errorf("服务未初始化")
	}
	return a.core.OpenTerminal(id, target, func(event gateway.TerminalEvent) { a.application.Event.Emit("terminal", event) }, selection)
}
func (a *App) TerminalInput(id, data string, columns, rows int) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.core == nil {
		return fmt.Errorf("服务未初始化")
	}
	return a.core.TerminalInput(id, data, columns, rows)
}
func (a *App) AckTerminal(id string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.core != nil {
		a.core.AckTerminal(id)
	}
}
func (a *App) CloseTerminal(id string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.core != nil {
		a.core.CloseTerminal(id)
	}
}
func (a *App) Quit() { a.application.Quit() }

func preferencePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "ssh-gateway-desktop", "data-path")
}
func (a *App) ChooseDataDir() error {
	info := a.Info()
	if info.Ready && !info.NeedsSetup {
		return fmt.Errorf("已有数据目录，请退出应用后通过 -data 指定其他目录")
	}
	dir, err := a.application.Dialog.OpenFile().SetTitle(a.text("选择 SSH Gateway数据目录")).CanChooseDirectories(true).CanChooseFiles(false).AttachToWindow(a.window).PromptForSingleSelection()
	if err != nil || dir == "" {
		return err
	}
	if dir == a.dir {
		return nil
	}
	a.open(dir)
	if !a.Info().Ready || a.Info().DataDir != dir {
		return fmt.Errorf("所选目录无法打开")
	}
	p := preferencePath()
	if p != "" {
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(dir), 0600)
	}
	return nil
}
func runDesktop() {
	dir, err := gateway.DefaultDataDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if data, err := os.ReadFile(preferencePath()); err == nil && filepath.IsAbs(string(data)) {
		dir = string(data)
	}
	flag.StringVar(&dir, "data", dir, "数据目录")
	flag.Parse()
	dir, _ = filepath.Abs(dir)
	app := newDesktopApp(dir)
	err = app.application.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newDesktopApp(dir string) *App {
	app := &App{dir: dir}
	hash := sha256.Sum256([]byte(dir))
	unique := hex.EncodeToString(hash[:16])
	app.application = application.New(application.Options{
		MarshalError: localization.MarshalError,
		Name:         "SSH Gateway", Icon: gateway.DesktopIcon(),
		Services:   []application.Service{application.NewService(app)},
		Assets:     application.AssetOptions{Handler: application.AssetFileServerFS(desktopAssets())},
		ShouldQuit: app.shouldQuit,
		WarningHandler: func(message string) {
			fmt.Fprintln(os.Stderr, message)
			if runtime.GOOS != "linux" && strings.Contains(strings.ToLower(message), "systray") {
				app.trayFailed.Store(true)
			}
		},
		ErrorHandler: func(err error) {
			fmt.Fprintln(os.Stderr, err)
			if runtime.GOOS != "linux" && strings.Contains(strings.ToLower(err.Error()), "systray") {
				app.trayFailed.Store(true)
			}
		},
		Linux:          application.LinuxOptions{ProgramName: "ssh-gateway-desktop", DisableQuitOnLastWindowClosed: true},
		Windows:        application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: unique, OnSecondInstanceLaunch: func(application.SecondInstanceData) { app.showWindow() }},
	})
	window := app.application.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", Title: "SSH Gateway", Width: 1280, Height: 860, MinWidth: 800, MinHeight: 600,
	})
	app.mu.Lock()
	app.window = window
	app.mu.Unlock()
	app.window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) { app.windowReady.Store(true) })
	app.window.RegisterHook(events.Common.WindowClosing, app.closeWindow)
	tray := app.application.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayTemplateIcon)
	} else {
		tray.SetIcon(gateway.DesktopIcon())
	}
	tray.SetTooltip("SSH Gateway")
	menu := app.application.Menu.New()
	app.showItem = menu.Add("显示主窗口").OnClick(func(*application.Context) { app.showWindow() })
	menu.AddSeparator()
	app.quitItem = menu.Add("退出应用").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
	app.tray, app.trayMenu = tray, menu
	if runtime.GOOS != "darwin" {
		tray.OnClick(app.showWindow)
	}
	return app
}
