package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func now() int64 { return time.Now().Unix() }

func RandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func GetSetting(d *sql.DB, key string) (string, error) {
	var v string
	err := d.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func SetSetting(d *sql.DB, key, value string) error {
	_, err := d.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func SettingInt(d *sql.DB, key string, fallback int) int {
	v, _ := GetSetting(d, key)
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func FileDir(d *sql.DB) string {
	rows, err := d.Query(`PRAGMA database_list`)
	if err != nil {
		return "data"
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			continue
		}
		if name != "main" {
			continue
		}
		file = strings.TrimSpace(file)
		if file == "" || strings.Contains(file, "mode=memory") {
			return "data"
		}
		return filepath.Dir(file)
	}
	return "data"
}

func CountUsers(d *sql.DB) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func CreateUser(d *sql.DB, username, passwordHash, role, remark string) (*User, error) {
	tok, err := RandomHex(16)
	if err != nil {
		return nil, err
	}
	res, err := d.Exec(`INSERT INTO users(username,password_hash,role,remark,sub_token,created_at) VALUES(?,?,?,?,?,?)`,
		username, passwordHash, role, remark, tok, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetUser(d, id)
}

// NormalizeResetMode accepts anniversary, none, or empty. Anything else is rejected.
// Empty and none both store as "" so existing packages stay non-resetting.
func NormalizeResetMode(s string) (string, error) {
	switch strings.TrimSpace(s) {
	case "", "none":
		return "", nil
	case ResetModeAnniversary:
		return ResetModeAnniversary, nil
	default:
		return "", fmt.Errorf("清零方式无效")
	}
}

// CreateUserConfigured creates the user and optional package binding atomically.
// resetDay 1–31 is written onto the user. 0 leaves the column at its default.
// Callers set the day only for a new anniversary package; this does not rewrite existing users.
func CreateUserConfigured(d *sql.DB, username, passwordHash, passwordPlain, role, remark string, expiresAt int64, pkg *Package, resetDay int) (*User, error) {
	tok, err := RandomHex(16)
	if err != nil {
		return nil, err
	}
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	createdAt := now()
	cycleStart := int64(0)
	speedLimit := int64(0)
	subPreset := ""
	subCats := []string{}
	siteMode := ""
	denyCats := []string{}
	denyDomains := []string{}
	if pkg != nil {
		var exists int
		if err := tx.QueryRow(`SELECT 1 FROM packages WHERE id=?`, pkg.ID).Scan(&exists); err != nil {
			return nil, err
		}
		cycleStart = createdAt
		speedLimit = pkg.SpeedLimit
		subPreset = pkg.SubRulePreset
		subCats = pkg.SubRuleCategories
		siteMode = pkg.SiteFilterMode
		denyCats = pkg.SiteDenyCategories
		denyDomains = pkg.SiteDenyDomains
	}
	day := 0
	if resetDay >= 1 && resetDay <= 31 {
		day = resetDay
	}
	res, err := tx.Exec(`INSERT INTO users(
		username,password_hash,password_plain,role,remark,sub_token,created_at,expires_at,cycle_start,
		speed_limit,sub_rule_preset,sub_rule_categories,site_filter_mode,site_deny_categories,site_deny_domains,
		traffic_reset_day
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		username, passwordHash, passwordPlain, role, remark, tok, createdAt, expiresAt, cycleStart,
		speedLimit, subPreset, encodeStringSlice(subCats), siteMode, encodeStringSlice(denyCats), encodeStringSlice(denyDomains), day)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if pkg != nil {
		if _, err := tx.Exec(`INSERT INTO user_packages(user_id,package_id,bound_at,expires_at) VALUES(?,?,?,?)`, id, pkg.ID, createdAt, expiresAt); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetUser(d, id)
}

func GetUser(d *sql.DB, id int64) (*User, error) {
	u := &User{}
	var en int
	var tlim sql.NullInt64
	var totpEn int
	var catsRaw, denyCatsRaw, denyDomsRaw string
	err := d.QueryRow(`SELECT id,username,password_hash,role,remark,enabled,expires_at,traffic_limit,used_up,used_down,cycle_start,sub_token,created_at,totp_secret,totp_enabled,traffic_reset_day,password_plain,speed_limit,sub_rule_preset,sub_rule_categories,site_deny_categories,site_deny_domains,site_filter_mode FROM users WHERE id=?`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Remark, &en, &u.ExpiresAt, &tlim, &u.UsedUp, &u.UsedDown, &u.CycleStart, &u.SubToken, &u.CreatedAt, &u.TOTPSecret, &totpEn, &u.TrafficResetDay, &u.PasswordPlain, &u.SpeedLimit, &u.SubRulePreset, &catsRaw, &denyCatsRaw, &denyDomsRaw, &u.SiteFilterMode)
	if err != nil {
		return nil, err
	}
	u.Enabled = en == 1
	u.TOTPEnabled = totpEn == 1
	if tlim.Valid {
		v := tlim.Int64
		u.TrafficLimit = &v
	}
	u.SubRuleCategories = decodeStringSlice(catsRaw)
	u.SiteDenyCategories = decodeStringSlice(denyCatsRaw)
	u.SiteDenyDomains = decodeStringSlice(denyDomsRaw)
	attachPackage(d, u)
	return u, nil
}

func GetUserByName(d *sql.DB, username string) (*User, error) {
	var id int64
	if err := d.QueryRow(`SELECT id FROM users WHERE username=?`, username).Scan(&id); err != nil {
		return nil, err
	}
	return GetUser(d, id)
}

func GetUserBySubToken(d *sql.DB, token string) (*User, error) {
	var id int64
	if err := d.QueryRow(`SELECT id FROM users WHERE sub_token=?`, token).Scan(&id); err != nil {
		return nil, err
	}
	return GetUser(d, id)
}

func attachPackage(d *sql.DB, u *User) {
	var pid int64
	var name string
	var exp int64
	var trafficBytes int64
	var direction string
	err := d.QueryRow(`SELECT p.id, p.name, p.traffic_bytes, p.direction, up.expires_at FROM user_packages up JOIN packages p ON p.id=up.package_id WHERE up.user_id=?`, u.ID).
		Scan(&pid, &name, &trafficBytes, &direction, &exp)
	var pkg *Package
	if err == nil {
		u.PackageID = &pid
		u.PackageName = name
		u.PkgExpires = exp
		pkg = &Package{ID: pid, Name: name, TrafficBytes: trafficBytes, Direction: direction}
	}
	applyUserBilling(u, pkg)
}

func applyUserBilling(u *User, pkg *Package) {
	if u == nil {
		return
	}
	if pkg != nil {
		u.Direction = pkg.Direction
	}
	u.TrafficCap = UserLimitBytes(u, pkg)
	u.BilledBytes = UserBilledBytes(u, pkg)
	u.QuotaRatio = quotaPercent(u.BilledBytes, u.TrafficCap)
}

func quotaPercent(used, cap int64) int {
	if cap <= 0 || used <= 0 {
		return 0
	}
	if used >= cap {
		return 100
	}
	if used > math.MaxInt64/100 {
		return 99
	}
	n := int(used * 100 / cap)
	if n > 100 {
		return 100
	}
	return n
}

func queryIDs(d *sql.DB, q string, args ...any) ([]int64, error) {
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func ListUsers(d *sql.DB) ([]*User, error) {
	ids, err := queryIDs(d, `SELECT id FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	out := make([]*User, 0, len(ids))
	for _, id := range ids {
		u, err := GetUser(d, id)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func UpdateUser(d *sql.DB, u *User) error {
	en := 0
	if u.Enabled {
		en = 1
	}
	_, err := d.Exec(`UPDATE users SET username=?, remark=?, enabled=?, expires_at=?, traffic_limit=?, traffic_reset_day=?, speed_limit=?, sub_rule_preset=?, sub_rule_categories=?, site_deny_categories=?, site_deny_domains=?, site_filter_mode=? WHERE id=?`,
		u.Username, u.Remark, en, u.ExpiresAt, u.TrafficLimit, u.TrafficResetDay, u.SpeedLimit, u.SubRulePreset, encodeStringSlice(u.SubRuleCategories), encodeStringSlice(u.SiteDenyCategories), encodeStringSlice(u.SiteDenyDomains), u.SiteFilterMode, u.ID)
	return err
}

func decodeStringSlice(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil {
		return []string{}
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

func encodeStringSlice(names []string) string {
	if len(names) == 0 {
		return ""
	}
	raw, err := json.Marshal(names)
	if err != nil {
		return ""
	}
	return string(raw)
}

func SetUserTOTP(d *sql.DB, id int64, secret string, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	_, err := d.Exec(`UPDATE users SET totp_secret=?, totp_enabled=? WHERE id=?`, secret, en, id)
	return err
}

func ClearUserTOTP(d *sql.DB, id int64) error {
	return SetUserTOTP(d, id, "", false)
}

func SetUserPassword(d *sql.DB, id int64, hash string) error {
	_, err := d.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, id)
	return err
}

func SetUserPasswordPlain(d *sql.DB, id int64, plain string) error {
	_, err := d.Exec(`UPDATE users SET password_plain=? WHERE id=?`, plain, id)
	return err
}

func ClearUserPasswordPlain(d *sql.DB, id int64) error {
	return SetUserPasswordPlain(d, id, "")
}

func DeleteSessionsForUserExcept(d *sql.DB, userID int64, keep string) error {
	if strings.TrimSpace(keep) == "" {
		return DeleteSessionsForUser(d, userID)
	}
	_, err := d.Exec(`DELETE FROM sessions WHERE user_id=? AND token!=?`, userID, keep)
	return err
}

func RotateSubToken(d *sql.DB, id int64) (string, error) {
	tok, err := RandomHex(16)
	if err != nil {
		return "", err
	}
	_, err = d.Exec(`UPDATE users SET sub_token=? WHERE id=?`, tok, id)
	return tok, err
}

func ResetUserTraffic(d *sql.DB, id int64) error {
	_, err := d.Exec(`UPDATE users SET used_up=0, used_down=0, cycle_start=? WHERE id=?`, now(), id)
	return err
}

// maxUserBilledBytes is 1 PiB. Package quotas are a few hundred G.
const maxUserBilledBytes int64 = 1 << 50

var ErrUsedNegative = errors.New("已用流量不能为负")
var ErrUsedTooBig = errors.New("已用流量过大")

// SetUserBilledTraffic replaces the quota counter. Both 单向 and 双向 then
// report this number. The billing cycle and daily history stay as they are.
// Later samples add on top of this baseline.
func SetUserBilledTraffic(d *sql.DB, id, billed int64) error {
	if billed < 0 {
		return ErrUsedNegative
	}
	if billed > maxUserBilledBytes {
		return ErrUsedTooBig
	}
	res, err := d.Exec(`UPDATE users SET used_up=0, used_down=? WHERE id=?`, billed, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func AddUserTraffic(d *sql.DB, id int64, up, down int64) error {
	_, err := d.Exec(`UPDATE users SET used_up=used_up+?, used_down=used_down+? WHERE id=?`, up, down, id)
	return err
}

type TrafficWrite struct {
	UserID     int64
	InboundID  int64
	BilledUp   int64
	BilledDown int64
	RawUp      int64
	RawDown    int64
}

func AddTrafficBatch(d *sql.DB, day string, items []TrafficWrite) error {
	if len(items) == 0 {
		return nil
	}
	now := ClockNow(d)
	hour := HourKey(now)
	cutoff := HourKey(now.Add(-48 * time.Hour))
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	upStmt, err := tx.Prepare(`UPDATE users SET used_up=used_up+?, used_down=used_down+? WHERE id=?`)
	if err != nil {
		return err
	}
	defer upStmt.Close()
	dayStmt, err := tx.Prepare(`INSERT INTO traffic_daily(day,user_id,inbound_id,up,down) VALUES(?,?,?,?,?)
		ON CONFLICT(day,user_id,inbound_id) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`)
	if err != nil {
		return err
	}
	defer dayStmt.Close()
	hourStmt, err := tx.Prepare(`INSERT INTO traffic_hourly(hour,up,down) VALUES(?,?,?)
		ON CONFLICT(hour) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`)
	if err != nil {
		return err
	}
	defer hourStmt.Close()
	var hourUp, hourDown int64
	for _, it := range items {
		if it.BilledUp == 0 && it.BilledDown == 0 && it.RawUp == 0 && it.RawDown == 0 {
			continue
		}
		if _, err := upStmt.Exec(it.BilledUp, it.BilledDown, it.UserID); err != nil {
			return err
		}
		if _, err := dayStmt.Exec(day, it.UserID, it.InboundID, it.RawUp, it.RawDown); err != nil {
			return err
		}
		hourUp += it.RawUp
		hourDown += it.RawDown
	}
	if hourUp != 0 || hourDown != 0 {
		if _, err := hourStmt.Exec(hour, hourUp, hourDown); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM traffic_hourly WHERE hour<?`, cutoff); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func DeleteUser(d *sql.DB, id int64) error {
	if _, err := d.Exec(`DELETE FROM tg_binds WHERE user_id=?`, id); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

func PutSession(d *sql.DB, token string, userID, expiresAt int64) error {
	_, err := d.Exec(`INSERT INTO sessions(token,user_id,expires_at) VALUES(?,?,?)`, token, userID, expiresAt)
	return err
}

func GetSessionUser(d *sql.DB, token string) (*User, error) {
	var uid, exp int64
	if err := d.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE token=?`, token).Scan(&uid, &exp); err != nil {
		return nil, err
	}
	if exp < now() {
		_, _ = d.Exec(`DELETE FROM sessions WHERE token=?`, token)
		return nil, sql.ErrNoRows
	}
	return GetUser(d, uid)
}

func DeleteSession(d *sql.DB, token string) error {
	_, err := d.Exec(`DELETE FROM sessions WHERE token=?`, token)
	return err
}

func DeleteSessionsForUser(d *sql.DB, userID int64) error {
	_, err := d.Exec(`DELETE FROM sessions WHERE user_id=?`, userID)
	return err
}

type SessionCount struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Count    int    `json:"count"`
}

func ListSessionCounts(d *sql.DB) ([]SessionCount, error) {
	rows, err := d.Query(`SELECT s.user_id, u.username, COUNT(*)
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.expires_at>?
		GROUP BY s.user_id
		ORDER BY COUNT(*) DESC, s.user_id`, now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionCount
	for rows.Next() {
		var c SessionCount
		if err := rows.Scan(&c.UserID, &c.Username, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if out == nil {
		out = []SessionCount{}
	}
	return out, rows.Err()
}

func CreateServer(d *sql.DB, name, publicHost, token string) (*Server, error) {
	res, err := d.Exec(`INSERT INTO servers(name,public_host,token,created_at) VALUES(?,?,?,?)`, name, publicHost, token, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetServer(d, id)
}

func GetServer(d *sql.DB, id int64) (*Server, error) {
	s := &Server{}
	var dis int
	err := d.QueryRow(`SELECT id,name,public_host,token,online,last_seen,agent_ver,os,arch,connect_ip,config_rev,last_error,last_error_at,cores,created_at,traffic_limit,port_min,port_max,expires_at,traffic_reset_day,disable_ipv6 FROM servers WHERE id=?`, id).
		Scan(&s.ID, &s.Name, &s.PublicHost, &s.Token, &s.Online, &s.LastSeen, &s.AgentVer, &s.OS, &s.Arch, &s.ConnectIP, &s.ConfigRev, &s.LastError, &s.LastErrorAt, &s.Cores, &s.CreatedAt, &s.TrafficLimit, &s.PortMin, &s.PortMax, &s.ExpiresAt, &s.TrafficResetDay, &dis)
	if err != nil {
		return nil, err
	}
	s.DisableIPv6 = dis == 1
	return s, nil
}

func GetServerByToken(d *sql.DB, token string) (*Server, error) {
	var id int64
	if err := d.QueryRow(`SELECT id FROM servers WHERE token=?`, token).Scan(&id); err != nil {
		return nil, err
	}
	return GetServer(d, id)
}

func ListServers(d *sql.DB) ([]*Server, error) {
	ids, err := queryIDs(d, `SELECT id FROM servers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	out := make([]*Server, 0, len(ids))
	for _, id := range ids {
		s, err := GetServer(d, id)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func UpdateServer(d *sql.DB, s *Server) error {
	dis := 0
	if s.DisableIPv6 {
		dis = 1
	}
	_, err := d.Exec(`UPDATE servers SET name=?, public_host=?, traffic_limit=?, port_min=?, port_max=?, expires_at=?, traffic_reset_day=?, disable_ipv6=? WHERE id=?`, s.Name, s.PublicHost, s.TrafficLimit, s.PortMin, s.PortMax, s.ExpiresAt, s.TrafficResetDay, dis, s.ID)
	return err
}

func RotateServerToken(d *sql.DB, id int64) (string, error) {
	tok, err := RandomHex(20)
	if err != nil {
		return "", err
	}
	_, err = d.Exec(`UPDATE servers SET token=? WHERE id=?`, tok, id)
	return tok, err
}

func ServerOverQuota(s *Server, used int64) bool {
	return s != nil && s.TrafficLimit > 0 && used >= s.TrafficLimit
}

type TrafficSum struct {
	Up   int64
	Down int64
}

func ServerTrafficTotals(d *sql.DB) (map[int64]TrafficSum, error) {
	return queryServerTraffic(d, `SELECT i.server_id, COALESCE(SUM(t.up),0), COALESCE(SUM(t.down),0)
		FROM inbounds i
		LEFT JOIN traffic_daily t ON t.inbound_id = i.id
		GROUP BY i.server_id`)
}

func serverTrafficSince(d *sql.DB, fromDay string) (map[int64]TrafficSum, error) {
	return queryServerTraffic(d, `SELECT i.server_id, COALESCE(SUM(t.up),0), COALESCE(SUM(t.down),0)
		FROM inbounds i
		LEFT JOIN traffic_daily t ON t.inbound_id = i.id AND t.day >= ?
		GROUP BY i.server_id`, fromDay)
}

func queryServerTraffic(d *sql.DB, q string, args ...any) (map[int64]TrafficSum, error) {
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]TrafficSum{}
	for rows.Next() {
		var id int64
		var s TrafficSum
		if err := rows.Scan(&id, &s.Up, &s.Down); err != nil {
			return nil, err
		}
		out[id] = s
	}
	return out, rows.Err()
}

// ServerQuotaTotals is lifetime traffic, except servers with traffic_reset_day
// only count from the current monthly cycle.
func ServerQuotaTotals(d *sql.DB, servers []*Server, now time.Time) (map[int64]TrafficSum, error) {
	lifetime, err := ServerTrafficTotals(d)
	if err != nil {
		return nil, err
	}
	if lifetime == nil {
		lifetime = map[int64]TrafficSum{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	groups := map[string][]int64{}
	for _, s := range servers {
		day := ServerCycleStartDay(s, now)
		if day == "" {
			continue
		}
		groups[day] = append(groups[day], s.ID)
	}
	for day, ids := range groups {
		part, err := serverTrafficSince(d, day)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if t, ok := part[id]; ok {
				lifetime[id] = t
			} else {
				lifetime[id] = TrafficSum{}
			}
		}
	}
	return lifetime, nil
}

func DeleteServer(d *sql.DB, id int64) error {
	_, err := d.Exec(`DELETE FROM servers WHERE id=?`, id)
	return err
}

func MarkAllServersOffline(d *sql.DB) error {
	_, err := d.Exec(`UPDATE servers SET online=0`)
	return err
}

func MarkServerOnline(d *sql.DB, id int64, ver, osName, arch, ip string, cores []string) error {
	fill := shareFillIP(ip)
	_, err := d.Exec(`UPDATE servers SET online=1, last_seen=?, agent_ver=?, os=?, arch=?, connect_ip=?, cores=?,
		public_host=CASE WHEN TRIM(COALESCE(public_host,''))='' AND ?!='' THEN ? ELSE public_host END
		WHERE id=?`,
		now(), ver, osName, arch, ip, joinCores(cores), fill, fill, id)
	return err
}

func shareFillIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || ip == "127.0.0.1" || ip == "::1" || ip == "localhost" {
		return ""
	}
	return ip
}

func SetServerCores(d *sql.DB, id int64, cores []string) error {
	_, err := d.Exec(`UPDATE servers SET cores=? WHERE id=?`, joinCores(cores), id)
	return err
}

func joinCores(cores []string) string {
	seen := map[string]struct{}{}
	var out []string
	for _, c := range cores {
		c = normalizeCoreName(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return strings.Join(out, ",")
}

func normalizeCoreName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "sing-box", "singbox":
		return "singbox"
	default:
		return s
	}
}

func ServerHasCore(s *Server, core string) bool {
	if s == nil {
		return false
	}
	if strings.TrimSpace(s.Cores) == "" {
		return true
	}
	want := normalizeCoreName(core)
	if want == "" || want == "wireguard" {
		return true
	}
	for _, c := range strings.Split(s.Cores, ",") {
		if normalizeCoreName(c) == want {
			return true
		}
	}
	return false
}

func DisableInboundsOnPorts(d *sql.DB, serverID int64, ports []int) (int, error) {
	n := 0
	for _, p := range ports {
		if p < 1 || p > 65535 {
			continue
		}
		res, err := d.Exec(`UPDATE inbounds SET enabled=0 WHERE server_id=? AND port=? AND enabled=1`, serverID, p)
		if err != nil {
			return n, err
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	return n, nil
}

func MarkServerOffline(d *sql.DB, id int64) error {
	_, err := d.Exec(`UPDATE servers SET online=0 WHERE id=?`, id)
	return err
}

func SetServerRev(d *sql.DB, id int64, rev string) error {
	_, err := d.Exec(`UPDATE servers SET config_rev=? WHERE id=?`, rev, id)
	return err
}

func SetServerApplyError(d *sql.DB, id int64, applyErr error) error {
	if applyErr == nil {
		_, err := d.Exec(`UPDATE servers SET last_error='', last_error_at=0 WHERE id=?`, id)
		return err
	}
	msg := applyErr.Error()
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	_, err := d.Exec(`UPDATE servers SET last_error=?, last_error_at=? WHERE id=?`, msg, now(), id)
	return err
}

func certAuto(v bool) int {
	if v {
		return 1
	}
	return 0
}

func scanCert(scan func(dest ...any) error, withPEM bool) (*Certificate, error) {
	c := &Certificate{}
	var auto int
	var err error
	if withPEM {
		err = scan(&c.ID, &c.Name, &c.CertPEM, &c.KeyPEM, &c.Domains, &c.Source, &c.ExpiresAt, &c.AcmeEmail, &c.LastError, &auto)
	} else {
		err = scan(&c.ID, &c.Name, &c.Domains, &c.Source, &c.ExpiresAt, &c.AcmeEmail, &c.LastError, &auto)
	}
	if err != nil {
		return nil, err
	}
	c.AutoRenew = auto != 0
	if c.Source == "" {
		c.Source = "upload"
	}
	return c, nil
}

func CreateCert(d *sql.DB, c *Certificate) (*Certificate, error) {
	if c.Source == "" {
		c.Source = "upload"
	}
	res, err := d.Exec(
		`INSERT INTO certificates(name,cert_pem,key_pem,domains,source,expires_at,acme_email,last_error,auto_renew) VALUES(?,?,?,?,?,?,?,?,?)`,
		c.Name, c.CertPEM, c.KeyPEM, c.Domains, c.Source, c.ExpiresAt, c.AcmeEmail, c.LastError, certAuto(c.AutoRenew),
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetCert(d, id)
}

func GetCert(d *sql.DB, id int64) (*Certificate, error) {
	return scanCert(d.QueryRow(`SELECT id,name,cert_pem,key_pem,domains,source,expires_at,acme_email,last_error,auto_renew FROM certificates WHERE id=?`, id).Scan, true)
}

func ListCerts(d *sql.DB) ([]*Certificate, error) {
	rows, err := d.Query(`SELECT id,name,domains,source,expires_at,acme_email,last_error,auto_renew FROM certificates ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Certificate
	for rows.Next() {
		c, err := scanCert(rows.Scan, false)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func ListCertIDsDueRenew(d *sql.DB, before int64) ([]int64, error) {
	rows, err := d.Query(`SELECT id FROM certificates WHERE source='acme-cf' AND auto_renew=1 AND expires_at>0 AND expires_at<=? ORDER BY id`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func SetCertError(d *sql.DB, id int64, msg string) error {
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	_, err := d.Exec(`UPDATE certificates SET last_error=? WHERE id=?`, msg, id)
	return err
}

func DeleteCert(d *sql.DB, id int64) error {
	_, err := d.Exec(`DELETE FROM certificates WHERE id=?`, id)
	return err
}

func CreatePackage(d *sql.DB, name string, trafficBytes int64, cycleDays, resetDay int, direction string) (*Package, error) {
	if direction == "" {
		direction = "oneway"
	}
	if cycleDays < 0 {
		cycleDays = 0
	}
	res, err := d.Exec(`INSERT INTO packages(name,traffic_bytes,cycle_days,reset_day,direction,created_at) VALUES(?,?,?,?,?,?)`,
		name, trafficBytes, cycleDays, resetDay, direction, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetPackage(d, id)
}

const (
	PackageRelationsNone    = ""
	PackageRelationsInbound = "inbounds"
	PackageRelationsServer  = "servers"
)

func insertPackageTx(tx *sql.Tx, p *Package) (int64, error) {
	direction := p.Direction
	if direction == "" {
		direction = "oneway"
	}
	cycleDays := p.CycleDays
	if cycleDays < 0 {
		cycleDays = 0
	}
	mode, err := NormalizeResetMode(p.ResetMode)
	if err != nil {
		return 0, err
	}
	res, err := tx.Exec(`INSERT INTO packages(
		name,traffic_bytes,cycle_days,reset_day,direction,created_at,speed_limit,
		sub_rule_preset,sub_rule_categories,site_deny_categories,site_deny_domains,site_filter_mode,reset_mode
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.Name, p.TrafficBytes, cycleDays, p.ResetDay, direction, now(), p.SpeedLimit,
		p.SubRulePreset, encodeStringSlice(p.SubRuleCategories), encodeStringSlice(p.SiteDenyCategories), encodeStringSlice(p.SiteDenyDomains), p.SiteFilterMode, mode)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func updatePackageTx(tx *sql.Tx, p *Package) error {
	mode, err := NormalizeResetMode(p.ResetMode)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE packages SET name=?, traffic_bytes=?, cycle_days=?, reset_day=?, direction=?, speed_limit=?, sub_rule_preset=?, sub_rule_categories=?, site_deny_categories=?, site_deny_domains=?, site_filter_mode=?, reset_mode=? WHERE id=?`,
		p.Name, p.TrafficBytes, p.CycleDays, p.ResetDay, p.Direction, p.SpeedLimit, p.SubRulePreset, encodeStringSlice(p.SubRuleCategories), encodeStringSlice(p.SiteDenyCategories), encodeStringSlice(p.SiteDenyDomains), p.SiteFilterMode, mode, p.ID)
	return err
}

func setPackageInboundsTx(tx *sql.Tx, pkgID int64, inboundIDs []int64, multipliers []float64, transitIDs []int64) error {
	if _, err := tx.Exec(`DELETE FROM package_inbounds WHERE package_id=?`, pkgID); err != nil {
		return err
	}
	transit := map[int64]struct{}{}
	for _, id := range transitIDs {
		if id > 0 {
			transit[id] = struct{}{}
		}
	}
	for i, iid := range inboundIDs {
		m := 1.0
		if i < len(multipliers) {
			got, err := normalizeMultiplier(multipliers[i])
			if err != nil {
				return err
			}
			m = got
		}
		flag := 0
		if _, ok := transit[iid]; ok {
			flag = 1
		}
		if _, err := tx.Exec(`INSERT INTO package_inbounds(package_id,inbound_id,multiplier,transit) VALUES(?,?,?,?)`, pkgID, iid, m, flag); err != nil {
			return err
		}
	}
	return nil
}

func setPackageServersTx(tx *sql.Tx, pkgID int64, serverIDs []int64) error {
	if _, err := tx.Exec(`DELETE FROM package_servers WHERE package_id=?`, pkgID); err != nil {
		return err
	}
	seen := map[int64]struct{}{}
	for _, sid := range serverIDs {
		if sid == 0 {
			continue
		}
		if _, ok := seen[sid]; ok {
			continue
		}
		seen[sid] = struct{}{}
		if _, err := tx.Exec(`INSERT INTO package_servers(package_id,server_id,multiplier) VALUES(?,?,1)`, pkgID, sid); err != nil {
			return err
		}
	}
	return nil
}

func CreatePackageConfigured(d *sql.DB, p *Package, relation string, ids []int64, multipliers []float64, transitIDs []int64) (*Package, error) {
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id, err := insertPackageTx(tx, p)
	if err != nil {
		return nil, err
	}
	switch relation {
	case PackageRelationsInbound:
		if err := setPackageInboundsTx(tx, id, ids, multipliers, transitIDs); err != nil {
			return nil, err
		}
		if err := setPackageServersTx(tx, id, nil); err != nil {
			return nil, err
		}
	case PackageRelationsServer:
		if err := setPackageServersTx(tx, id, ids); err != nil {
			return nil, err
		}
		if err := setPackageInboundsTx(tx, id, nil, nil, nil); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("节点绑定模式无效")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetPackage(d, id)
}

func UpdatePackageConfigured(d *sql.DB, p *Package, relation string, ids []int64, multipliers []float64, transitIDs []int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := updatePackageTx(tx, p); err != nil {
		return err
	}
	switch relation {
	case PackageRelationsNone:
	case PackageRelationsInbound:
		if err := setPackageInboundsTx(tx, p.ID, ids, multipliers, transitIDs); err != nil {
			return err
		}
		if err := setPackageServersTx(tx, p.ID, nil); err != nil {
			return err
		}
	case PackageRelationsServer:
		if err := setPackageServersTx(tx, p.ID, ids); err != nil {
			return err
		}
		if err := setPackageInboundsTx(tx, p.ID, nil, nil, nil); err != nil {
			return err
		}
	default:
		return fmt.Errorf("节点绑定模式无效")
	}
	return tx.Commit()
}

func GetPackage(d *sql.DB, id int64) (*Package, error) {
	p := &Package{}
	var catsRaw, denyCatsRaw, denyDomsRaw string
	err := d.QueryRow(`SELECT id,name,traffic_bytes,cycle_days,reset_day,direction,created_at,speed_limit,sub_rule_preset,sub_rule_categories,site_deny_categories,site_deny_domains,site_filter_mode,reset_mode FROM packages WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.TrafficBytes, &p.CycleDays, &p.ResetDay, &p.Direction, &p.CreatedAt, &p.SpeedLimit, &p.SubRulePreset, &catsRaw, &denyCatsRaw, &denyDomsRaw, &p.SiteFilterMode, &p.ResetMode)
	if err != nil {
		return nil, err
	}
	p.SubRuleCategories = decodeStringSlice(catsRaw)
	p.SiteDenyCategories = decodeStringSlice(denyCatsRaw)
	p.SiteDenyDomains = decodeStringSlice(denyDomsRaw)
	rows, err := d.Query(`SELECT inbound_id, multiplier, transit FROM package_inbounds WHERE package_id=?`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var iid int64
		var m float64
		var transit int
		if err := rows.Scan(&iid, &m, &transit); err != nil {
			rows.Close()
			return nil, err
		}
		p.InboundIDs = append(p.InboundIDs, iid)
		p.Multipliers = append(p.Multipliers, m)
		if transit != 0 {
			p.TransitIDs = append(p.TransitIDs, iid)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	srows, err := d.Query(`SELECT server_id FROM package_servers WHERE package_id=? ORDER BY server_id`, id)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	p.ServerIDs = []int64{}
	for srows.Next() {
		var sid int64
		if err := srows.Scan(&sid); err != nil {
			return nil, err
		}
		p.ServerIDs = append(p.ServerIDs, sid)
	}
	return p, srows.Err()
}

func ListPackages(d *sql.DB) ([]*Package, error) {
	ids, err := queryIDs(d, `SELECT id FROM packages ORDER BY id`)
	if err != nil {
		return nil, err
	}
	out := make([]*Package, 0, len(ids))
	for _, id := range ids {
		p, err := GetPackage(d, id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func SetPackageInbounds(d *sql.DB, pkgID int64, inboundIDs []int64, multipliers []float64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setPackageInboundsTx(tx, pkgID, inboundIDs, multipliers, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func SetPackageServers(d *sql.DB, pkgID int64, serverIDs []int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setPackageServersTx(tx, pkgID, serverIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func UpdatePackage(d *sql.DB, p *Package) error {
	mode, err := NormalizeResetMode(p.ResetMode)
	if err != nil {
		return err
	}
	_, err = d.Exec(`UPDATE packages SET name=?, traffic_bytes=?, cycle_days=?, reset_day=?, direction=?, speed_limit=?, sub_rule_preset=?, sub_rule_categories=?, site_deny_categories=?, site_deny_domains=?, site_filter_mode=?, reset_mode=? WHERE id=?`,
		p.Name, p.TrafficBytes, p.CycleDays, p.ResetDay, p.Direction, p.SpeedLimit, p.SubRulePreset, encodeStringSlice(p.SubRuleCategories), encodeStringSlice(p.SiteDenyCategories), encodeStringSlice(p.SiteDenyDomains), p.SiteFilterMode, mode, p.ID)
	return err
}

// CopyPackagePolicy writes suite defaults onto a user. Call only on 开户 / 换套餐.
func CopyPackagePolicy(dst *User, p *Package) {
	if dst == nil || p == nil {
		return
	}
	dst.SpeedLimit = p.SpeedLimit
	dst.SubRulePreset = p.SubRulePreset
	dst.SubRuleCategories = append([]string{}, p.SubRuleCategories...)
	dst.SiteFilterMode = p.SiteFilterMode
	dst.SiteDenyCategories = append([]string{}, p.SiteDenyCategories...)
	dst.SiteDenyDomains = append([]string{}, p.SiteDenyDomains...)
}

func DeletePackage(d *sql.DB, id int64) error {
	if _, err := d.Exec(`DELETE FROM user_packages WHERE package_id=?`, id); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM packages WHERE id=?`, id)
	return err
}

func BindUserPackage(d *sql.DB, userID, packageID, expiresAt int64) error {
	_, err := d.Exec(`INSERT INTO user_packages(user_id,package_id,bound_at,expires_at) VALUES(?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET package_id=excluded.package_id, bound_at=excluded.bound_at, expires_at=excluded.expires_at`,
		userID, packageID, now(), expiresAt)
	if err != nil {
		return err
	}
	_, err = d.Exec(`UPDATE users SET cycle_start=?, used_up=0, used_down=0, expires_at=? WHERE id=?`, now(), expiresAt, userID)
	return err
}

// SetPackageExpiry updates expiry only. Does not reset traffic (unlike BindUserPackage).
func SetPackageExpiry(d *sql.DB, userID, expiresAt int64) error {
	if _, err := d.Exec(`UPDATE users SET expires_at=? WHERE id=?`, expiresAt, userID); err != nil {
		return err
	}
	_, err := d.Exec(`UPDATE user_packages SET expires_at=? WHERE user_id=?`, expiresAt, userID)
	return err
}

func UnbindUserPackage(d *sql.DB, userID int64) error {
	_, err := d.Exec(`DELETE FROM user_packages WHERE user_id=?`, userID)
	return err
}

func CreateInbound(d *sql.DB, in *Inbound) (*Inbound, error) {
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if in.Port != 0 {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM inbounds WHERE server_id=? AND port=?`, in.ServerID, in.Port).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, fmt.Errorf("该服务器上端口已被占用")
		}
	}
	res, err := tx.Exec(`INSERT INTO inbounds(server_id,name,profile,protocol,network,security,core,listen,port,enabled,settings,cert_id,line_kind,exit_inbound_id,exit_uri,reject_cn,owner_user_id,entry_inbound_id,auth_hold,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.ServerID, in.Name, in.Profile, in.Protocol, in.Network, in.Security, in.Core, in.Listen, in.Port, boolInt(in.Enabled),
		in.Settings, in.CertID, nz(in.LineKind, "direct"), in.ExitInboundID, in.ExitURI, boolInt(in.RejectCN), in.OwnerUserID, in.EntryInboundID, boolInt(in.AuthHold), now())
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetInbound(d, id)
}

func GetInbound(d *sql.DB, id int64) (*Inbound, error) {
	in := &Inbound{}
	var en, rejectCN, authHold int
	var certID, exitID sql.NullInt64
	err := d.QueryRow(`SELECT i.id,i.server_id,i.name,i.profile,i.protocol,i.network,i.security,i.core,i.listen,i.port,i.enabled,i.settings,i.cert_id,i.line_kind,i.exit_inbound_id,i.exit_uri,i.reject_cn,i.owner_user_id,i.entry_inbound_id,i.auth_hold,i.created_at,
		s.name, s.public_host, s.connect_ip, s.online
		FROM inbounds i JOIN servers s ON s.id=i.server_id WHERE i.id=?`, id).
		Scan(&in.ID, &in.ServerID, &in.Name, &in.Profile, &in.Protocol, &in.Network, &in.Security, &in.Core, &in.Listen, &in.Port, &en, &in.Settings, &certID, &in.LineKind, &exitID, &in.ExitURI, &rejectCN, &in.OwnerUserID, &in.EntryInboundID, &authHold, &in.CreatedAt,
			&in.ServerName, &in.ServerHost, &in.ConnectIP, &in.ServerOnline)
	if err != nil {
		return nil, err
	}
	in.Enabled = en == 1
	in.RejectCN = rejectCN == 1
	in.AuthHold = authHold == 1
	if certID.Valid {
		v := certID.Int64
		in.CertID = &v
	}
	if exitID.Valid {
		v := exitID.Int64
		in.ExitInboundID = &v
	}
	return in, nil
}

func ListInbounds(d *sql.DB) ([]*Inbound, error) {
	ids, err := queryIDs(d, `SELECT id FROM inbounds ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return getInbounds(d, ids)
}

func ListInboundsByServer(d *sql.DB, serverID int64) ([]*Inbound, error) {
	ids, err := queryIDs(d, `SELECT id FROM inbounds WHERE server_id=? ORDER BY id`, serverID)
	if err != nil {
		return nil, err
	}
	return getInbounds(d, ids)
}

func getInbounds(d *sql.DB, ids []int64) ([]*Inbound, error) {
	out := make([]*Inbound, 0, len(ids))
	for _, id := range ids {
		in, err := GetInbound(d, id)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, nil
}

func UsedPortsOnServer(d *sql.DB, serverID int64, excludeID int64) (map[int]struct{}, error) {
	rows, err := d.Query(`SELECT id, port FROM inbounds WHERE server_id=?`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	used := map[int]struct{}{}
	for rows.Next() {
		var id int64
		var port int
		if err := rows.Scan(&id, &port); err != nil {
			return nil, err
		}
		if id == excludeID {
			continue
		}
		used[port] = struct{}{}
	}
	return used, rows.Err()
}

func UpdateInbound(d *sql.DB, in *Inbound) error {
	_, err := d.Exec(`UPDATE inbounds SET name=?, port=?, enabled=?, settings=?, cert_id=?, line_kind=?, exit_inbound_id=?, exit_uri=?, reject_cn=? WHERE id=?`,
		in.Name, in.Port, boolInt(in.Enabled), in.Settings, in.CertID, nz(in.LineKind, "direct"), in.ExitInboundID, in.ExitURI, boolInt(in.RejectCN), in.ID)
	return err
}

func DeleteInbound(d *sql.DB, id int64) error {
	_, err := d.Exec(`DELETE FROM inbounds WHERE id=?`, id)
	return err
}

func UpsertClient(d *sql.DB, c *Client) error {
	_, err := d.Exec(`INSERT INTO clients(inbound_id,user_id,email,uuid,password,username,enabled) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(inbound_id,user_id) DO UPDATE SET email=excluded.email, uuid=excluded.uuid, password=excluded.password, username=excluded.username, enabled=excluded.enabled`,
		c.InboundID, c.UserID, c.Email, c.UUID, c.Password, c.Username, boolInt(c.Enabled))
	return err
}

func ListClientsByInbound(d *sql.DB, inboundID int64) ([]*Client, error) {
	rows, err := d.Query(`SELECT id,inbound_id,user_id,email,uuid,password,username,enabled FROM clients WHERE inbound_id=? ORDER BY id`, inboundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanClients(rows)
}

func ListClientsByUser(d *sql.DB, userID int64) ([]*Client, error) {
	rows, err := d.Query(`SELECT id,inbound_id,user_id,email,uuid,password,username,enabled FROM clients WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanClients(rows)
}

func scanClients(rows *sql.Rows) ([]*Client, error) {
	var out []*Client
	for rows.Next() {
		c := &Client{}
		var en int
		if err := rows.Scan(&c.ID, &c.InboundID, &c.UserID, &c.Email, &c.UUID, &c.Password, &c.Username, &en); err != nil {
			return nil, err
		}
		c.Enabled = en == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

func SetClientsEnabledForUser(d *sql.DB, userID int64, enabled bool) error {
	_, err := d.Exec(`UPDATE clients SET enabled=? WHERE user_id=?`, boolInt(enabled), userID)
	return err
}

func DeleteClientsForUser(d *sql.DB, userID int64) error {
	_, err := d.Exec(`DELETE FROM clients WHERE user_id=?`, userID)
	return err
}

func AddDailyTraffic(d *sql.DB, day string, userID, inboundID, up, down int64) error {
	_, err := d.Exec(`INSERT INTO traffic_daily(day,user_id,inbound_id,up,down) VALUES(?,?,?,?,?)
		ON CONFLICT(day,user_id,inbound_id) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`,
		day, userID, inboundID, up, down)
	return err
}

func ClientByEmail(d *sql.DB, email string) (*Client, error) {
	c := &Client{}
	var en int
	err := d.QueryRow(`SELECT id,inbound_id,user_id,email,uuid,password,username,enabled FROM clients WHERE email=?`, email).
		Scan(&c.ID, &c.InboundID, &c.UserID, &c.Email, &c.UUID, &c.Password, &c.Username, &en)
	if err != nil {
		return nil, err
	}
	c.Enabled = en == 1
	return c, nil
}

func ClientsByUsernames(d *sql.DB, names []string) ([]*Client, error) {
	ph, args := strPlaceholders(names)
	if ph == "" {
		return nil, nil
	}
	rows, err := d.Query(`SELECT id,inbound_id,user_id,email,uuid,password,username,enabled FROM clients WHERE username IN (`+ph+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanClients(rows)
}

func ClientsByUUIDs(d *sql.DB, ids []string) ([]*Client, error) {
	ph, args := strPlaceholders(ids)
	if ph == "" {
		return nil, nil
	}
	rows, err := d.Query(`SELECT id,inbound_id,user_id,email,uuid,password,username,enabled FROM clients WHERE uuid IN (`+ph+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanClients(rows)
}

func strPlaceholders(vals []string) (string, []any) {
	ph := make([]string, 0, len(vals))
	args := make([]any, 0, len(vals))
	seen := map[string]struct{}{}
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		ph = append(ph, "?")
		args = append(args, v)
	}
	return strings.Join(ph, ","), args
}

func ClientByIdentity(d *sql.DB, id string) (*Client, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, sql.ErrNoRows
	}
	c, err := ClientByEmail(d, id)
	if err == nil {
		return c, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	c = &Client{}
	var en int
	err = d.QueryRow(`SELECT id,inbound_id,user_id,email,uuid,password,username,enabled FROM clients WHERE username=? ORDER BY id LIMIT 1`, id).
		Scan(&c.ID, &c.InboundID, &c.UserID, &c.Email, &c.UUID, &c.Password, &c.Username, &en)
	if err != nil {
		return nil, err
	}
	c.Enabled = en == 1
	return c, nil
}

type TrafficPoint struct {
	Day  string `json:"day,omitempty"`
	Hour string `json:"hour,omitempty"`
	Up   int64  `json:"up"`
	Down int64  `json:"down"`
}

type NamedTraffic struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Up   int64  `json:"up"`
	Down int64  `json:"down"`
}

func TrafficSeries(d *sql.DB, from, to string, userID, inboundID int64) ([]TrafficPoint, error) {
	q := `SELECT day, COALESCE(SUM(up),0), COALESCE(SUM(down),0) FROM traffic_daily WHERE day>=? AND day<=?`
	args := []any{from, to}
	if userID > 0 {
		q += ` AND user_id=?`
		args = append(args, userID)
	}
	if inboundID > 0 {
		q += ` AND inbound_id=?`
		args = append(args, inboundID)
	}
	q += ` GROUP BY day ORDER BY day`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrafficPoint
	for rows.Next() {
		var p TrafficPoint
		if err := rows.Scan(&p.Day, &p.Up, &p.Down); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if out == nil {
		out = []TrafficPoint{}
	}
	return out, rows.Err()
}

func TrafficHourSeries(d *sql.DB, from, to string) ([]TrafficPoint, error) {
	rows, err := d.Query(`SELECT hour, up, down FROM traffic_hourly WHERE hour>=? AND hour<=? ORDER BY hour`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrafficPoint
	for rows.Next() {
		var p TrafficPoint
		if err := rows.Scan(&p.Hour, &p.Up, &p.Down); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if out == nil {
		out = []TrafficPoint{}
	}
	return out, rows.Err()
}

func HourRange(now time.Time, n int) (from, to string) {
	if n < 1 {
		n = 24
	}
	end := hourFloor(now)
	start := end.Add(-time.Duration(n-1) * time.Hour)
	return HourKey(start), HourKey(end)
}

func FillTrafficHours(now time.Time, n int, got []TrafficPoint) []TrafficPoint {
	if n < 1 {
		n = 24
	}
	end := hourFloor(now)
	start := end.Add(-time.Duration(n-1) * time.Hour)
	idx := map[string]TrafficPoint{}
	for _, p := range got {
		idx[p.Hour] = p
	}
	var out []TrafficPoint
	for t := start; !t.After(end); t = t.Add(time.Hour) {
		key := HourKey(t)
		if p, ok := idx[key]; ok {
			out = append(out, p)
		} else {
			out = append(out, TrafficPoint{Hour: key})
		}
	}
	return out
}

func FillTrafficDays(from, to string, got []TrafficPoint) []TrafficPoint {
	start, err1 := time.ParseInLocation("2006-01-02", from, time.UTC)
	end, err2 := time.ParseInLocation("2006-01-02", to, time.UTC)
	if err1 != nil || err2 != nil || end.Before(start) {
		if got == nil {
			return []TrafficPoint{}
		}
		return got
	}
	idx := map[string]TrafficPoint{}
	for _, p := range got {
		idx[p.Day] = p
	}
	var out []TrafficPoint
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		if p, ok := idx[key]; ok {
			out = append(out, p)
		} else {
			out = append(out, TrafficPoint{Day: key})
		}
	}
	return out
}

func TrafficByUser(d *sql.DB, from, to string) ([]NamedTraffic, error) {
	rows, err := d.Query(`SELECT t.user_id, u.username, COALESCE(SUM(t.up),0), COALESCE(SUM(t.down),0)
		FROM traffic_daily t JOIN users u ON u.id=t.user_id
		WHERE t.day>=? AND t.day<=?
		GROUP BY t.user_id
		ORDER BY SUM(t.up)+SUM(t.down) DESC, t.user_id
		LIMIT 100`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNamedTraffic(rows)
}

func TrafficByInbound(d *sql.DB, from, to string, userID int64) ([]NamedTraffic, error) {
	q := `SELECT t.inbound_id, i.name, COALESCE(SUM(t.up),0), COALESCE(SUM(t.down),0)
		FROM traffic_daily t JOIN inbounds i ON i.id=t.inbound_id
		WHERE t.day>=? AND t.day<=?`
	args := []any{from, to}
	if userID > 0 {
		q += ` AND t.user_id=?`
		args = append(args, userID)
	}
	q += ` GROUP BY t.inbound_id ORDER BY SUM(t.up)+SUM(t.down) DESC, t.inbound_id LIMIT 100`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNamedTraffic(rows)
}

func scanNamedTraffic(rows *sql.Rows) ([]NamedTraffic, error) {
	var out []NamedTraffic
	for rows.Next() {
		var n NamedTraffic
		if err := rows.Scan(&n.ID, &n.Name, &n.Up, &n.Down); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if out == nil {
		out = []NamedTraffic{}
	}
	return out, rows.Err()
}

func InboundTrafficTotals(d *sql.DB) (map[int64]TrafficSum, error) {
	rows, err := d.Query(`SELECT inbound_id, COALESCE(SUM(up),0), COALESCE(SUM(down),0) FROM traffic_daily GROUP BY inbound_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]TrafficSum{}
	for rows.Next() {
		var id int64
		var s TrafficSum
		if err := rows.Scan(&id, &s.Up, &s.Down); err != nil {
			return nil, err
		}
		out[id] = s
	}
	return out, rows.Err()
}

func UserInboundTrafficTotals(d *sql.DB, userID int64) (map[int64]TrafficSum, error) {
	rows, err := d.Query(`SELECT inbound_id, COALESCE(SUM(up),0), COALESCE(SUM(down),0) FROM traffic_daily WHERE user_id=? GROUP BY inbound_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]TrafficSum{}
	for rows.Next() {
		var id int64
		var s TrafficSum
		if err := rows.Scan(&id, &s.Up, &s.Down); err != nil {
			return nil, err
		}
		out[id] = s
	}
	return out, rows.Err()
}

// UserBilledBytes is the quota-facing total.
// 单向 counts downlink only; 双向 counts uplink + downlink.
// Node multipliers are already folded into used_up / used_down at write time.
func UserBilledBytes(u *User, pkg *Package) int64 {
	if u == nil {
		return 0
	}
	if pkg != nil && pkg.Direction == "oneway" {
		return u.UsedDown
	}
	return u.UsedUp + u.UsedDown
}

func UserLimitBytes(u *User, pkg *Package) int64 {
	if u == nil {
		return 0
	}
	if u.TrafficLimit != nil {
		return *u.TrafficLimit
	}
	if pkg != nil {
		return pkg.TrafficBytes
	}
	return 0
}

func UserAccessOK(u *User, pkg *Package) bool {
	if u == nil || !u.Enabled || u.Role == "admin" {
		return u != nil && u.Enabled
	}
	if u.ExpiresAt > 0 && u.ExpiresAt < now() {
		return false
	}
	if u.PkgExpires > 0 && u.PkgExpires < now() {
		return false
	}
	// Missing package row (deleted or lookup failed) must not fail open.
	if pkg == nil {
		return false
	}
	lim := UserLimitBytes(u, pkg)
	if lim > 0 && UserBilledBytes(u, pkg) >= lim {
		return false
	}
	return true
}

func UserNeedsProvision(d *sql.DB, u *User) (bool, error) {
	if u == nil {
		return false, nil
	}
	var pkg *Package
	if u.PackageID != nil {
		p, err := GetPackage(d, *u.PackageID)
		if err == nil {
			pkg = p
		}
	}
	ok := UserAccessOK(u, pkg)
	clients, err := ListClientsByUser(d, u.ID)
	if err != nil {
		return true, err
	}
	for _, c := range clients {
		if c != nil && c.Enabled && !ok {
			return true, nil
		}
	}
	return false, nil
}

func InboundIDsForUser(d *sql.DB, u *User) ([]int64, error) {
	if u.Role == "admin" {
		return queryIDs(d, `SELECT id FROM inbounds WHERE owner_user_id=0 ORDER BY id`)
	}
	var shared []int64
	if u.PackageID != nil {
		p, err := GetPackage(d, *u.PackageID)
		if err != nil {
			return nil, err
		}
		shared, err = PackageInboundIDs(d, p)
		if err != nil {
			return nil, err
		}
	}
	owned, err := queryIDs(d, `SELECT id FROM inbounds WHERE owner_user_id=? ORDER BY id`, u.ID)
	if err != nil {
		return nil, err
	}
	if len(owned) == 0 {
		return shared, nil
	}
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(shared)+len(owned))
	for _, id := range shared {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range owned {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func PackageInboundIDs(d *sql.DB, p *Package) ([]int64, error) {
	if p == nil {
		return nil, nil
	}
	if len(p.ServerIDs) > 0 {
		ph, args := intPlaceholders(p.ServerIDs)
		ids, err := queryIDs(d, `SELECT id FROM inbounds WHERE owner_user_id=0 AND server_id IN (`+ph+`) ORDER BY id`, args...)
		if err != nil {
			return nil, err
		}
		if ids == nil {
			ids = []int64{}
		}
		return ids, nil
	}
	if len(p.InboundIDs) == 0 {
		return []int64{}, nil
	}
	ph, args := intPlaceholders(p.InboundIDs)
	rows, err := queryIDs(d, `SELECT id FROM inbounds WHERE owner_user_id=0 AND id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	keep := map[int64]struct{}{}
	for _, id := range rows {
		keep[id] = struct{}{}
	}
	out := make([]int64, 0, len(rows))
	for _, id := range p.InboundIDs {
		if _, ok := keep[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

func intPlaceholders(ids []int64) (string, []any) {
	ph := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		ph = append(ph, "?")
		args = append(args, id)
	}
	return strings.Join(ph, ","), args
}

func AddAudit(d *sql.DB, userID *int64, action, detail string) {
	_, _ = d.Exec(`INSERT INTO audit(at,user_id,action,detail) VALUES(?,?,?,?)`, now(), userID, action, detail)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nz(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func EmailFor(userID, inboundID int64) string {
	return fmt.Sprintf("u%d.i%d", userID, inboundID)
}

func TouchServerLastSeen(d *sql.DB, id int64) error {
	_, err := d.Exec(`UPDATE servers SET last_seen=? WHERE id=?`, now(), id)
	return err
}

func UpdateCert(d *sql.DB, c *Certificate) error {
	_, err := d.Exec(`UPDATE certificates SET name=?, cert_pem=?, key_pem=?, domains=?, source=?, expires_at=?, acme_email=?, last_error=?, auto_renew=? WHERE id=?`,
		c.Name, c.CertPEM, c.KeyPEM, c.Domains, c.Source, c.ExpiresAt, c.AcmeEmail, c.LastError, certAuto(c.AutoRenew), c.ID)
	return err
}

func CountInboundsByCert(d *sql.DB, certID int64) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM inbounds WHERE cert_id=?`, certID).Scan(&n)
	return n, err
}

func CountExitsTo(d *sql.DB, inboundID int64) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM inbounds WHERE exit_inbound_id=?`, inboundID).Scan(&n)
	return n, err
}

func GetClient(d *sql.DB, inboundID, userID int64) (*Client, error) {
	c := &Client{}
	var en int
	err := d.QueryRow(`SELECT id,inbound_id,user_id,email,uuid,password,username,enabled FROM clients WHERE inbound_id=? AND user_id=?`, inboundID, userID).
		Scan(&c.ID, &c.InboundID, &c.UserID, &c.Email, &c.UUID, &c.Password, &c.Username, &en)
	if err != nil {
		return nil, err
	}
	c.Enabled = en == 1
	return c, nil
}

func DeleteClientsNotIn(d *sql.DB, userID int64, keep []int64) error {
	if len(keep) == 0 {
		return DeleteClientsForUser(d, userID)
	}
	args := make([]any, 0, 1+len(keep))
	args = append(args, userID)
	var b strings.Builder
	b.WriteString(`DELETE FROM clients WHERE user_id=? AND inbound_id NOT IN (`)
	for i, id := range keep {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('?')
		args = append(args, id)
	}
	b.WriteByte(')')
	_, err := d.Exec(b.String(), args...)
	return err
}

func ListAudit(d *sql.DB, limit int) ([]*Audit, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.Query(`SELECT id,at,user_id,action,detail FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Audit
	for rows.Next() {
		a := &Audit{}
		var uid sql.NullInt64
		if err := rows.Scan(&a.ID, &a.At, &uid, &a.Action, &a.Detail); err != nil {
			return nil, err
		}
		if uid.Valid {
			v := uid.Int64
			a.UserID = &v
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func normalizeMultiplier(m float64) (float64, error) {
	if math.IsNaN(m) || math.IsInf(m, 0) {
		return 0, fmt.Errorf("倍率无效")
	}
	if m <= 0 {
		return 1, nil
	}
	if m > 1000 {
		return 0, fmt.Errorf("倍率不能超过 1000")
	}
	return m, nil
}

func PackageMultiplier(p *Package, inboundID int64) float64 {
	if p == nil {
		return 1
	}
	for i, id := range p.InboundIDs {
		if id == inboundID {
			if i < len(p.Multipliers) && p.Multipliers[i] > 0 {
				return p.Multipliers[i]
			}
			return 1
		}
	}
	return 1
}

// TransitEntryOK reports a shared direct node that can authorize its machine
// as a personal forward entry. Chain lines, port forwards, and WireGuard cannot.
func TransitEntryOK(in *Inbound) bool {
	if in == nil || in.OwnerUserID != 0 {
		return false
	}
	if in.LineKind == "chain" || strings.TrimSpace(in.ExitURI) != "" {
		return false
	}
	switch in.Profile {
	case "", "port-forward", "wireguard":
		return false
	default:
		return true
	}
}

func UserIDsByPackage(d *sql.DB, packageID int64) ([]int64, error) {
	return queryIDs(d, `SELECT user_id FROM user_packages WHERE package_id=? ORDER BY user_id`, packageID)
}

func PackageIDsByInbound(d *sql.DB, inboundID int64) ([]int64, error) {
	return queryIDs(d, `SELECT package_id FROM package_inbounds WHERE inbound_id=? ORDER BY package_id`, inboundID)
}

func CountOwnedInbounds(d *sql.DB, userID int64) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM inbounds WHERE owner_user_id=?`, userID).Scan(&n)
	return n, err
}

func ListInboundsByOwner(d *sql.DB, userID int64) ([]*Inbound, error) {
	ids, err := queryIDs(d, `SELECT id FROM inbounds WHERE owner_user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	return getInbounds(d, ids)
}

func SetInboundRunState(d *sql.DB, id int64, enabled, authHold bool) error {
	_, err := d.Exec(`UPDATE inbounds SET enabled=?, auth_hold=? WHERE id=?`, boolInt(enabled), boolInt(authHold), id)
	return err
}

func DeleteOwnedInbounds(d *sql.DB, userID int64) ([]int64, error) {
	servers, err := queryIDs(d, `SELECT DISTINCT server_id FROM inbounds WHERE owner_user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	if _, err := d.Exec(`DELETE FROM inbounds WHERE owner_user_id=?`, userID); err != nil {
		return nil, err
	}
	return servers, nil
}

// ValidatePackageNodes rejects personal forwards inside a shared package, and
// transit marks that are not a checked direct node.
func ValidatePackageNodes(d *sql.DB, inboundIDs, transitIDs []int64) error {
	picked := map[int64]struct{}{}
	for _, id := range inboundIDs {
		if id <= 0 {
			continue
		}
		if _, ok := picked[id]; ok {
			continue
		}
		picked[id] = struct{}{}
		in, err := GetInbound(d, id)
		if err != nil {
			return fmt.Errorf("节点不存在")
		}
		if in.OwnerUserID != 0 {
			return fmt.Errorf("这是用户自己的中转，不能放进套餐")
		}
	}
	for _, id := range transitIDs {
		if id <= 0 {
			continue
		}
		if _, ok := picked[id]; !ok {
			return fmt.Errorf("中转入口必须是已勾选的节点")
		}
		in, err := GetInbound(d, id)
		if err != nil {
			return fmt.Errorf("节点不存在")
		}
		if !TransitEntryOK(in) {
			return fmt.Errorf("链式线路、端口中转和 WireGuard 不能当中转入口")
		}
	}
	return nil
}

func transitServerSet(d *sql.DB, u *User) (map[int64]struct{}, error) {
	out := map[int64]struct{}{}
	if u == nil || u.PackageID == nil {
		return out, nil
	}
	p, err := GetPackage(d, *u.PackageID)
	if err != nil {
		return out, nil
	}
	for _, id := range p.TransitIDs {
		in, err := GetInbound(d, id)
		if err != nil || in == nil || !TransitEntryOK(in) {
			continue
		}
		out[in.ServerID] = struct{}{}
	}
	return out, nil
}

// UserHasTransit reports that the user's package marks at least one entry.
func UserHasTransit(d *sql.DB, u *User) bool {
	set, err := transitServerSet(d, u)
	return err == nil && len(set) > 0
}

// UserTransitServer reports that this machine is still marked on the user's package.
func UserTransitServer(d *sql.DB, u *User, serverID int64) bool {
	if serverID == 0 {
		return false
	}
	set, err := transitServerSet(d, u)
	if err != nil {
		return false
	}
	_, ok := set[serverID]
	return ok
}

// TransitEntries are the marked nodes this user may clone as a personal entry.
func TransitEntries(d *sql.DB, u *User) ([]*Inbound, error) {
	if u == nil || u.PackageID == nil {
		return []*Inbound{}, nil
	}
	p, err := GetPackage(d, *u.PackageID)
	if err != nil {
		return []*Inbound{}, nil
	}
	out := make([]*Inbound, 0, len(p.TransitIDs))
	seen := map[int64]struct{}{}
	for _, id := range p.TransitIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		in, err := GetInbound(d, id)
		if err != nil || in == nil || !TransitEntryOK(in) {
			continue
		}
		out = append(out, in)
	}
	return out, nil
}

// SyncUserForwards stops personal forwards whose machine is no longer marked,
// and resumes ones that were stopped only because the mark was removed.
func SyncUserForwards(d *sql.DB, u *User) error {
	if u == nil || u.ID == 0 {
		return nil
	}
	allowed, err := transitServerSet(d, u)
	if err != nil {
		return err
	}
	owned, err := ListInboundsByOwner(d, u.ID)
	if err != nil {
		return err
	}
	for _, in := range owned {
		if in == nil {
			continue
		}
		_, ok := allowed[in.ServerID]
		if ok {
			if in.AuthHold {
				if err := SetInboundRunState(d, in.ID, true, false); err != nil {
					return err
				}
			}
			continue
		}
		if in.Enabled {
			if err := SetInboundRunState(d, in.ID, false, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func DeleteExpiredSessions(d *sql.DB) error {
	_, err := d.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now())
	return err
}
