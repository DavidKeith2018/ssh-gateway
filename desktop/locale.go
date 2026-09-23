package main

import (
	"net/url"
	"ssh-gateway/localization"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// SetLocale 更新界面语言，保留核心服务和现有会话。
func (a *App) SetLocale(value string) {
	value = localization.Normalize(value)
	a.mu.Lock()
	a.locale = value
	window, tray, menu := a.window, a.tray, a.trayMenu
	show, quit := a.showItem, a.quitItem
	a.mu.Unlock()
	application.InvokeSync(func() {
		if window != nil {
			window.SetTitle(localization.Text(value, "SSH Gateway"))
		}
		if show != nil {
			show.SetLabel(localization.Text(value, "显示主窗口"))
		}
		if quit != nil {
			quit.SetLabel(localization.Text(value, "退出应用"))
		}
		if tray != nil {
			tray.SetTooltip(localization.Text(value, "SSH Gateway"))
			tray.SetMenu(menu)
		}
	})
}

func (a *App) text(source string) string {
	a.mu.RLock()
	value := a.locale
	a.mu.RUnlock()
	return localization.Text(value, source)
}

func (a *App) windowLanguage(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return address
	}
	a.mu.RLock()
	value := localization.Normalize(a.locale)
	a.mu.RUnlock()
	query := u.Query()
	query.Set("lang", value)
	u.RawQuery = query.Encode()
	return u.String()
}
