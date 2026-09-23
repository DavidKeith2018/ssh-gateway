package gateway

import (
	"context"
	"encoding/json"
	"testing"
)

func TestFileFavoritesPersistenceIsolationAndRevocation(t *testing.T) {
	f := newWebFixture(t)
	ctx := context.Background()
	request := func(target, op string, entries []fileFavorite, status int) []fileFavorite {
		t.Helper()
		r := f.request(t, "POST", "/api/targets/"+target+"/favorites", favoriteRequest{op, entries}, nil)
		defer r.Body.Close()
		if r.StatusCode != status {
			t.Fatalf("status %d, want %d", r.StatusCode, status)
		}
		if status != 200 {
			return nil
		}
		var result []fileFavorite
		if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	entry := []fileFavorite{{Path: "/demo/file.txt", Kind: "file"}, {Path: "/demo/folder", Kind: "directory"}}
	request("test", "read", nil, 401)
	f.login(t)
	admin := f.cookie
	if got := request("test", "add", entry, 200); len(got) != 2 {
		t.Fatal(got)
	}
	if got := request("test", "import", entry, 200); len(got) != 2 {
		t.Fatal("duplicate import", got)
	}
	request("test", "add", []fileFavorite{{Path: "relative", Kind: "file"}}, 400)
	request("test", "replace", nil, 400)
	otherTarget := f.input
	otherTarget.ID = "other"
	otherTarget.RelayUser = "other-relay"
	if _, err := f.store.Put(ctx, otherTarget); err != nil {
		t.Fatal(err)
	}
	if got := request("other", "read", nil, 200); len(got) != 0 {
		t.Fatal("target isolation", got)
	}
	in := accountInput{Username: "alice", Enabled: true, TargetIDs: []string{"test"}}
	id := addTestAccount(t, f.store, in)
	userLogin(t, f, "alice")
	if got := request("test", "read", nil, 200); len(got) != 0 {
		t.Fatal("user isolation", got)
	}
	request("other", "read", nil, 403)
	request("test", "add", entry[:1], 200)
	in.TargetIDs = nil
	if _, err := f.store.saveAccount(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	request("test", "read", nil, 403)
	request("test", "remove", entry[:1], 403)
	f.cookie = admin
	if got := request("test", "remove", entry[:1], 200); len(got) != 1 || got[0].Path != entry[1].Path {
		t.Fatal(got)
	}
	other, err := OpenStore(f.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := other.fileFavorites(ctx, "", "test", favoriteRequest{Op: "read"})
	other.Close()
	if err != nil || len(got) != 1 {
		t.Fatal("reopen persistence", got, err)
	}
	if _, err := f.store.db.Exec(`DELETE FROM users WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	var count int
	f.store.db.QueryRow(`SELECT count(*) FROM file_favorites WHERE user_id=?`, id).Scan(&count)
	if count != 0 {
		t.Fatal("deleted user's favorites retained")
	}
	if err := f.store.Delete(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Put(ctx, f.input); err != nil {
		t.Fatal(err)
	}
	if got := request("test", "read", nil, 200); len(got) != 0 {
		t.Fatal("deleted target's favorites retained", got)
	}
}

func TestDesktopFavoritesUsePersistentAPI(t *testing.T) {
	d := freshDesktop(t)
	loginDesktop(t, d)
	if reply := desktopCall(t, d, "POST", "/targets", testInput(t)); reply.Status != 200 {
		t.Fatal("create target", reply.Status)
	}
	for _, body := range []string{`{"op":"add","entries":[{"path":"/demo/folder","kind":"directory"}]}`, `{"op":"read"}`} {
		reply, err := d.MachineCall(context.Background(), "test", "favorites", body)
		if err != nil || reply.Status != 200 {
			t.Fatal("desktop favorites", err, reply.Status)
		}
		var entries []fileFavorite
		if err := json.Unmarshal(reply.Data, &entries); err != nil || len(entries) != 1 || entries[0].Path != "/demo/folder" {
			t.Fatal("desktop favorites data", err)
		}
	}
}
