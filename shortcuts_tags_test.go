package gateway

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func TestShortcutTagsMatchingAndPermissions(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	seedPagination(t, f, 3)
	admin := f.cookie
	create := func(name string, tags ...string) Shortcut {
		t.Helper()
		r := f.request(t, "POST", "/api/shortcuts", Shortcut{Name: name, Command: "pwd", Tags: tags}, nil)
		defer r.Body.Close()
		var item Shortcut
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil || r.StatusCode != 200 {
			t.Fatalf("创建失败 %d %v", r.StatusCode, err)
		}
		return item
	}
	one := create("单标签", "分组0")
	multi := create("多标签", " 分组0 ", "分组1", "分组0")
	orphan := create("暂不匹配", "未来标签")
	if !reflect.DeepEqual(multi.Tags, []string{"分组0", "分组1"}) {
		t.Fatal(multi)
	}
	check := func(path string, total int) {
		t.Helper()
		got := getPage[Shortcut](t, f, path)
		if got.Total != total {
			t.Fatalf("%s: 总数 %d，期望 %d", path, got.Total, total)
		}
	}
	check("/api/shortcuts?applicable_to=page-000", 3)
	check("/api/shortcuts?applicable_to=page-001", 2)
	check("/api/shortcuts?scope=tags&page_size=1", 3)
	check("/api/shortcuts?tag="+url.QueryEscape("分组0"), 2)
	check("/api/shortcuts?q="+url.QueryEscape("未来标签"), 1)
	// 同一机器匹配多个标签仍只计一次，机器标签更新立即影响下一次查询。
	if _, err := f.store.db.Exec(`UPDATE targets SET config=json_set(config,'$.tags',json('["分组0","分组1"]')) WHERE id='page-000'`); err != nil {
		t.Fatal(err)
	}
	check("/api/shortcuts?applicable_to=page-000&page_size=1", 3)
	if _, err := f.store.db.Exec(`UPDATE targets SET config=json_set(config,'$.tags',json('[]')) WHERE id='page-000'`); err != nil {
		t.Fatal(err)
	}
	check("/api/shortcuts?applicable_to=page-000", 1)
	_, err := f.store.saveAccount(context.Background(), "", accountInput{Username: "tag-reader", Password: testUserPassword, Enabled: true, TargetIDs: []string{"page-001"}})
	if err != nil {
		t.Fatal(err)
	}
	userLogin(t, f, "tag-reader")
	check("/api/shortcuts?scope=tags&page_size=1", 1)
	check("/api/shortcuts?tag="+url.QueryEscape("分组0"), 1)
	check("/api/shortcuts?q="+url.QueryEscape("暂不匹配"), 0)
	requireStatus(t, f, "GET", "/api/shortcuts?applicable_to=page-002", nil, 403)
	requireStatus(t, f, "PUT", "/api/shortcuts/"+multi.ID, multi, 403)
	requireStatus(t, f, "DELETE", "/api/shortcuts/"+multi.ID, nil, 403)
	f.cookie = admin
	// 删除最后一台匹配机器不会删除标签命令。
	requireStatus(t, f, "DELETE", "/api/targets/page-002", nil, 200)
	check("/api/shortcuts?scope=tags", 3)
	requireStatus(t, f, "PUT", "/api/shortcuts/"+one.ID, Shortcut{Name: one.Name, Command: "ls", TargetID: "*"}, 200)
	check("/api/shortcuts?scope=tags", 2)
	requireStatus(t, f, "PUT", "/api/shortcuts/"+one.ID, Shortcut{Name: one.Name, Command: "ls", Tags: []string{"分组1"}}, 200)
	requireStatus(t, f, "PUT", "/api/shortcuts/"+one.ID, Shortcut{Name: one.Name, Command: "ls", TargetID: "page-001"}, 200)
	check("/api/shortcuts?scope=tags", 2)
	requireStatus(t, f, "DELETE", "/api/shortcuts/"+orphan.ID, nil, 200)
	var count int
	if err := f.store.db.QueryRow("SELECT COUNT(*) FROM shortcut_tags WHERE shortcut_id IN (?,?)", one.ID, orphan.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("标签关联未清理 %d %v", count, err)
	}
}

func TestShortcutTagsValidation(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	for _, item := range []Shortcut{
		{TargetID: "*", Tags: []string{"分组0"}},
		{TargetID: "test", Tags: []string{"分组0"}},
		{Tags: []string{}},
		{Tags: []string{" "}},
		{Tags: []string{"非法\n标签"}},
	} {
		item.Name = "无效范围"
		item.Command = "pwd"
		requireStatus(t, f, "POST", "/api/shortcuts", item, 400)
	}
}

func TestShortcutTagsUpgradeAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟仅有旧快捷命令表的数据库。
	if _, err = s.db.Exec(`DROP TRIGGER delete_shortcut_tags; DROP TABLE shortcut_tags;
 INSERT INTO shortcuts(id,name,command,target_id) VALUES('legacy','旧命令','pwd','*')`); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	var target string
	if err = s.db.QueryRow("SELECT target_id FROM shortcuts WHERE id='legacy'").Scan(&target); err != nil || target != "*" {
		t.Fatalf("旧命令兼容失败 %q %v", target, err)
	}
	if _, err = s.db.Exec(`INSERT INTO shortcuts(id,name,command,target_id) VALUES('tagged','标签命令','ls','');
 INSERT INTO shortcut_tags VALUES('tagged','生产')`); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var tag string
	if err = s.db.QueryRow("SELECT tag FROM shortcut_tags WHERE shortcut_id='tagged'").Scan(&tag); err != nil || tag != "生产" {
		t.Fatalf("标签未持久保存 %q %v", tag, err)
	}
	if _, err = s.db.Exec("DELETE FROM shortcuts WHERE id='tagged'"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM shortcut_tags").Scan(&count); err != nil || count != 0 {
		t.Fatalf("升级后清理失败 %d %v", count, err)
	}
}
