package gateway

import (
	"bytes"
	"context"
	"golang.org/x/crypto/bcrypt"
	"os"
	"path/filepath"
	"testing"
)

func TestUnifiedAdministratorEncryptionPassword(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	old, next := "original-admin-password", "replacement-admin-password"
	if err = s.SetAdminPassword(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err = s.SetCredentialProtection(ctx, old, true); err != nil {
		t.Fatal(err)
	}
	key := bytes.Clone(s.dataKey)
	before, _ := s.adminHash(ctx)
	// A competing key replacement must leave both credentials unchanged.
	disk, _ := os.ReadFile(filepath.Join(dir, "master.key"))
	if err = os.WriteFile(filepath.Join(dir, "master.key"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.SetAdminPassword(ctx, next); err == nil {
		t.Fatal("stale key update succeeded")
	}
	after, _ := s.adminHash(ctx)
	if !bytes.Equal(before, after) {
		t.Fatal("password changed after failed key save")
	}
	if err = os.WriteFile(filepath.Join(dir, "master.key"), disk, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.SetAdminPassword(ctx, next); err != nil {
		t.Fatal(err)
	}
	after, _ = s.adminHash(ctx)
	if bcrypt.CompareHashAndPassword(after, []byte(next)) != nil || bytes.Equal(before, after) {
		t.Fatal("administrator password not updated")
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.UnlockMasterPassword(old); err == nil {
		t.Fatal("old password unlocked credentials")
	}
	if err = s.UnlockMasterPassword(next); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key, s.dataKey) {
		t.Fatal("data key changed")
	}
	if _, err = LoadHostKey(dir); err != nil {
		t.Fatal(err)
	}
	archive, err := s.ExportBackup(ctx, "unified-backup-password")
	if err != nil {
		t.Fatal(err)
	}
	restoreDir := t.TempDir()
	initial, err := OpenStore(restoreDir)
	if err != nil {
		t.Fatal(err)
	}
	initial.Close()
	if _, err = RestoreBackup(ctx, restoreDir, archive, "unified-backup-password"); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenStore(restoreDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.UnlockMasterPassword(next); err != nil {
		t.Fatal(err)
	}
	restoredHash, err := restored.adminHash(ctx)
	if err != nil || bcrypt.CompareHashAndPassword(restoredHash, []byte(next)) != nil || !bytes.Equal(key, restored.dataKey) {
		t.Fatal("backup lost unified credentials")
	}
	if err = s.SetCredentialProtection(ctx, next, false); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	after, _ = s.adminHash(ctx)
	if bcrypt.CompareHashAndPassword(after, []byte(next)) != nil || s.MasterPasswordStatus().Enabled {
		t.Fatal("disable lost administrator password")
	}
}

func TestDesktopUnifiedPasswordChangeAndRestart(t *testing.T) {
	dir := t.TempDir()
	d, err := OpenDesktop(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { d.Close() }()
	reply := desktopCall(t, d, "POST", "/desktop/setup", map[string]any{"password": testAdminPassword, "encryption": true})
	if reply.Status != 200 {
		t.Fatal(reply)
	}
	loginDesktop(t, d)
	next := "desktop-replacement-password"
	reply = desktopCall(t, d, "POST", "/desktop/password", map[string]string{"current": testAdminPassword, "password": next})
	if reply.Status != 200 {
		t.Fatal(reply)
	}
	if reply = desktopCall(t, d, "GET", "/me", nil); reply.Status != 401 {
		t.Fatal("old login remains valid", reply)
	}
	d.Close()
	d, err = OpenDesktop(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if reply = desktopCall(t, d, "POST", "/unlock", map[string]string{"password": testAdminPassword}); reply.Status != 401 {
		t.Fatal("old password unlocks", reply)
	}
	if reply = desktopCall(t, d, "POST", "/unlock", map[string]string{"password": next}); reply.Status != 200 {
		t.Fatal(reply)
	}
	if reply = desktopCall(t, d, "POST", "/login", map[string]string{"username": adminUsername, "password": next}); reply.Status != 200 {
		t.Fatal(reply)
	}
}

func TestUnifiedEnvelopeAuthenticatesAdminHash(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte(testAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := wrapUnifiedMasterKey(make([]byte, 32), testAdminPassword, hash)
	if err != nil {
		t.Fatal(err)
	}
	envelope[30] ^= 1
	if _, err = unwrapMasterKey(envelope, testAdminPassword); err == nil {
		t.Fatal("tampered hash accepted")
	}
}
