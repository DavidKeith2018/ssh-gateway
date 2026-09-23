//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// 必须同时存在托盘宿主和本进程的注册项，才允许隐藏窗口。
// 每次关闭重新检查，兼容桌面面板重启和托盘扩展被关闭。
func trayAvailable() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	watcher := conn.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher")
	get := func(name string) (dbus.Variant, error) {
		var value dbus.Variant
		err := watcher.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.kde.StatusNotifierWatcher", name).Store(&value)
		return value, err
	}
	host, err := get("IsStatusNotifierHostRegistered")
	if err != nil || host.Value() != true {
		return false
	}
	items, err := get("RegisteredStatusNotifierItems")
	if err != nil {
		return false
	}
	list, ok := items.Value().([]string)
	if !ok {
		return false
	}
	names := append([]string{fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())}, conn.Names()...)
	for _, item := range list {
		for _, name := range names {
			if item == name || strings.HasPrefix(item, name+"/") {
				return true
			}
		}
	}
	return false
}
