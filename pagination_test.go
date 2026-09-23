package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"
)

func getPage[T any](t *testing.T, f *webFixture, path string) pageResult[T] {
	t.Helper()
	r := f.request(t, "GET", path, nil, nil)
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("%s 状态 %d", path, r.StatusCode)
	}
	var result pageResult[T]
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
func seedPagination(t *testing.T, f *webFixture, n int) {
	t.Helper()
	target, err := f.store.Get(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM events; DELETE FROM shortcuts"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("page-%03d", i)
		target.ID = id
		target.Name = "分页机器 " + id
		target.Tags = []string{fmt.Sprintf("分组%d", i%2)}
		raw, _ := json.Marshal(target)
		_, err = tx.Exec(`INSERT INTO targets(id,config,relay_user,password,password_hash,revision) SELECT ?,?,?,password,password_hash,revision FROM targets WHERE id='test'`, id, string(raw), id)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(`INSERT INTO users(id,username,password_hash,enabled,epoch) VALUES(?,?,X'00',1,0)`, id, id)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(`INSERT INTO global_ips(ip,expires_at) VALUES(?,?)`, fmt.Sprintf("192.0.2.%03d", i), time.Now().Add(time.Hour).UnixNano())
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(`INSERT INTO events(time,target,source,message) VALUES('2026-09-14T00:00:00Z',?,'127.0.0.1',?)`, id, id)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(`INSERT INTO shortcuts(id,name,command,target_id) VALUES(?,?,?,?)`, id, id, "echo "+id, id)
		if err != nil {
			t.Fatal(err)
		}
		m := Mapping{ID: id, TargetID: id, Name: id, Direction: "local", ServiceHost: "127.0.0.1", ServicePort: 3000, ListenPort: 20000 + i, Scope: "loopback"}
		config, _ := json.Marshal(m)
		_, err = tx.Exec(`INSERT INTO mappings(id,target_id,config,source_ip,revision,listener_key,listen_port) VALUES(?,?,?,'127.0.0.1',1,?,?)`, id, id, string(config), id, m.ListenPort)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
func TestAllServerPagination(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	seedPagination(t, f, 125)
	for _, path := range []string{"targets", "users", "global-ips", "events", "mappings", "shortcuts"} {
		t.Run(path, func(t *testing.T) {
			total := 125
			if path == "targets" {
				total++
			}
			first := getPage[map[string]any](t, f, "/api/"+path)
			if first.Total != total || len(first.Items) != 20 || first.Page != 1 || first.PageSize != 20 {
				t.Fatalf("默认分页错误 %+v", first)
			}
			second := getPage[map[string]any](t, f, "/api/"+path+"?page=2&page_size=20")
			if len(second.Items) != 20 || fmt.Sprint(first.Items[0]) == fmt.Sprint(second.Items[0]) {
				t.Fatal("翻页未改变数据")
			}
			last := getPage[map[string]any](t, f, "/api/"+path+"?page=999999999&page_size=20")
			if last.Page != 7 || len(last.Items) != total-120 {
				t.Fatalf("末页回退错误 %+v", last)
			}
			for _, bad := range []string{"page=0", "page=-1", "page=x", "page=1.5", "page=", "page=1&page=2", "page_size=0", "page_size=101", "page_size=x", "page=999999999999999999999999999"} {
				requireStatus(t, f, "GET", "/api/"+path+"?"+bad, nil, 400)
			}
		})
	}
	result := getPage[Target](t, f, "/api/targets?q="+url.QueryEscape("分页机器")+"&tag="+url.QueryEscape("分组1")+"&page=2&page_size=10")
	if result.Items[0].Revision < 1 {
		t.Fatal("分页丢失配置版本")
	}
	if result.Total != 62 || len(result.Items) != 10 || result.Items[0].ID != "page-021" {
		t.Fatalf("机器筛选错误 %+v", result)
	}
	mappings := getPage[MappingView](t, f, "/api/mappings?target_id=page-124&status=stopped&q=3000")
	if mappings.Total != 1 || mappings.Items[0].ID != "page-124" {
		t.Fatal(mappings)
	}
	commands := getPage[Shortcut](t, f, "/api/shortcuts?q=page-12&page_size=10")
	if commands.Total != 5 {
		t.Fatal(commands)
	}
	empty := getPage[Target](t, f, "/api/targets?q=不存在&page=50")
	if empty.Total != 0 || empty.Page != 1 || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatal(empty)
	}
	// 删除末页最后一条后自动回到上一页。
	if _, err := f.store.db.Exec("DELETE FROM shortcuts WHERE seq > (SELECT seq FROM shortcuts ORDER BY seq LIMIT 1 OFFSET 120)"); err != nil {
		t.Fatal(err)
	}
	before := getPage[Shortcut](t, f, "/api/shortcuts?page=7")
	if len(before.Items) != 1 {
		t.Fatal(before)
	}
	requireStatus(t, f, "DELETE", "/api/shortcuts/"+before.Items[0].ID, nil, 200)
	after := getPage[Shortcut](t, f, "/api/shortcuts?page=7")
	if after.Page != 6 || len(after.Items) != 20 {
		t.Fatal(after)
	}
	// 选项和汇总不受当前页限制。
	r := f.request(t, "GET", "/api/targets/options", nil, nil)
	var opts []targetOption
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil || len(opts) != 126 {
		t.Fatalf("选项不完整 %d %v", len(opts), err)
	}
	requireStatus(t, f, "GET", "/api/targets/page-124", nil, 200)
	r = f.request(t, "GET", "/api/mappings/summary", nil, nil)
	var summary mappingSummary
	if err := json.NewDecoder(r.Body).Decode(&summary); err != nil || summary.Total != 125 || summary.ByTarget["page-124"].Total != 1 {
		t.Fatalf("映射统计错误 %+v %v", summary, err)
	}
}

func TestUnicodeSearchBeforePagination(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	for i := 0; i < 2; i++ {
		in := f.input
		in.ID = fmt.Sprintf("unicode-%d", i)
		in.RelayUser = in.ID
		in.Name = "Équipe Äpfel ТЕСТ"
		in.Tags = []string{"Étiquette"}
		if _, err := f.store.Put(context.Background(), in); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, f, "POST", "/api/shortcuts", Shortcut{Name: "Übung", Command: "echo ЖУРНАЛ", TargetID: in.ID}, 200)
	}
	for _, tc := range []struct{ path, query string }{
		{"targets", "Équipe"}, {"targets", "équipe"}, {"targets", "ÄPFEL"}, {"targets", "тест"}, {"targets", "ÉTIQUETTE"},
		{"shortcuts", "Übung"}, {"shortcuts", "übung"}, {"shortcuts", "журнал"}, {"shortcuts", "Équipe"},
	} {
		t.Run(tc.path+"/"+tc.query, func(t *testing.T) {
			path := "/api/" + tc.path + "?q=" + url.QueryEscape(tc.query) + "&page_size=1"
			first := getPage[map[string]any](t, f, path)
			second := getPage[map[string]any](t, f, path+"&page=2")
			if first.Total != 2 || second.Total != 2 || len(first.Items) != 1 || len(second.Items) != 1 || second.Page != 2 {
				t.Fatalf("Unicode 搜索结果或分页错误：%+v，%+v", first, second)
			}
			if first.Items[0]["id"] == second.Items[0]["id"] {
				t.Fatal("分页重复返回了同一条数据")
			}
		})
	}
}
func TestShortcutPermissionsAndVisibilityBeforePaging(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	seedPagination(t, f, 25)
	admin := f.cookie
	id, err := f.store.saveAccount(context.Background(), "", accountInput{Username: "pager", Password: testUserPassword, Enabled: true, TargetIDs: []string{"page-024"}})
	if err != nil {
		t.Fatal(err)
	}
	response := f.request(t, "POST", "/api/shortcuts", Shortcut{Name: "全局", Command: "pwd", TargetID: "*"}, nil)
	var global Shortcut
	if err = json.NewDecoder(response.Body).Decode(&global); err != nil || response.StatusCode != 200 {
		t.Fatal(err)
	}
	requireStatus(t, f, "POST", "/api/shortcuts", Shortcut{Name: "错误", Command: "pwd\nls", TargetID: "*"}, 400)
	requireStatus(t, f, "POST", "/api/shortcuts", Shortcut{Name: "错误", Command: "pwd", TargetID: "missing"}, 400)
	userLogin(t, f, "pager")
	targets := getPage[Target](t, f, "/api/targets?page_size=1")
	if targets.Total != 1 || len(targets.Items) != 1 || targets.Items[0].ID != "page-024" {
		t.Fatal(targets)
	}
	commands := getPage[Shortcut](t, f, "/api/shortcuts?page_size=1")
	if commands.Total != 2 || commands.Items[0].TargetID != "page-024" {
		t.Fatal(commands)
	}
	hidden := getPage[Shortcut](t, f, "/api/shortcuts?target_id=page-000&q=page")
	if hidden.Total != 0 {
		t.Fatal("泄露未授权命令")
	}
	applicable := getPage[Shortcut](t, f, "/api/shortcuts?applicable_to=page-024")
	if applicable.Total != 2 {
		t.Fatal(applicable)
	}
	requireStatus(t, f, "GET", "/api/shortcuts?applicable_to=page-000", nil, 403)
	requireStatus(t, f, "GET", "/api/targets/page-000", nil, 403)
	for _, op := range []struct{ method, path string }{{"POST", "/api/shortcuts"}, {"PUT", "/api/shortcuts/" + global.ID}, {"DELETE", "/api/shortcuts/" + global.ID}} {
		requireStatus(t, f, op.method, op.path, Shortcut{Name: "篡改", Command: "ls", TargetID: "*"}, 403)
	}
	// 标签授权也在分页前生效。
	f.cookie = admin
	_, err = f.store.saveAccount(context.Background(), id, accountInput{Username: "pager", Enabled: true, Tags: []string{"分组1"}})
	if err != nil {
		t.Fatal(err)
	}
	userLogin(t, f, "pager")
	tagged := getPage[Target](t, f, "/api/targets?page_size=1&page=2")
	if tagged.Total != 12 || tagged.Items[0].ID != "page-003" {
		t.Fatal(tagged)
	}
	f.cookie = admin
	_, err = f.store.saveAccount(context.Background(), id, accountInput{Username: "pager", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	userLogin(t, f, "pager")
	noMachines := getPage[Shortcut](t, f, "/api/shortcuts")
	if noMachines.Total != 0 {
		t.Fatal("无机器授权的账号不应获得快捷命令")
	}

	f.cookie = admin
	requireStatus(t, f, "DELETE", "/api/targets/page-024", nil, 200)
	removed := getPage[Shortcut](t, f, "/api/shortcuts?target_id=page-024")
	if removed.Total != 0 {
		t.Fatal("删除机器未清理命令")
	}
	remaining := getPage[Shortcut](t, f, "/api/shortcuts?target_id=*")
	if remaining.Total != 1 {
		t.Fatal("全局命令丢失")
	}
	requireStatus(t, f, "PUT", "/api/shortcuts/"+global.ID, Shortcut{Name: "已修改", Command: "ls", TargetID: "*"}, 200)
	requireStatus(t, f, "PUT", "/api/shortcuts/missing", Shortcut{Name: "已修改", Command: "ls", TargetID: "*"}, 404)
}
func TestDesktopPaginationQuery(t *testing.T) {
	d := freshDesktop(t)
	loginDesktop(t, d)
	reply := desktopCall(t, d, "GET", "/targets?page=2&page_size=10&q="+url.QueryEscape("中文 + & / ?"), nil)
	var page pageResult[Target]
	if err := json.Unmarshal(reply.Data, &page); err != nil || reply.Status != 200 || page.PageSize != 10 || page.Page != 1 {
		t.Fatalf("桌面查询失败 %+v %v", reply, err)
	}
	for _, path := range []string{"https://example.com/targets?page=1", "//example.com/targets", "/targets#x", "/targets?q=%zz", "/targets/../../terminal?page=1"} {
		if _, err := d.Call("GET", path, ""); err == nil {
			t.Fatalf("接受非法路径 %s", path)
		}
	}
}

func TestShortcutDatabasePersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM shortcuts").Scan(&count); err != nil || count != 5 {
		t.Fatalf("默认命令数量错误 %d %v", count, err)
	}
	if _, err = s.db.Exec("INSERT INTO shortcuts(id,name,command,target_id) VALUES('saved','持久命令','pwd','*')"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var name string
	if err = reopened.db.QueryRow("SELECT name FROM shortcuts WHERE id='saved'").Scan(&name); err != nil || name != "持久命令" {
		t.Fatalf("命令未持久保存 %s %v", name, err)
	}
}

func TestMappingStatusPaginationAndUnfilteredSummary(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	seedPagination(t, f, 25)
	m := f.web.mappings
	m.mu.Lock()
	m.runs["page-023"] = &mappingRun{config: Mapping{Revision: 1}, status: "running"}
	m.runs["page-024"] = &mappingRun{config: Mapping{Revision: 1}, status: "error", failure: "测试失败"}
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.runs, "page-023"); delete(m.runs, "page-024"); m.mu.Unlock() }()
	r := f.request(t, "GET", "/api/mappings?status=running&page=10&page_size=1", nil, nil)
	var result struct {
		pageResult[MappingView]
		Summary mappingSummary `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Page != 1 || len(result.Items) != 1 || result.Items[0].ID != "page-023" {
		t.Fatalf("状态筛选未在分页前执行 %+v", result)
	}
	if result.Summary.Total != 25 || result.Summary.Stopped != 23 || result.Summary.Running != 1 || result.Summary.Error != 1 || result.Summary.ByTarget["page-023"].Running != 1 {
		t.Fatalf("统计受分页筛选影响 %+v", result.Summary)
	}
}

func TestTargetsOrderedByNameAcrossPages(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	seedPagination(t, f, 4)
	for i, name := range []string{"Zulu", "alpha", "Bravo", "alpha"} {
		if _, err := f.store.db.Exec(`UPDATE targets SET config=json_set(config,'$.name',?) WHERE id=?`, name, fmt.Sprintf("page-%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	// 名称顺序与编号、创建顺序不同；同名使用编号保持跨页稳定。
	want := []string{"page-001", "page-003", "page-002", "page-000"}
	for page := 1; page <= 2; page++ {
		got := getPage[Target](t, f, fmt.Sprintf("/api/targets?q=page-&page=%d&page_size=2", page))
		if got.Total != 4 || len(got.Items) != 2 {
			t.Fatalf("分页错误：%+v", got)
		}
		for i, item := range got.Items {
			if item.ID != want[(page-1)*2+i] {
				t.Fatalf("第 %d 页顺序错误：%s", page, item.ID)
			}
		}
	}
	if _, err := f.store.db.Exec(`UPDATE targets SET config=json_set(config,'$.name','Aardvark') WHERE id='page-000'`); err != nil {
		t.Fatal(err)
	}
	got := getPage[Target](t, f, "/api/targets?q=page-&page_size=2")
	if got.Items[0].ID != "page-000" {
		t.Fatal("重命名后未重新排序")
	}
}
