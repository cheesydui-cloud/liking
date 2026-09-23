package db

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const (
	BackupFormat        = "liking-backup"
	BackupFormatVersion = 1
	maxBackupJSON       = 64 << 20
)

var identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

var backupTables = []string{
	"users",
	"servers",
	"certificates",
	"packages",
	"inbounds",
	"clients",
	"package_inbounds",
	"package_servers",
	"user_packages",
	"traffic_daily",
	"settings",
	"audit",
}

var wipeOrder = []string{
	"traffic_hourly",
	"traffic_daily",
	"clients",
	"package_inbounds",
	"package_servers",
	"user_packages",
	"sessions",
	"audit",
	"inbounds",
	"packages",
	"certificates",
	"servers",
	"users",
	"settings",
}

var insertOrder = []string{
	"users",
	"servers",
	"certificates",
	"packages",
	"inbounds",
	"clients",
	"package_inbounds",
	"package_servers",
	"user_packages",
	"traffic_daily",
	"settings",
	"audit",
}

var autoincTables = []string{
	"users", "servers", "certificates", "packages", "inbounds", "clients", "audit",
}

// Snapshot is a versioned panel backup. Sessions are never included.
type Snapshot struct {
	Format        string                      `json:"format"`
	FormatVersion int                         `json:"format_version"`
	AppVersion    string                      `json:"app_version"`
	CreatedAt     int64                       `json:"created_at"`
	Tables        map[string][]map[string]any `json:"tables"`
}

// BackupSummary is a secret-free view of a snapshot or the live database.
type BackupSummary struct {
	Format        string `json:"format,omitempty"`
	FormatVersion int    `json:"format_version,omitempty"`
	AppVersion    string `json:"app_version,omitempty"`
	CreatedAt     int64  `json:"created_at,omitempty"`
	Users         int    `json:"users"`
	Servers       int    `json:"servers"`
	Inbounds      int    `json:"inbounds"`
	Packages      int    `json:"packages"`
	Certs         int    `json:"certs"`
	Clients       int    `json:"clients"`
	HasCFToken    bool   `json:"has_cf_token"`
}

func quoteIdent(name string) (string, error) {
	if !identRe.MatchString(name) {
		return "", fmt.Errorf("无效标识")
	}
	return `"` + name + `"`, nil
}

