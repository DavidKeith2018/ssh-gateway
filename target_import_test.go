package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestImportPreviewAndAtomicCommit(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, content := range []string{"Host *\n User root", "Include ~/.ssh/config.d/*\nHost a\nHostName localhost", "Host a\nProxyCommand touch /tmp/never-run"} {
		rows, err := s.PreviewImport(ctx, "ssh_config", content)
		if err != nil {
			t.Fatal(err)
		}
		if !rows[0].Blocked {
			t.Fatalf("未阻止不支持配置：%s", content)
		}
	}
	rows, err := s.PreviewImport(ctx, "ssh_config", "Host office\n HostName example.com\n Port 2222\n User deploy\n IdentityFile \"~/.ssh/my key\"\n")
	if err != nil || rows[0].Blocked || rows[0].Input.Port != 2222 || rows[0].IdentityFile != "~/.ssh/my key" || len(rows[0].Issues) == 0 {
		t.Fatalf("预览：%+v %v", rows, err)
	}
	a := testInput(t)
	a.ID = "ignored-id"
	a.RelayUser = ""
	b := a
	b.Host = "192.0.2.2"
	b.TargetPassword = ""
	if _, err = s.ImportTargets(ctx, []PutInput{a, b}); err == nil {
		t.Fatal("应整批拒绝缺少密码")
	}
	all, _ := s.List(ctx)
	if len(all) != 0 {
		t.Fatal("部分写入")
	}
	b.TargetPassword = "valid-target-password"
	ids, err := s.ImportTargets(ctx, []PutInput{a, b})
	if err != nil || len(ids) != 2 {
		t.Fatalf("导入失败：%v", err)
	}
	if ids[0] == a.ID {
		t.Fatal("保留了外部记录编号")
	}
	r, err := s.get(ctx, "id", ids[0])
	if err != nil {
		t.Fatal(err)
	}
	secret, err := s.decrypt(r)
	if err != nil || secret != a.TargetPassword {
		t.Fatal("导入凭证不可解密")
	}
	raw, _ := json.Marshal([]PutInput{a})
	rows, err = s.PreviewImport(ctx, "json", string(raw))
	if err != nil || !rows[0].Duplicate {
		t.Fatalf("重复预览：%+v %v", rows, err)
	}
	c := a
	c.Host = "192.0.2.3"
	if _, err = s.ImportTargets(ctx, []PutInput{c, a}); err == nil {
		t.Fatal("重复冲突未拒绝")
	}
	all, _ = s.List(ctx)
	if len(all) != 2 {
		t.Fatal("重复冲突后未回滚")
	}
}

func TestImportStagingNeverPersistsMasterKey(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.ChangeMasterPassword("", "import-master-password"); err != nil {
		t.Fatal(err)
	}
	stage, cleanup, err := s.importStore()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err = os.Stat(filepath.Join(stage.dir, "master.key")); !os.IsNotExist(err) {
		t.Fatalf("暂存库不应写入密钥文件：%v", err)
	}
	input := testInput(t)
	if _, err = stage.PutTarget(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(stage.dir, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if bytes.Contains(data, s.dataKey) {
			t.Errorf("临时文件含裸数据密钥：%s", entry.Name())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := s.ImportTargets(context.Background(), []PutInput{input})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := s.get(context.Background(), "id", ids[0])
	if err != nil {
		t.Fatal(err)
	}
	secret, err := s.decrypt(rec)
	if err != nil || secret != input.TargetPassword {
		t.Fatalf("主密码启用后导入凭证不可解密：%v", err)
	}
}

func TestImportSSHAssignmentSeparators(t *testing.T) {
	for _, separator := range []string{" ", "=", " = ", " =", "= "} {
		rows, err := parseImport("ssh_config", "Host"+separator+"office\nHostName"+separator+"192.0.2.1\nUser"+separator+"deploy\nPort"+separator+"2222\nIdentityFile"+separator+"\"~/.ssh/key=a\"\n")
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || len(rows[0].Issues) != 0 || rows[0].Input.Host != "192.0.2.1" || rows[0].Input.User != "deploy" || rows[0].Input.Port != 2222 || rows[0].IdentityFile != "~/.ssh/key=a" {
			t.Fatalf("分隔符 %q 解析错误：%+v", separator, rows)
		}
	}
}
