package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// TrafficHop is one intermediate machine. Position starts at 1.
// DialHost is the address the previous machine uses. Empty means the
// instance public address.
type TrafficHop struct {
	Position   int    `json:"position"`
	ServerID   int64  `json:"server_id"`
	ListenPort int    `json:"listen_port"`
	DialHost   string `json:"dial_host"`
	ServerName string `json:"server_name,omitempty"`
}

// TrafficForward moves bytes across instances. The entry and hops do not
// get a node. The protocol stays on the landing inbound.
type TrafficForward struct {
	ID               int64        `json:"id"`
	Name             string       `json:"name"`
	Enabled          bool         `json:"enabled"`
	EntryServerID    int64        `json:"entry_server_id"`
	EntryPort        int          `json:"entry_port"`
	LandingInboundID int64        `json:"landing_inbound_id"`
	LandingDialHost  string       `json:"landing_dial_host"`
	CreatedAt        int64        `json:"created_at"`
	Hops             []TrafficHop `json:"hops"`
	EntryServerName  string       `json:"entry_server_name,omitempty"`
	EntryHost        string       `json:"entry_host,omitempty"`
	LandingName      string       `json:"landing_name,omitempty"`
	LandingProfile   string       `json:"landing_profile,omitempty"`
	LandingPort      int          `json:"landing_port,omitempty"`
	LandingServerID  int64        `json:"landing_server_id,omitempty"`
}

func ListTrafficForwards(d *sql.DB) ([]*TrafficForward, error) {
	rows, err := d.Query(`SELECT id FROM traffic_forwards ORDER BY id`)
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
	out := make([]*TrafficForward, 0, len(ids))
	for _, id := range ids {
		fw, err := GetTrafficForward(d, id)
		if err != nil {
			return nil, err
		}
		out = append(out, fw)
	}
	return out, nil
}

func GetTrafficForward(d *sql.DB, id int64) (*TrafficForward, error) {
	fw := &TrafficForward{}
	var enabled int
	err := d.QueryRow(`SELECT id, name, enabled, entry_server_id, entry_port, landing_inbound_id, landing_dial_host, created_at
		FROM traffic_forwards WHERE id=?`, id).
		Scan(&fw.ID, &fw.Name, &enabled, &fw.EntryServerID, &fw.EntryPort, &fw.LandingInboundID, &fw.LandingDialHost, &fw.CreatedAt)
	if err != nil {
		return nil, err
	}
	fw.Enabled = enabled != 0
	hops, err := listTrafficHops(d, id)
	if err != nil {
		return nil, err
	}
	fw.Hops = hops
	if srv, err := GetServer(d, fw.EntryServerID); err == nil && srv != nil {
		fw.EntryServerName = srv.Name
		fw.EntryHost = strings.TrimSpace(srv.PublicHost)
	}
	if in, err := GetInbound(d, fw.LandingInboundID); err == nil && in != nil {
		fw.LandingName = in.Name
		fw.LandingProfile = in.Profile
		fw.LandingPort = in.Port
		fw.LandingServerID = in.ServerID
	}
	for i := range fw.Hops {
		if srv, err := GetServer(d, fw.Hops[i].ServerID); err == nil && srv != nil {
			fw.Hops[i].ServerName = srv.Name
		}
	}
	return fw, nil
}

