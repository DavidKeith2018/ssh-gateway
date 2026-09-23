package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"ssh-gateway/localization"
	"strconv"
	"strings"
	"unicode"
)

type ImportRow struct {
	IssueMessages []localization.Message `json:"issues_i18n"`
	Blocked       bool                   `json:"blocked"`
	Input         PutInput               `json:"input"`
	IdentityFile  string                 `json:"identity_file,omitempty"`
	Issues        []string               `json:"issues"`
	Duplicate     bool                   `json:"duplicate"`
}
type importRequest struct {
	Format  string     `json:"format"`
	Content string     `json:"content"`
	Rows    []PutInput `json:"rows"`
}

// sshFields 只做词法拆分，绝不执行展开、命令或读取 IdentityFile/Include。
func sshFields(line string) ([]string, error) {
	var fields []string
	var word strings.Builder
	var quote rune
	escaped := false
	assignment := false
	flush := func() {
		if word.Len() > 0 {
			fields = append(fields, word.String())
			word.Reset()
		}
	}
	for _, c := range line {
		if escaped {
			word.WriteRune(c)
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				word.WriteRune(c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == '#' {
			break
		}
		if c == '=' && !assignment && (len(fields) == 0 || len(fields) == 1 && word.Len() == 0) {
			flush()
			assignment = true
			continue
		}
		if unicode.IsSpace(c) {
			flush()
			continue
		}
		word.WriteRune(c)
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("引号或转义不完整")
	}
	flush()
	return fields, nil
}
func parseImport(format, content string) ([]ImportRow, error) {
	rows := []ImportRow{}
	if format == "json" {
		var inputs []PutInput
		dec := json.NewDecoder(strings.NewReader(content))
		dec.DisallowUnknownFields()
		if strings.HasPrefix(strings.TrimSpace(content), "[") {
			if err := dec.Decode(&inputs); err != nil {
				return nil, fmt.Errorf("机器 JSON 无效：%w", err)
			}
		} else {
			var input PutInput
			if err := dec.Decode(&input); err != nil {
				return nil, fmt.Errorf("机器 JSON 无效：%w", err)
			}
			inputs = []PutInput{input}
		}
		if dec.Decode(new(any)) != io.EOF {
			return nil, fmt.Errorf("只允许一个 JSON 数组")
		}
		for _, input := range inputs {
			rows = append(rows, ImportRow{Input: input, Issues: []string{}})
		}
	} else if format == "ssh_config" {
		scanner := bufio.NewScanner(strings.NewReader(content))
		scanner.Buffer(make([]byte, 4096), 1<<20)
		current := -1
		globalIssue := false
		seen := map[string]bool{}
		for scanner.Scan() {
			fields, err := sshFields(scanner.Text())
			if err != nil {
				return nil, err
			}
			if len(fields) == 0 {
				continue
			}
			key := strings.ToLower(fields[0])
			if key == "include" || key == "match" {
				globalIssue = true
			}
			if key == "host" {
				row := ImportRow{Input: PutInput{Target: Target{Port: 22, Enabled: true, AuthType: "password", AllowedSources: []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}, SourceMode: "private", Tags: []string{}}}, Issues: []string{}}
				if len(fields) != 2 || strings.ContainsAny(strings.Join(fields[1:], " "), "*?!%") {
					row.Issues = append(row.Issues, "只支持单个明确的 Host，不能使用通配符或多个别名")
					globalIssue = true
				}
				if len(fields) > 1 {
					row.Input.Name = fields[1]
					row.Input.Host = fields[1]
				}
				rows = append(rows, row)
				current = len(rows) - 1
				seen = map[string]bool{}
				continue
			}
			if current < 0 {
				globalIssue = true
				continue
			}
			row := &rows[current]
			if len(fields) != 2 {
				row.Issues = append(row.Issues, "配置项参数数量无效："+key)
				continue
			}
			if seen[key] {
				if key == "identityfile" {
					row.Issues = append(row.Issues, "不支持多个 IdentityFile，请明确选择一个私钥")
				}
				continue
			}
			seen[key] = true
			value := fields[1]
			switch key {
			case "hostname":
				if strings.ContainsAny(value, "%$") {
					row.Issues = append(row.Issues, "不支持主机变量展开")
				}
				row.Input.Host = value
			case "port":
				row.Input.Port, _ = strconv.Atoi(value)
				if row.Input.Port < 1 || row.Input.Port > 65535 {
					row.Issues = append(row.Issues, "端口无效")
				}
			case "user":
				row.Input.User = value
			case "identityfile":
				row.IdentityFile = value
				row.Input.AuthType = "private_key"
			default:
				row.Issues = append(row.Issues, "不支持配置项："+key)
			}
			// Match/Include 可能影响后续 Host，因此整份文件均需先简化。
			if key == "match" || key == "include" {
				globalIssue = true
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		if globalIssue {
			for i := range rows {
				rows[i].Issues = append(rows[i].Issues, "存在全局配置、Include 或 Match；请先整理为独立的明确 Host 条目")
			}
		}
	} else {
		return nil, fmt.Errorf("导入格式必须为 ssh_config 或 json")
	}
	if len(rows) == 0 || len(rows) > 200 {
		return nil, fmt.Errorf("每次导入必须为 1～200 台机器")
	}
	return rows, nil
}
func importKey(t Target) string {
	return strings.ToLower(strings.TrimSpace(t.Host)) + ":" + strconv.Itoa(t.Port) + ":" + t.User
}
func (s *Store) importStore() (*Store, func(), error) {
	s.keyMu.RLock()
	defer s.keyMu.RUnlock()
	if s.aead == nil {
		return nil, nil, ErrMasterLocked
	}
	dir, err := os.MkdirTemp(s.dir, ".import-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	stage, err := openStore(dir, s.dataKey)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return stage, func() { stage.Close(); cleanup() }, nil
}
func normalizeImport(in PutInput) PutInput {
	in.ID = ""
	in.Revision = 0
	if in.Port == 0 {
		in.Port = 22
	}
	if in.AuthType == "" {
		in.AuthType = "password"
	}
	if len(in.LoginInputs) > 0 {
		chosen := in.DefaultLoginID
		if chosen == "" {
			chosen = in.LoginInputs[0].ID
		}
		for _, login := range in.LoginInputs {
			if login.ID == chosen {
				in.User = login.User
			}
		}
	}
	return in
}
func (s *Store) PreviewImport(ctx context.Context, format, content string) ([]ImportRow, error) {
	rows, err := parseImport(format, content)
	if err != nil {
		return nil, err
	}
	existing, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for _, target := range existing {
		keys[importKey(target)] = true
	}
	stage, close, err := s.importStore()
	if err != nil {
		return nil, err
	}
	defer close()
	for i := range rows {
		row := &rows[i]
		row.Blocked = len(row.Issues) > 0
		row.Input = normalizeImport(row.Input)
		key := importKey(row.Input.Target)
		row.Duplicate = keys[key]
		keys[key] = true
		if row.Duplicate {
			row.Issues = append(row.Issues, "已有相同主机、端口和账号，默认跳过")
			continue
		}
		if len(row.Issues) > 0 {
			continue
		}
		if _, err = stage.PutTarget(ctx, row.Input); err != nil {
			row.Issues = append(row.Issues, err.Error())
		}
	}
	for i := range rows {
		for _, issue := range rows[i].Issues {
			rows[i].IssueMessages = append(rows[i].IssueMessages, localization.Describe(issue))
		}
	}
	return rows, nil
}
func (s *Store) ImportTargets(ctx context.Context, inputs []PutInput) ([]string, error) {
	if len(inputs) == 0 || len(inputs) > 200 {
		return nil, fmt.Errorf("每次导入必须为 1～200 台机器")
	}
	stage, close, err := s.importStore()
	if err != nil {
		return nil, err
	}
	defer close()
	ids := []string{}
	for _, input := range inputs {
		result, e := stage.PutTarget(ctx, normalizeImport(input))
		if e != nil {
			return nil, e
		}
		ids = append(ids, result.ID)
	}
	records := []record{}
	passwords := map[string]string{}
	for _, id := range ids {
		var passwordsValue string
		r, e := stage.get(ctx, "id", id)
		if e != nil {
			return nil, e
		}
		records = append(records, r)
		if e = stage.db.QueryRowContext(ctx, "SELECT passwords FROM relay_passwords WHERE target_id=?", id).Scan(&passwordsValue); e != nil {
			return nil, e
		}
		passwords[id] = passwordsValue
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// 先取得写锁，再检查重复，防止其他管理进程在检查后插入。
	if _, err = tx.ExecContext(ctx, "UPDATE settings SET value=value WHERE name='admin_password'"); err != nil {
		return nil, err
	}
	existing, err := tx.QueryContext(ctx, "SELECT config FROM targets")
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for existing.Next() {
		var raw string
		var target Target
		if err = existing.Scan(&raw); err != nil {
			existing.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &target); err != nil {
			existing.Close()
			return nil, err
		}
		keys[importKey(target)] = true
	}
	err = existing.Err()
	existing.Close()
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		key := importKey(r.Target)
		if keys[key] {
			return nil, fail(409, "导入包含重复机器，请重新预览")
		}
		keys[key] = true
		for _, username := range targetRelayNames(r.Target) {
			var exists bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM targets WHERE relay_user=? OR EXISTS(SELECT 1 FROM json_each(config,'$.relays') WHERE json_extract(value,'$.username')=?))`, username, username).Scan(&exists); err != nil {
				return nil, err
			}
			if exists {
				return nil, fail(409, "中转用户名已被其他机器使用")
			}
		}
		raw, _ := json.Marshal(r.Target)
		if _, err = tx.ExecContext(ctx, "INSERT INTO targets(id,config,relay_user,password,password_hash,revision) VALUES(?,?,?,?,?,1)", r.ID, string(raw), r.RelayUser, r.password, r.hash); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO relay_passwords(target_id,passwords) VALUES(?,?)", r.ID, passwords[r.ID]); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}
func (web *Web) importPreview(w http.ResponseWriter, r *http.Request) {
	var in importRequest
	if err := decodeJSON(w, r, &in); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	rows, err := web.store.PreviewImport(r.Context(), in.Format, in.Content)
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, rows)
}
func (web *Web) importCommit(w http.ResponseWriter, r *http.Request) {
	var in importRequest
	if err := decodeJSON(w, r, &in); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	ids, err := web.store.ImportTargets(r.Context(), in.Rows)
	if err != nil {
		var conflict *machineError
		if errors.As(err, &conflict) {
			machineFailure(w, err)
			return
		}
		apiError(w, 400, err.Error())
		return
	}
	web.store.logEvent("", sourceIP(r), fmt.Sprintf("批量导入机器：%d 台", len(ids)))
	jsonResponse(w, 200, map[string]any{"ids": ids})
}
