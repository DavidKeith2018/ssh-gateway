package gateway

import "testing"

func TestDefaultShortcutsPreserveEditsAndDeletions(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE shortcuts SET name='My directory',command='pwd -P' WHERE command='pwd'; DELETE FROM shortcuts WHERE command='free -h'"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var count int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM shortcuts").Scan(&count); err != nil || count != 4 {
		t.Fatalf("commands were reseeded: %d %v", count, err)
	}
	if err = s.db.QueryRow("SELECT COUNT(*) FROM shortcuts WHERE name='My directory' AND command='pwd -P'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("edit was lost: %d %v", count, err)
	}
}

func TestDefaultShortcutsUpgradePreservesExistingCommands(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce a pre-defaults database with an existing global command.
	if _, err = s.db.Exec("DELETE FROM settings WHERE name='default_shortcuts_v1'; DELETE FROM shortcuts; INSERT INTO shortcuts(id,name,command,target_id) VALUES('custom','My directory','pwd','*')"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var count int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM shortcuts WHERE target_id='*'").Scan(&count); err != nil || count != 5 {
		t.Fatalf("upgrade defaults: %d %v", count, err)
	}
	var name string
	if err = s.db.QueryRow("SELECT name FROM shortcuts WHERE id='custom'").Scan(&name); err != nil || name != "My directory" {
		t.Fatalf("custom command lost: %s %v", name, err)
	}
}
