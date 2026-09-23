package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/wailsapp/wails/v3/pkg/application"
	"os"
	"path/filepath"
)

// SaveBackup 只保存已经加密的备份；路径由系统保存窗口选择。
func (a *App) SaveBackup(encoded string) (bool, error) {
	if len(encoded) > 90<<20 {
		return false, fmt.Errorf("备份过大")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) < 52 || !bytes.HasPrefix(data, []byte("SGBACK01")) {
		return false, fmt.Errorf("备份格式无效")
	}
	dialog := a.application.Dialog.SaveFile()
	dialog.SetOptions(&application.SaveFileDialogOptions{Title: a.text("保存加密备份"), Filename: "ssh-gateway-backup.sgb", Window: a.window, CanCreateDirectories: true})
	local, err := dialog.PromptForSingleSelection()
	if err != nil {
		return false, err
	}
	if local == "" {
		return false, nil
	}
	file, err := os.CreateTemp(filepath.Dir(local), ".ssh-gateway-backup-")
	if err != nil {
		return false, err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return false, err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return false, err
	}
	if err = file.Close(); err != nil {
		return false, err
	}
	if err = os.Rename(file.Name(), local); err != nil {
		return false, err
	}
	return true, nil
}
