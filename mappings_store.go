package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/google/uuid"
)

// Mapping 只包含转发配置；目标认证材料仍由目标保存。
type Mapping struct {
	ID          string `json:"id"`
	TargetID    string `json:"target_id"`
	Name        string `json:"name"`
	Direction   string `json:"direction"`
	ServiceHost string `json:"service_host"`
	ServicePort int    `json:"service_port"`
	ListenPort  int    `json:"listen_port"`
	Scope       string `json:"scope"`
	AutoStart   bool   `json:"auto_start"`
	Revision    int64  `json:"revision"`
}
type mappingRecord struct {
	Mapping
	source string
}

func (m Mapping) bindHost() string {
	if m.Scope == "shared" {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}
func (m Mapping) bindAddress() string {
	return net.JoinHostPort(m.bindHost(), fmt.Sprint(m.ListenPort))
}
func (s *Store) mapping(ctx context.Context, id string) (mappingRecord, error) {
	var m mappingRecord
	var raw string
	var rev int64
	err := s.db.QueryRowContext(ctx, `SELECT m.config,m.source_ip,m.revision FROM mappings m JOIN targets t ON t.id=m.target_id WHERE m.id=?`, id).Scan(&raw, &m.source, &rev)
	if err != nil {
		return m, err
	}
	err = json.Unmarshal([]byte(raw), &m.Mapping)
	m.Revision = rev
	return m, err
}
func (s *Store) mappingRecords(ctx context.Context) ([]mappingRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.config,m.source_ip,m.revision FROM mappings m JOIN targets t ON t.id=m.target_id ORDER BY m.listen_port,m.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []mappingRecord{}
	for rows.Next() {
		var m mappingRecord
		var raw string
		var rev int64
		if err = rows.Scan(&raw, &m.source, &rev); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &m.Mapping); err != nil {
			return nil, err
		}
		m.Revision = rev
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) putMapping(ctx context.Context, in Mapping, source string, create bool) (Mapping, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.ServiceHost = strings.TrimSpace(in.ServiceHost)
	if !identifier.MatchString(in.TargetID) || in.Name == "" || len(in.Name) > 180 || len(in.ServiceHost) > 253 || in.ServiceHost == "" || strings.ContainsAny(in.ServiceHost, " \t\r\n/[]\x00") {
		return in, fail(400, "请填写名称、所属服务器和有效的服务主机地址")
	}
	if in.Direction != "local" && in.Direction != "reverse" {
		return in, fail(400, "转发方向无效")
	}
	if in.Scope != "loopback" && in.Scope != "shared" {
		return in, fail(400, "访问范围无效")
	}
	if in.ServicePort < 1 || in.ServicePort > 65535 || in.ListenPort < 1 || in.ListenPort > 65535 {
		return in, fail(400, "端口必须在 1～65535 之间")
	}
	target, err := s.Get(ctx, in.TargetID)
	if err != nil {
		return in, fail(404, "所属服务器不存在")
	}
	key := "local"
	if in.Direction == "reverse" {
		key = "remote:" + strings.ToLower(target.Host)
		if ip := net.ParseIP(target.Host); ip != nil {
			key = "remote:" + ip.String()
		}
	}
	if create {
		in.ID = uuid.NewString()
		in.Revision = 1
	} else {
		old, e := s.mapping(ctx, in.ID)
		if errors.Is(e, sql.ErrNoRows) {
			return in, fail(404, "映射不存在")
		}
		if e != nil {
			return in, e
		}
		if old.TargetID != in.TargetID {
			return in, fail(400, "编辑时不能更换所属服务器")
		}
		if old.Revision != in.Revision {
			return in, fail(409, "映射已被修改，请刷新后重试")
		}
		in.Revision++
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return in, err
	}
	if create {
		// 同一条 SQL 限制条数，避免并发创建绕过上限。
		var result sql.Result
		result, err = s.db.ExecContext(ctx, `INSERT INTO mappings(id,target_id,config,source_ip,revision,listener_key,listen_port) SELECT ?,?,?,?,?,?,? WHERE (SELECT COUNT(*) FROM mappings)<128`, in.ID, in.TargetID, string(raw), source, in.Revision, key, in.ListenPort)
		if err == nil {
			if n, _ := result.RowsAffected(); n == 0 {
				return in, fail(409, "映射最多保存 128 条")
			}
		}
	} else {
		var result sql.Result
		result, err = s.db.ExecContext(ctx, `UPDATE mappings SET config=?,source_ip=?,revision=?,listener_key=?,listen_port=? WHERE id=? AND revision=?`, string(raw), source, in.Revision, key, in.ListenPort, in.ID, in.Revision-1)
		if err == nil {
			if n, _ := result.RowsAffected(); n == 0 {
				return in, fail(409, "映射已被修改，请刷新后重试")
			}
		}
	}
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return in, fail(409, "同一监听机上的映射端口已被其他规则使用")
	}
	return in, err
}
