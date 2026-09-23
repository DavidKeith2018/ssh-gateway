package gateway

import (
	"context"
	"encoding/json"
	"testing"
)

func TestServerCredentialsAccess(t *testing.T) {
	f := newWebFixture(t)
	path := "/api/targets/" + f.input.ID + "/logins/default/credentials"
	requireStatus(t, f, "POST", path, map[string]string{}, 401)
	f.login(t)
	read := func(path string) loginSecret {
		t.Helper()
		res := f.request(t, "POST", path, map[string]string{}, nil)
		defer res.Body.Close()
		if res.StatusCode != 200 || res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("读取失败或缓存凭证", res.StatusCode)
		}
		var result struct {
			Credential loginSecret `json:"credential"`
		}
		if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result.Credential
	}
	if read(path).Password != f.input.TargetPassword {
		t.Fatal("旧账号密码不匹配")
	}
	in := multiLoginInput(t)
	created, err := f.store.PutTarget(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	for i, login := range created.Target.Logins {
		secret := read("/api/targets/" + in.ID + "/logins/" + login.ID + "/credentials")
		if secret.Password != in.LoginInputs[i].TargetPassword || secret.PrivateKey != in.LoginInputs[i].TargetPrivateKey || secret.Passphrase != in.LoginInputs[i].TargetKeyPassphrase {
			t.Fatal("返回了错误账号的凭证")
		}
	}
	requireStatus(t, f, "POST", "/api/targets/"+in.ID+"/logins/missing/credentials", map[string]string{}, 404)
	addTestAccount(t, f.store, accountInput{Username: "server-reader", Enabled: true, TargetIDs: []string{f.input.ID}})
	userLogin(t, f, "server-reader")
	requireStatus(t, f, "POST", path, map[string]string{}, 403)
}
