package main

import "testing"

func TestQuitWaitsForFrontendAndDeduplicates(t *testing.T) {
	var state exitState
	if allow, notify := state.request(true); allow || notify {
		t.Fatal("握手前应保留请求")
	}
	if !state.ready() {
		t.Fatal("握手后应发送待处理请求")
	}
	if state.ready() {
		t.Fatal("重复握手不应重复弹窗")
	}
	if allow, notify := state.request(true); allow || notify {
		t.Fatal("待确认时应合并退出请求")
	}
	state.cancel()
	if !state.hideOnClose(true) {
		t.Fatal("取消后不应仍在退出")
	}
	if allow, notify := state.request(true); allow || !notify {
		t.Fatal("取消后应可重新请求退出")
	}
	state.confirm()
	if allow, notify := state.request(true); !allow || notify {
		t.Fatal("确认后应退出")
	}
}

func TestQuitWithoutCore(t *testing.T) {
	var state exitState
	if allow, notify := state.request(false); !allow || notify {
		t.Fatal("初始化失败应允许退出")
	}
}

func TestCloseRequiresAvailableTray(t *testing.T) {
	var state exitState
	if !state.hideOnClose(true) {
		t.Fatal("可用托盘应隐藏窗口")
	}
	if state.hideOnClose(false) {
		t.Fatal("无托盘时应请求退出")
	}
	state.request(true)
	if state.hideOnClose(true) {
		t.Fatal("待确认时不能隐藏确认框")
	}
	state.cancel()
	if !state.hideOnClose(true) {
		t.Fatal("取消后应恢复隐藏行为")
	}
}
