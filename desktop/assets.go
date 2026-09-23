package main

import (
	"embed"
	"io/fs"
)

// 保留 frontend 目录可让全新检出在前端构建前生成绑定。
//
//go:embed all:frontend
var frontendAssets embed.FS

//go:embed tray-template.png
var trayTemplateIcon []byte

func desktopAssets() fs.FS {
	assets, err := fs.Sub(frontendAssets, "frontend/dist")
	if err != nil {
		panic(err)
	}
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		panic("桌面前端尚未构建，请先执行 npm --prefix web run build:desktop")
	}
	return assets
}
