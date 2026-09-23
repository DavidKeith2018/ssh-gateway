package gateway

// seedDefaultShortcuts installs editable global commands once per data directory.
func (s *Store) seedDefaultShortcuts() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("INSERT INTO settings(name,value) VALUES('default_shortcuts_v1','1') ON CONFLICT(name) DO NOTHING")
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	for _, command := range []string{"pwd", "ls -lah", "df -h", "free -h", "uptime"} {
		_, err = tx.Exec("INSERT INTO shortcuts(id,name,command,target_id) SELECT lower(hex(randomblob(16))),?,?, '*' WHERE NOT EXISTS (SELECT 1 FROM shortcuts WHERE command=? AND target_id='*')", command, command, command)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