func dumpTable(tx *sql.Tx, table string) ([]map[string]any, error) {
	q, err := quoteIdent(table)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT * FROM ` + q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, c := range cols {
			v := raw[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			row[c] = v
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// Export captures a consistent snapshot of panel data.
func Export(d *sql.DB, appVersion string) (*Snapshot, error) {
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	snap := &Snapshot{
		Format:        BackupFormat,
		FormatVersion: BackupFormatVersion,
		AppVersion:    appVersion,
		CreatedAt:     time.Now().Unix(),
		Tables:        map[string][]map[string]any{},
	}
	for _, t := range backupTables {
		rows, err := dumpTable(tx, t)
		if err != nil {
			return nil, fmt.Errorf("导出 %s：%w", t, err)
		}
		snap.Tables[t] = rows
	}
	return snap, nil
}

func (s *Snapshot) EncodeGzip() ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if err := json.NewEncoder(gz).Encode(s); err != nil {
		gz.Close()
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeSnapshot reads gzip or plain JSON.
func DecodeSnapshot(r io.Reader) (*Snapshot, error) {
	br := bufio.NewReader(r)
	magic, _ := br.Peek(2)
	var src io.Reader = br
	if len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("备份文件无法解压")
		}
		defer gz.Close()
		src = gz
	}
	dec := json.NewDecoder(io.LimitReader(src, maxBackupJSON))
	dec.UseNumber()
	var snap Snapshot
	if err := dec.Decode(&snap); err != nil {
		return nil, fmt.Errorf("不是 liking 备份文件")
	}
	if snap.Format != BackupFormat {
		return nil, fmt.Errorf("不是 liking 备份文件")
	}
	if snap.FormatVersion < 1 {
		return nil, fmt.Errorf("备份格式无法识别")
	}
	if snap.FormatVersion > BackupFormatVersion {
		return nil, fmt.Errorf("备份格式太新，请先升级面板")
	}
	if snap.Tables == nil {
		snap.Tables = map[string][]map[string]any{}
	}
	return &snap, nil
}

func (s *Snapshot) Summary() BackupSummary {
	sum := BackupSummary{
		Format:        s.Format,
		FormatVersion: s.FormatVersion,
		AppVersion:    s.AppVersion,
		CreatedAt:     s.CreatedAt,
		Users:         len(s.Tables["users"]),
		Servers:       len(s.Tables["servers"]),
		Inbounds:      len(s.Tables["inbounds"]),
		Packages:      len(s.Tables["packages"]),
		Certs:         len(s.Tables["certificates"]),
		Clients:       len(s.Tables["clients"]),
	}
	for _, row := range s.Tables["settings"] {
		if asString(row["key"]) == "cf_api_token" && strings.TrimSpace(asString(row["value"])) != "" {
			sum.HasCFToken = true
			break
		}
	}
	return sum
}

func LiveSummary(d *sql.DB) (BackupSummary, error) {
	count := func(table string) (int, error) {
		q, err := quoteIdent(table)
		if err != nil {
			return 0, err
		}
		var n int
		err = d.QueryRow(`SELECT COUNT(*) FROM ` + q).Scan(&n)
		return n, err
	}
	var s BackupSummary
	var err error
	if s.Users, err = count("users"); err != nil {
		return s, err
	}
	if s.Servers, err = count("servers"); err != nil {
		return s, err
	}
	if s.Inbounds, err = count("inbounds"); err != nil {
		return s, err
	}
	if s.Packages, err = count("packages"); err != nil {
		return s, err
	}
	if s.Certs, err = count("certificates"); err != nil {
		return s, err
	}
	if s.Clients, err = count("clients"); err != nil {
		return s, err
	}
	tok, _ := GetSetting(d, "cf_api_token")
	s.HasCFToken = strings.TrimSpace(tok) != ""
	return s, nil
}

func ValidateSnapshot(s *Snapshot) error {
	users := s.Tables["users"]
	if len(users) == 0 {
		return fmt.Errorf("备份里没有用户")
	}
	admin := false
	for _, u := range users {
		if asString(u["role"]) == "admin" {
			admin = true
			break
		}
	}
	if !admin {
		return fmt.Errorf("备份里没有管理员账号")
	}
	return nil
}

type colMeta struct {
	Name string
	Decl string
}

func tableCols(tx *sql.Tx, table string) ([]colMeta, error) {
	q, err := quoteIdent(table)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(`PRAGMA table_info(` + q + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []colMeta
	for rows.Next() {
		var cid int
		var name, decl string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &decl, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out = append(out, colMeta{Name: name, Decl: decl})
	}
	return out, rows.Err()
}

func toInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case nil:
		return 0, false
	case int64:
		return t, true
	case int:
		return int64(t), true
	case float64:
		return int64(t), true
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			f, ferr := t.Float64()
			if ferr != nil {
				return 0, false
			}
			return int64(f), true
		}
		return n, true
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	case string:
		if t == "" {
			return 0, false
		}
		var n int64
		_, err := fmt.Sscan(t, &n)
		return n, err == nil
	default:
		return 0, false
	}
}

func toFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case nil:
		return 0, false
	case float64:
		return t, true
	case int64:
		return float64(t), true
	case int:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

func sqlValue(decl string, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	dt := strings.ToUpper(decl)
	switch {
	case strings.Contains(dt, "INT"):
		n, ok := toInt64(v)
		if !ok {
			return nil, fmt.Errorf("无法解析整数")
		}
		return n, nil
	case strings.Contains(dt, "REAL") || strings.Contains(dt, "FLOA") || strings.Contains(dt, "DOUB"):
		f, ok := toFloat64(v)
		if !ok {
			return nil, fmt.Errorf("无法解析数字")
		}
		return f, nil
	default:
		return asString(v), nil
	}
}

func insertTable(tx *sql.Tx, table string, rows []map[string]any, skip map[string]bool) error {
	if len(rows) == 0 {
		return nil
	}
	cols, err := tableCols(tx, table)
	if err != nil {
		return err
	}
	qtable, err := quoteIdent(table)
	if err != nil {
		return err
	}
	for _, row := range rows {
		var names []string
		var ph []string
		var args []any
		for _, c := range cols {
			if skip[c.Name] {
				continue
			}
			v, ok := row[c.Name]
			if !ok {
				continue
			}
			sv, err := sqlValue(c.Decl, v)
			if err != nil {
				return fmt.Errorf("%s.%s: %w", table, c.Name, err)
			}
			qn, err := quoteIdent(c.Name)
			if err != nil {
				return err
			}
			names = append(names, qn)
			ph = append(ph, "?")
			args = append(args, sv)
		}
		if len(names) == 0 {
			continue
		}
		q := `INSERT INTO ` + qtable + ` (` + strings.Join(names, ",") + `) VALUES (` + strings.Join(ph, ",") + `)`
		if _, err := tx.Exec(q, args...); err != nil {
			return fmt.Errorf("%s: %w", table, err)
		}
	}
	return nil
}

func resetSeq(tx *sql.Tx, table string) error {
	q, err := quoteIdent(table)
	if err != nil {
		return err
	}
	var max int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM ` + q).Scan(&max); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sqlite_sequence WHERE name=?`, table); err != nil {
		if max <= 0 {
			return nil
		}
		return err
	}
	if max <= 0 {
		return nil
	}
	_, err = tx.Exec(`INSERT INTO sqlite_sequence(name, seq) VALUES(?, ?)`, table, max)
	return err
}

func patchInboundExits(tx *sql.Tx, rows []map[string]any) error {
	for _, row := range rows {
		id, ok := toInt64(row["id"])
		if !ok || id == 0 {
			continue
		}
		exit, ok := toInt64(row["exit_inbound_id"])
		if !ok || exit == 0 {
			continue
		}
		if _, err := tx.Exec(`UPDATE inbounds SET exit_inbound_id=? WHERE id=?`, exit, id); err != nil {
			return err
		}
	}
	return nil
}

// Import replaces live panel data with the snapshot. Caller must drop agent
// connections first. Sessions are always wiped.
func Import(d *sql.DB, snap *Snapshot) error {
	if err := ValidateSnapshot(snap); err != nil {
		return err
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE inbounds SET exit_inbound_id=NULL`); err != nil {
		return err
	}
	for _, t := range wipeOrder {
		q, err := quoteIdent(t)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM ` + q); err != nil {
			return fmt.Errorf("清空 %s：%w", t, err)
		}
	}

	for _, t := range insertOrder {
		rows := snap.Tables[t]
		skip := map[string]bool{}
		if t == "inbounds" {
			skip["exit_inbound_id"] = true
		}
		if err := insertTable(tx, t, rows, skip); err != nil {
			return fmt.Errorf("写入 %s：%w", t, err)
		}
	}
	if err := patchInboundExits(tx, snap.Tables["inbounds"]); err != nil {
		return fmt.Errorf("链式入口：%w", err)
	}
	if _, err := tx.Exec(`UPDATE users SET password_plain=''`); err != nil {
		return err
	}
	for _, t := range autoincTables {
		if err := resetSeq(tx, t); err != nil {
			return fmt.Errorf("序号 %s：%w", t, err)
		}
	}
	return tx.Commit()
}
