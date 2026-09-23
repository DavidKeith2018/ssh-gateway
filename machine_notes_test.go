package gateway

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRecordNotesPersistenceAndConflict(t *testing.T) {
	f := newWebFixture(t)
	request := func(body any, status int) map[string]string {
		t.Helper()
		r := f.request(t, "POST", "/api/targets/test/notes", body, nil)
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		if r.StatusCode != status {
			t.Fatalf("状态 %d，预期 %d：%s", r.StatusCode, status, data)
		}
		var result map[string]string
		json.Unmarshal(data, &result)
		return result
	}
	request(map[string]string{"op": "read"}, 401)
	f.login(t)
	in := f.input
	in.Enabled = false
	if _, err := f.store.Put(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	before, _ := f.store.Get(context.Background(), in.ID)
	empty := request(map[string]string{"op": "read"}, 200)
	saved := request(map[string]string{"op": "write", "content": "# 中文笔记", "version": empty["version"]}, 200)
	if _, ok := saved["path"]; ok {
		t.Fatal("笔记不应返回文件路径")
	}
	var content string
	if err := f.store.db.QueryRow(`SELECT notes FROM targets WHERE id=?`, in.ID).Scan(&content); err != nil || content != saved["content"] {
		t.Fatalf("数据库笔记错误：%v", err)
	}
	if _, err := os.Stat(filepath.Join(f.store.dir, "notes")); !os.IsNotExist(err) {
		t.Fatal("不应创建笔记目录")
	}
	after, _ := f.store.Get(context.Background(), in.ID)
	if before.Revision != after.Revision {
		t.Fatal("笔记保存改变连接版本")
	}
	request(map[string]string{"op": "write", "content": "旧版本", "version": empty["version"]}, 409)
	if got := request(map[string]string{"op": "read"}, 200); got["content"] != saved["content"] {
		t.Fatal("冲突丢失内容")
	}
	in.Name = "新名称"
	if _, err := f.store.Put(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if got := request(map[string]string{"op": "read"}, 200); got["content"] != saved["content"] {
		t.Fatal("配置更新丢失笔记")
	}
	request(map[string]any{"op": "write", "content": "确认覆盖", "overwrite": true}, 200)
	request(map[string]string{"op": "write", "content": strings.Repeat("x", editLimit+1)}, 413)
	request(map[string]string{"op": "delete"}, 400)
	request(map[string]string{"op": "read", "path": "/tmp/任意文件"}, 400)
	other, err := OpenStore(f.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	got, err := other.recordNote(context.Background(), in.ID, "read", "", "", false)
	if err != nil || got["content"] != "确认覆盖" {
		t.Fatal("重开数据库丢失笔记", err)
	}
	f.store.Delete(context.Background(), in.ID)
	if _, err = f.store.Put(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if got := request(map[string]string{"op": "read"}, 200); got["content"] != "" {
		t.Fatal("重建机器继承旧笔记")
	}
}
func TestNotesConcurrentSave(t *testing.T) {
	f := newWebFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, content := range []string{"甲", "乙"} {
		wg.Add(1)
		go func(content string) {
			defer wg.Done()
			_, err := f.store.recordNote(ctx, "test", "write", content, version(nil), false)
			results <- err
		}(content)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if e, ok := err.(*machineError); ok && e.code == 409 {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("并发写入未保护版本", success, conflict)
	}
}
func TestLegacyNotesMigration(t *testing.T) {
	for _, bad := range []bool{false, true} {
		t.Run(map[bool]string{false: "导入", true: "失败重试"}[bad], func(t *testing.T) {
			s, openErr := OpenStore(t.TempDir())
			if openErr != nil {
				t.Fatal(openErr)
			}
			in := testInput(t)
			if _, err := s.Put(context.Background(), in); err != nil {
				t.Fatal(err)
			}
			// 还原旧数据库结构及文件。
			if _, err := s.db.Exec(`DELETE FROM settings WHERE name='notes_database_migrated'; ALTER TABLE targets DROP COLUMN notes`); err != nil {
				t.Fatal(err)
			}
			dir := s.dir
			s.Close()
			noteDir := filepath.Join(dir, "notes")
			os.Mkdir(noteDir, 0700)
			p := filepath.Join(noteDir, in.ID+".md")
			data := []byte("旧笔记\n中文")
			if bad {
				data = []byte{0xff}
			}
			os.WriteFile(p, data, 0600)
			migrated, err := OpenStore(dir)
			if bad {
				if err == nil {
					migrated.Close()
					t.Fatal("应拒绝无效笔记")
				}
				os.WriteFile(p, []byte("旧笔记\n中文"), 0600)
				migrated, err = OpenStore(dir)
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := migrated.recordNote(context.Background(), in.ID, "read", "", "", false)
			if err != nil || got["content"] != "旧笔记\n中文" {
				t.Fatal(got, err)
			}
			if _, err = os.Stat(p); err != nil {
				t.Fatal("旧文件应保留")
			}
			migrated.recordNote(context.Background(), in.ID, "write", "", got["version"], false)
			migrated.Close()
			migrated, err = OpenStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer migrated.Close()
			got, err = migrated.recordNote(context.Background(), in.ID, "read", "", "", false)
			if err != nil || got["content"] != "" {
				t.Fatal("重复迁移恢复了已清空笔记")
			}
		})
	}
}
