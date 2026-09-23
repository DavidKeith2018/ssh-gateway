package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	gateway "ssh-gateway"
	"strings"
	"testing"
)

func TestBackupCommands(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	store, err := gateway.OpenStore(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = gateway.LoadHostKey(source); err != nil {
		t.Fatal(err)
	}
	data, err := store.ExportBackup(ctx, "backup-cli-password")
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "backup.sgb")
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "new-data")
	var output, diagnostics bytes.Buffer
	if err = run(ctx, []string{"-data", target, "backup-check", file}, strings.NewReader("backup-cli-password"), &output, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("检查备份不应创建目标目录")
	}
	if err = run(ctx, []string{"-data", target, "backup-restore", file}, strings.NewReader("backup-cli-password"), &output, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if err = run(ctx, []string{"-data", target, "backup-restore", file}, strings.NewReader("backup-cli-password"), &output, &diagnostics); err == nil {
		t.Fatal("覆盖应要求 -replace")
	}
	if err = run(ctx, []string{"-data", target, "-replace", "backup-restore", file}, strings.NewReader("backup-cli-password"), &output, &diagnostics); err != nil {
		t.Fatal(err)
	}
}
