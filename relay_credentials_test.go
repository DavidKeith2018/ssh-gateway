package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestRelayPasswordsPersistenceAndUpdates(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	in := multiLoginInput(t)
	created, err := s.PutTarget(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := s.db.QueryRow(`SELECT passwords FROM relay_passwords WHERE target_id=?`, in.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	for i, credential := range created.Credentials {
		if strings.Contains(stored, credential.Password) || !strings.HasPrefix(stored, relayCipherPrefix) {
			t.Fatal("中转密码未加密保存")
		}
		if !strings.HasPrefix(credential.Username, "relay-"+in.LoginInputs[i].User+"-") {
			t.Fatal("自动中转名称未包含绑定账号")
		}
	}
	if strings.Contains(stored, in.LoginInputs[0].TargetPassword) || strings.Contains(stored, in.LoginInputs[1].TargetPrivateKey) {
		t.Fatal("中转表不应保存原始目标凭证")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	passwords, err := s.relayPasswords(ctx, in.ID)
	if err != nil || passwords[created.Credentials[0].ID] != created.Credentials[0].Password {
		t.Fatal("重开数据库后中转密码丢失")
	}
	update := PutInput{Target: *created.Target}
	update.Name = "改名保留中转密码"
	result, err := s.PutTarget(ctx, update)
	if err != nil {
		t.Fatal(err)
	}
	passwords, _ = s.relayPasswords(ctx, in.ID)
	if passwords[created.Credentials[1].ID] != created.Credentials[1].Password {
		t.Fatal("编辑导致另一账号密码丢失")
	}
	update.Target = *result.Target
	for _, relay := range result.Target.Relays {
		update.RelayInputs = append(update.RelayInputs, TargetRelayInput{TargetRelay: relay})
	}
	update.RelayInputs[0].Password = "replacement-relay-password"
	result, err = s.PutTarget(ctx, update)
	if err != nil {
		t.Fatal(err)
	}
	passwords, _ = s.relayPasswords(ctx, in.ID)
	if passwords[created.Credentials[0].ID] != update.RelayInputs[0].Password || passwords[created.Credentials[1].ID] != created.Credentials[1].Password {
		t.Fatal("密码更新未按凭证隔离")
	}
	update.Target = *result.Target
	update.RelayInputs = update.RelayInputs[:1]
	if _, err := s.PutTarget(ctx, update); err != nil {
		t.Fatal(err)
	}
	passwords, _ = s.relayPasswords(ctx, in.ID)
	if len(passwords) != 1 {
		t.Fatal("删除的凭证仍可导出")
	}
	if err := s.Delete(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	s.db.QueryRow(`SELECT count(*) FROM relay_passwords WHERE target_id=?`, in.ID).Scan(&count)
	if count != 0 {
		t.Fatal("删除目标后仍残留中转密码")
	}
}

func TestRelayPasswordPlaintextMigration(t *testing.T) {
	for _, protected := range []bool{false, true} {
		name := "启动迁移"
		if protected {
			name = "解锁迁移"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			s, err := OpenStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if s != nil {
					s.Close()
				}
			})
			in := testInput(t)
			if _, err := s.Put(ctx, in); err != nil {
				t.Fatal(err)
			}
			before, _ := s.Get(ctx, in.ID)
			plain, _ := json.Marshal(map[string]string{"default": in.RelayPassword})
			if _, err := s.db.Exec(`UPDATE relay_passwords SET passwords=? WHERE target_id=?`, string(plain), in.ID); err != nil {
				t.Fatal(err)
			}
			if protected {
				if err := s.ChangeMasterPassword("", "migration-master-password"); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = OpenStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			if protected {
				if _, err := s.relayPasswords(ctx, in.ID); !errors.Is(err, ErrMasterLocked) {
					t.Fatal("锁定时不能读取中转密码")
				}
				if err := s.UnlockMasterPassword("migration-master-password"); err != nil {
					t.Fatal(err)
				}
			}
			passwords, err := s.relayPasswords(ctx, in.ID)
			if err != nil || passwords["default"] != in.RelayPassword {
				t.Fatal("迁移后未能解密原密码")
			}
			var stored string
			s.db.QueryRow(`SELECT passwords FROM relay_passwords WHERE target_id=?`, in.ID).Scan(&stored)
			if !strings.HasPrefix(stored, relayCipherPrefix) || strings.Contains(stored, in.RelayPassword) {
				t.Fatal("明文没有迁移为密文")
			}
			after, _ := s.Get(ctx, in.ID)
			if before.Revision != after.Revision || before.RelayUser != after.RelayUser {
				t.Fatal("迁移改变了目标版本或账号")
			}
			if err := s.migrateRelayPasswords(s.aead); err != nil {
				t.Fatal(err)
			}
			var repeated string
			s.db.QueryRow(`SELECT passwords FROM relay_passwords WHERE target_id=?`, in.ID).Scan(&repeated)
			if repeated != stored {
				t.Fatal("重复迁移不应重写密文")
			}
		})
	}
}

func TestRelayCredentialsAccessAndLegacy(t *testing.T) {
	f := newWebFixture(t)
	ctx := context.Background()
	path := "/api/targets/" + f.input.ID + "/relays/default/credentials"
	requireStatus(t, f, "POST", path, map[string]string{}, 401)
	f.login(t)
	read := func(path string) relayAccess {
		t.Helper()
		res := f.request(t, "POST", path, map[string]string{}, nil)
		if res.StatusCode != 200 || res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("凭证读取失败或被缓存", res.StatusCode)
		}
		var data relayAccess
		if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	if got := read(path); got.Credential.Password != f.input.RelayPassword || got.Login.User != f.input.User {
		t.Fatal("单账号凭证不匹配")
	}
	// 模拟升级前仅保存哈希的数据，读取不能修改或替换原密码。
	if _, err := f.store.db.Exec(`DELETE FROM relay_passwords WHERE target_id=?`, f.input.ID); err != nil {
		t.Fatal(err)
	}
	before, _ := f.store.Get(ctx, f.input.ID)
	if read(path).Credential.Password != "" {
		t.Fatal("旧密码被错误恢复")
	}
	after, _ := f.store.Get(ctx, f.input.ID)
	if before.Revision != after.Revision {
		t.Fatal("读取凭证不应修改目标")
	}
	in := multiLoginInput(t)
	in.ID = "copy-multiple"
	created, err := f.store.PutTarget(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.PutGlobalIP(ctx, GlobalIP{IP: "198.51.100.3", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	f.store.db.Exec(`INSERT INTO global_ips(ip,expires_at) VALUES(?,?)`, "198.51.100.4", time.Now().Add(-time.Hour).UnixNano())
	for i, credential := range created.Credentials {
		got := read("/api/targets/" + in.ID + "/relays/" + credential.ID + "/credentials")
		if got.Credential.Password != credential.Password || got.Login.User != in.LoginInputs[i].User {
			t.Fatal("跨账号复制了错误的凭证")
		}
		if len(got.GlobalIPs) != 1 || got.GlobalIPs[0].IP != "198.51.100.3" {
			t.Fatal("全局允许来源未正确过滤过期项")
		}
		if len(got.Target.AllowedSources) == 0 {
			t.Fatal("缺少目标允许来源")
		}
	}
	for _, endpoint := range []string{"/api/targets", "/api/targets/" + in.ID, "/api/events"} {
		res := f.request(t, "GET", endpoint, nil, nil)
		body, _ := io.ReadAll(res.Body)
		for _, credential := range created.Credentials {
			if strings.Contains(string(body), credential.Password) {
				t.Fatal("密码泄露到普通列表或日志接口")
			}
		}
	}
	requireStatus(t, f, "POST", "/api/targets/"+in.ID+"/relays/missing/credentials", map[string]string{}, 404)
	update := PutInput{Target: *created.Target}
	update.Enabled = false
	if _, err := f.store.PutTarget(ctx, update); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f, "POST", "/api/targets/"+in.ID+"/relays/"+created.Credentials[0].ID+"/credentials", map[string]string{}, 409)
	if _, err := f.store.Put(ctx, f.input); err != nil {
		t.Fatal(err)
	}
	accountID := addTestAccount(t, f.store, accountInput{Username: "relay-reader", Enabled: true, TargetIDs: []string{f.input.ID}})
	userLogin(t, f, "relay-reader")
	if read(path).Credential.Password != f.input.RelayPassword {
		t.Fatal("授权普通用户未能解密读取中转密码")
	}
	requireStatus(t, f, "POST", "/api/targets/"+in.ID+"/relays/"+created.Credentials[0].ID+"/credentials", map[string]string{}, 403)
	if _, err := f.store.saveAccount(ctx, accountID, accountInput{Username: "relay-reader", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f, "POST", path, map[string]string{}, 403)

}
