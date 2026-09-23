package gateway

import (
	"context"
	"sync"
)

// trackSSHConnection 只统计持续的中转连接和交互终端，不统计文件请求与端口映射。
func (s *Store) trackSSHConnection(target string) func() {
	s.activeMu.Lock()
	if s.activeSSH == nil {
		s.activeSSH = make(map[string]int)
	}
	s.activeSSH[target]++
	s.activeMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.activeMu.Lock()
			defer s.activeMu.Unlock()
			s.activeSSH[target]--
			if s.activeSSH[target] <= 0 {
				delete(s.activeSSH, target)
			}
		})
	}
}

func (s *Store) activeConnections(ctx context.Context, user string) (int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT t.id FROM targets t WHERE "+targetAccessSQL, user, user)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	total := 0
	for _, id := range ids {
		total += s.activeSSH[id]
	}
	return total, nil
}
