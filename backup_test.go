package gateway

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupRoundTripAndFailures(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = LoadHostKey(s.dir); err != nil {
		t.Fatal(err)
	}
	in := testInput(t)
	if _, err = s.Put(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err = s.SetAdminPassword(ctx, "backup-admin-password"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.fileFavorites(ctx, "", in.ID, favoriteRequest{Op: "add", Entries: []fileFavorite{{Path: "/demo/folder", Kind: "directory"}}}); err != nil {
		t.Fatal(err)
	}
	data, err := s.ExportBackup(ctx, "backup-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(in.TargetPassword)) {
		t.Fatal("备份泄露密码")
	}
	info, err := InspectBackup(ctx, data, "backup-password-123")
	if err != nil || info.Targets != 1 {
		t.Fatalf("检查：%+v %v", info, err)
	}
	dir := t.TempDir()
	old, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	old.Close()
	previous, err := RestoreBackup(ctx, dir, data, "backup-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(previous, "gateway.db")); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	favorites, err := restored.fileFavorites(ctx, "", in.ID, favoriteRequest{Op: "read"})
	if err != nil || len(favorites) != 1 || favorites[0].Path != "/demo/folder" {
		t.Fatal("favorites backup restoration failed", err)
	}
	r, err := restored.get(ctx, "id", in.ID)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := restored.decrypt(r)
	if err != nil || secret != in.TargetPassword {
		t.Fatalf("凭证恢复失败：%v", err)
	}
	for _, broken := range [][]byte{data[:20], append([]byte(nil), data...)} {
		if len(broken) > 20 {
			broken[len(broken)-1] ^= 1
		}
		if _, err = InspectBackup(ctx, broken, "backup-password-123"); err == nil {
			t.Fatal("损坏备份未拒绝")
		}
	}
	if _, err = RestoreBackup(ctx, dir, data, "wrong-password-123"); err == nil {
		t.Fatal("错误密码未拒绝")
	}
	lock, err := AcquireInstance(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err = RestoreBackup(ctx, dir, data, "backup-password-123"); err == nil {
		t.Fatal("运行中恢复未拒绝")
	}
}

func TestBackupPreservesMasterProtectionAndDatabase(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = LoadHostKey(s.dir); err != nil {
		t.Fatal(err)
	}
	in := testInput(t)
	if _, err = s.Put(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE targets SET notes=? WHERE id=?", "恢复我的笔记", in.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("INSERT INTO settings(name,value) VALUES('backup_test',?)", "完整配置"); err != nil {
		t.Fatal(err)
	}
	if err = s.ChangeMasterPassword("", "original-master-password"); err != nil {
		t.Fatal(err)
	}
	data, err := s.ExportBackup(ctx, "independent-backup-password")
	if err != nil {
		t.Fatal(err)
	}
	info, err := InspectBackup(ctx, data, "independent-backup-password")
	if err != nil || !info.MasterProtected {
		t.Fatalf("%+v %v", info, err)
	}
	dir := t.TempDir()
	if _, err = RestoreBackup(ctx, dir, data, "independent-backup-password"); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if !restored.MasterPasswordStatus().Locked {
		t.Fatal("恢复后未保留主密码保护")
	}
	if err = restored.UnlockMasterPassword("independent-backup-password"); err == nil {
		t.Fatal("备份密码不应解锁主密码")
	}
	if err = restored.UnlockMasterPassword("original-master-password"); err != nil {
		t.Fatal(err)
	}
	var note, value string
	if err = restored.db.QueryRow("SELECT notes FROM targets WHERE id=?", in.ID).Scan(&note); err != nil || note != "恢复我的笔记" {
		t.Fatal("笔记未恢复")
	}
	if err = restored.db.QueryRow("SELECT value FROM settings WHERE name='backup_test'").Scan(&value); err != nil || value != "完整配置" {
		t.Fatal("设置未恢复")
	}
}

func TestRestoreRejectsOpenStoreAndInterruptedMarker(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = LoadHostKey(s.dir); err != nil {
		t.Fatal(err)
	}
	data, err := s.ExportBackup(ctx, "backup-maintenance-password")
	if err != nil {
		t.Fatal(err)
	}
	// 即便没有服务实例锁，命令行/库仍在使用数据库时也不能恢复。
	if _, err = RestoreBackup(ctx, s.dir, data, "backup-maintenance-password"); err == nil {
		t.Fatal("未拒绝正在使用的数据库")
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "restore.pending"), []byte("original-data"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = OpenStore(dir); err == nil {
		t.Fatal("恢复中断后仍可启动")
	}
	if _, err = RestoreBackup(ctx, dir, data, "backup-maintenance-password"); err == nil {
		t.Fatal("恢复中断后仍可覆盖现场")
	}
}

func TestRestoreKeepsRollbackInsideDataDirectory(t *testing.T) {
	ctx := context.Background()
	source, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err = LoadHostKey(source.dir); err != nil {
		t.Fatal(err)
	}
	data, err := source.ExportBackup(ctx, "backup-rollback-password")
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "data")
	original, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	original.Close()
	// 父目录不可写，数据目录仍可写；恢复不能要求在父目录创建回退目录。
	if err = os.Chmod(parent, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0700)
	previous, err := RestoreBackup(ctx, dir, data, "backup-rollback-password")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(previous) != dir {
		t.Fatalf("回退目录离开了数据目录所在文件系统：%s", previous)
	}
	if _, err = os.Stat(filepath.Join(previous, "gateway.db")); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	restored.Close()
}
