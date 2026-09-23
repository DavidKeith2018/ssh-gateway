package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ssh-gateway/updater"
)

func TestVersionDoesNotOpenDataDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "不存在的数据目录")
	var output, diagnostics bytes.Buffer
	if err := run(context.Background(), []string{"-data", directory, "version"}, strings.NewReader(""), &output, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if output.String() != updater.Version+"\n" {
		t.Fatalf("版本输出错误：%q", output.String())
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("查看版本不应创建数据目录：%v", err)
	}
}
