package gateway

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// 所有结构与旧笔记导入在同一事务完成，失败可以安全重试。
func (s *Store) migrateAccountsAndNotes() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS users (
 id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash BLOB NOT NULL,
 enabled INTEGER NOT NULL, epoch INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS user_targets(user_id TEXT NOT NULL,target_id TEXT NOT NULL,PRIMARY KEY(user_id,target_id));
 CREATE TABLE IF NOT EXISTS user_tags(user_id TEXT NOT NULL,tag TEXT NOT NULL,PRIMARY KEY(user_id,tag));
 CREATE TRIGGER IF NOT EXISTS delete_user_grants AFTER DELETE ON users BEGIN
 DELETE FROM user_targets WHERE user_id=OLD.id; DELETE FROM user_tags WHERE user_id=OLD.id; END;
 CREATE TRIGGER IF NOT EXISTS delete_target_grants AFTER DELETE ON targets BEGIN
 DELETE FROM user_targets WHERE target_id=OLD.id; END;`)
	if err != nil {
		return err
	}
	rows, err := tx.Query(`PRAGMA table_info(targets)`)
	if err != nil {
		return err
	}
	hasNotes := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var def any
		if err = rows.Scan(&cid, &name, &kind, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "notes" {
			hasNotes = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !hasNotes {
		if _, err = tx.Exec(`ALTER TABLE targets ADD COLUMN notes TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	var marker string
	err = tx.QueryRow(`SELECT value FROM settings WHERE name='notes_database_migrated'`).Scan(&marker)
	if err == nil {
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err = s.importLegacyNotes(tx); err != nil {
		return fmt.Errorf("迁移笔记失败，原文件已保留：%w", err)
	}
	if _, err = tx.Exec(`INSERT INTO settings(name,value) VALUES('notes_database_migrated','1')`); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) importLegacyNotes(tx *sql.Tx) error {
	dir := filepath.Join(s.dir, "notes")
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("旧笔记目录必须是普通目录")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	rows, err := tx.Query(`SELECT id FROM targets`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !identifier.MatchString(id) {
			return fmt.Errorf("机器 ID 无效")
		}
		name := id + ".md"
		info, err := root.Lstat(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("笔记 %s 必须是普通文件", id)
		}
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, editLimit+1))
		closeErr := f.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if len(data) > editLimit || !utf8.Valid(data) {
			return fmt.Errorf("笔记 %s 必须是 2 MiB 以内的 UTF-8 文本", id)
		}
		if _, err = tx.Exec(`UPDATE targets SET notes=? WHERE id=? AND notes=''`, string(data), id); err != nil {
			return err
		}
	}
	return nil
}