func listTrafficHops(d *sql.DB, id int64) ([]TrafficHop, error) {
	rows, err := d.Query(`SELECT position, server_id, listen_port, dial_host FROM traffic_forward_hops WHERE forward_id=? ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hops []TrafficHop
	for rows.Next() {
		var h TrafficHop
		if err := rows.Scan(&h.Position, &h.ServerID, &h.ListenPort, &h.DialHost); err != nil {
			return nil, err
		}
		hops = append(hops, h)
	}
	if hops == nil {
		hops = []TrafficHop{}
	}
	return hops, rows.Err()
}

func InsertTrafficForward(d *sql.DB, fw *TrafficForward) error {
	if fw == nil {
		return fmt.Errorf("缺少转发")
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	enabled := 0
	if fw.Enabled {
		enabled = 1
	}
	if fw.CreatedAt == 0 {
		fw.CreatedAt = now()
	}
	res, err := tx.Exec(`INSERT INTO traffic_forwards(name, enabled, entry_server_id, entry_port, landing_inbound_id, landing_dial_host, created_at)
		VALUES(?,?,?,?,?,?,?)`, strings.TrimSpace(fw.Name), enabled, fw.EntryServerID, fw.EntryPort, fw.LandingInboundID, strings.TrimSpace(fw.LandingDialHost), fw.CreatedAt)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if err := insertTrafficHops(tx, id, fw.Hops); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	fw.ID = id
	return nil
}

func UpdateTrafficForward(d *sql.DB, fw *TrafficForward) error {
	if fw == nil || fw.ID == 0 {
		return fmt.Errorf("缺少转发")
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	enabled := 0
	if fw.Enabled {
		enabled = 1
	}
	res, err := tx.Exec(`UPDATE traffic_forwards SET name=?, enabled=?, entry_server_id=?, entry_port=?, landing_inbound_id=?, landing_dial_host=? WHERE id=?`,
		strings.TrimSpace(fw.Name), enabled, fw.EntryServerID, fw.EntryPort, fw.LandingInboundID, strings.TrimSpace(fw.LandingDialHost), fw.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.Exec(`DELETE FROM traffic_forward_hops WHERE forward_id=?`, fw.ID); err != nil {
		return err
	}
	if err := insertTrafficHops(tx, fw.ID, fw.Hops); err != nil {
		return err
	}
	return tx.Commit()
}

func insertTrafficHops(tx *sql.Tx, id int64, hops []TrafficHop) error {
	for i, h := range hops {
		pos := h.Position
		if pos == 0 {
			pos = i + 1
		}
		if _, err := tx.Exec(`INSERT INTO traffic_forward_hops(forward_id, position, server_id, listen_port, dial_host) VALUES(?,?,?,?,?)`,
			id, pos, h.ServerID, h.ListenPort, strings.TrimSpace(h.DialHost)); err != nil {
			return err
		}
	}
	return nil
}

func DeleteTrafficForward(d *sql.DB, id int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM package_forwards WHERE forward_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM traffic_forward_hops WHERE forward_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM traffic_forwards WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func TrafficForwardsByPackage(d *sql.DB, packageID int64) ([]*TrafficForward, error) {
	rows, err := d.Query(`SELECT forward_id FROM package_forwards WHERE package_id=? ORDER BY forward_id`, packageID)
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
	out := make([]*TrafficForward, 0, len(ids))
	for _, id := range ids {
		fw, err := GetTrafficForward(d, id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return nil, err
		}
		out = append(out, fw)
	}
	return out, nil
}

func SetPackageForwards(d *sql.DB, packageID int64, ids []int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setPackageForwardsTx(tx, packageID, ids); err != nil {
		return err
	}
	return tx.Commit()
}

func setPackageForwardsTx(tx *sql.Tx, packageID int64, ids []int64) error {
	if _, err := tx.Exec(`DELETE FROM package_forwards WHERE package_id=?`, packageID); err != nil {
		return err
	}
	seen := map[int64]struct{}{}
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		var exists int
		if err := tx.QueryRow(`SELECT 1 FROM traffic_forwards WHERE id=?`, id).Scan(&exists); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("流量中转不存在")
			}
			return err
		}
		if _, err := tx.Exec(`INSERT INTO package_forwards(package_id, forward_id) VALUES(?,?)`, packageID, id); err != nil {
			return err
		}
	}
	return nil
}

func PackageForwardIDs(d *sql.DB, packageID int64) ([]int64, error) {
	return queryIDs(d, `SELECT forward_id FROM package_forwards WHERE package_id=? ORDER BY forward_id`, packageID)
}

// LandingIDsForPackage returns landing nodes of the package's enabled forwards.
func LandingIDsForPackage(d *sql.DB, packageID int64) ([]int64, error) {
	return queryIDs(d, `SELECT DISTINCT f.landing_inbound_id
		FROM package_forwards pf
		JOIN traffic_forwards f ON f.id = pf.forward_id
		WHERE pf.package_id=? AND f.enabled != 0
		ORDER BY f.landing_inbound_id`, packageID)
}

// AllLandingIDsForPackage includes disabled forwards so a pause does not
// throw away the landing account.
func AllLandingIDsForPackage(d *sql.DB, packageID int64) ([]int64, error) {
	return queryIDs(d, `SELECT DISTINCT f.landing_inbound_id
		FROM package_forwards pf
		JOIN traffic_forwards f ON f.id = pf.forward_id
		WHERE pf.package_id=?
		ORDER BY f.landing_inbound_id`, packageID)
}

// AllLandingIDsForUser is empty for admins. Their nodes do not come from a package.
func AllLandingIDsForUser(d *sql.DB, u *User) ([]int64, error) {
	if u == nil || u.Role == "admin" || u.PackageID == nil {
		return nil, nil
	}
	return AllLandingIDsForPackage(d, *u.PackageID)
}

// ForwardTouchServers returns machines that run forward rules for any path
// that uses serverID as entry, hop, or landing. A public-address change has
// to be pushed to the previous hop.
func ForwardTouchServers(d *sql.DB, serverID int64) ([]int64, error) {
	rows, err := d.Query(`SELECT f.id FROM traffic_forwards f
		LEFT JOIN traffic_forward_hops h ON h.forward_id = f.id
		LEFT JOIN inbounds i ON i.id = f.landing_inbound_id
		WHERE f.entry_server_id=? OR h.server_id=? OR i.server_id=?
		GROUP BY f.id`, serverID, serverID, serverID)
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
	seen := map[int64]struct{}{}
	var out []int64
	add := func(id int64) {
		if id == 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range ids {
		fw, err := GetTrafficForward(d, id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return nil, err
		}
		add(fw.EntryServerID)
		for _, h := range fw.Hops {
			add(h.ServerID)
		}
	}
	return out, nil
}

// ListenPortsUsed is inbound ports plus traffic-forward listen ports on one machine.
func ListenPortsUsed(d *sql.DB, serverID, excludeInboundID, excludeForwardID int64) (map[int]struct{}, error) {
	used, err := UsedPortsOnServer(d, serverID, excludeInboundID)
	if err != nil {
		return nil, err
	}
	if used == nil {
		used = map[int]struct{}{}
	}
	if err := addForwardListenPorts(d, serverID, excludeForwardID, used); err != nil {
		return nil, err
	}
	return used, nil
}

func addForwardListenPorts(d *sql.DB, serverID, excludeForwardID int64, used map[int]struct{}) error {
	rows, err := d.Query(`SELECT id, entry_port FROM traffic_forwards WHERE entry_server_id=?`, serverID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var port int
		if err := rows.Scan(&id, &port); err != nil {
			return err
		}
		if id == excludeForwardID {
			continue
		}
		used[port] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	hops, err := d.Query(`SELECT forward_id, listen_port FROM traffic_forward_hops WHERE server_id=?`, serverID)
	if err != nil {
		return err
	}
	defer hops.Close()
	for hops.Next() {
		var id int64
		var port int
		if err := hops.Scan(&id, &port); err != nil {
			return err
		}
		if id == excludeForwardID {
			continue
		}
		used[port] = struct{}{}
	}
	return hops.Err()
}

func ServerIDsForTrafficForward(fw *TrafficForward) []int64 {
	if fw == nil {
		return nil
	}
	ids := []int64{fw.EntryServerID}
	for _, h := range fw.Hops {
		ids = append(ids, h.ServerID)
	}
	if fw.LandingServerID != 0 {
		ids = append(ids, fw.LandingServerID)
	}
	return ids
}
