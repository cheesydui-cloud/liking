package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// wireguardProfile matches corecfg.ProfileWireGuard. This package must not
// import corecfg (corecfg already imports db).
const wireguardProfile = "wireguard"

// InUseError is a refused delete. The row is unchanged.
type InUseError struct {
	Message string
}

func (e *InUseError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func IsInUse(err error) bool {
	var target *InUseError
	return errors.As(err, &target)
}

func inUse(msg string) error {
	return &InUseError{Message: msg}
}

type useHit struct {
	InboundID     int64
	InboundName   string
	ServerID      int64
	Profile       string
	ExitInboundID sql.NullInt64
	Settings      string
	PackageName   string
	Customers     int
}

type inboundRef struct {
	ID       int64
	Name     string
	ServerID int64
}

// PackageInUse reports a non-admin customer still bound to the package.
// Disabled, expired, and quota-exhausted customers still count. Admins do not.
// A missing package is not in use.
func PackageInUse(d *sql.DB, id int64) error {
	var name string
	err := d.QueryRow(`SELECT name FROM packages WHERE id=?`, id).Scan(&name)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	n, err := packageCustomers(d, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return inUse(fmt.Sprintf("还有 %d 个客户在用套餐「%s」，不能删除", n, name))
	}
	return nil
}

// InboundInUse reports that a package with customers includes this node,
// either by package_inbounds or by package_servers on its instance.
// A missing node is not in use.
func InboundInUse(d *sql.DB, id int64) error {
	var exists int
	err := d.QueryRow(`SELECT 1 FROM inbounds WHERE id=?`, id).Scan(&exists)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	protected, err := protectedInbounds(d)
	if err != nil {
		return err
	}
	h, ok := protected[id]
	if !ok {
		return nil
	}
	return inUse(fmt.Sprintf("节点「%s」还在套餐「%s」里，还有 %d 个客户在用，不能删除", h.InboundName, h.PackageName, h.Customers))
}

// ServerInUse reports that deleting the instance would remove a line customers
// still have: a protected node on it, a whole-instance package binding, a
// chain landing or hop used by a protected node, or a WireGuard home exit.
// A missing instance is not in use.
func ServerInUse(d *sql.DB, id int64) error {
	srv, err := GetServer(d, id)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	protected, err := protectedInbounds(d)
	if err != nil {
		return err
	}
	if h := busiestOnServer(protected, id); h != nil {
		return inUse(fmt.Sprintf("实例「%s」上的节点「%s」还在套餐「%s」里，还有 %d 个客户在用，不能删除", srv.Name, h.InboundName, h.PackageName, h.Customers))
	}
	pkgName, n, err := boundServerCustomers(d, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return inUse(fmt.Sprintf("实例「%s」绑在套餐「%s」上，还有 %d 个客户在用，不能删除", srv.Name, pkgName, n))
	}
	idx, err := inboundIndex(d)
	if err != nil {
		return err
	}
	if msg := chainServerUse(srv.Name, id, protected, idx); msg != "" {
		return inUse(msg)
	}
	if h := busiestExit(protected, id); h != nil {
		return inUse(fmt.Sprintf("实例「%s」是节点「%s」的家宽出口，还有 %d 个客户在用，不能删除", srv.Name, h.InboundName, h.Customers))
	}
	return nil
}

func packageCustomers(d *sql.DB, packageID int64) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM user_packages up
		JOIN users u ON u.id = up.user_id
		WHERE up.package_id=? AND u.role != 'admin'`, packageID).Scan(&n)
	return n, err
}

func protectedInbounds(d *sql.DB) (map[int64]useHit, error) {
	rows, err := d.Query(`SELECT i.id, i.name, i.server_id, i.profile, i.exit_inbound_id, i.settings, p.name, COUNT(DISTINCT u.id)
		FROM inbounds i
		JOIN packages p ON (
			EXISTS (SELECT 1 FROM package_inbounds pi WHERE pi.package_id = p.id AND pi.inbound_id = i.id)
			OR EXISTS (SELECT 1 FROM package_servers ps WHERE ps.package_id = p.id AND ps.server_id = i.server_id)
		)
		JOIN user_packages up ON up.package_id = p.id
		JOIN users u ON u.id = up.user_id AND u.role != 'admin'
		GROUP BY i.id, i.name, i.server_id, i.profile, i.exit_inbound_id, i.settings, p.id, p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	best := map[int64]useHit{}
	for rows.Next() {
		var h useHit
		if err := rows.Scan(&h.InboundID, &h.InboundName, &h.ServerID, &h.Profile, &h.ExitInboundID, &h.Settings, &h.PackageName, &h.Customers); err != nil {
			return nil, err
		}
		cur, ok := best[h.InboundID]
		if !ok || h.Customers > cur.Customers {
			best[h.InboundID] = h
		}
	}
	return best, rows.Err()
}

func busiestOnServer(protected map[int64]useHit, serverID int64) *useHit {
	var best *useHit
	for _, h := range protected {
		if h.ServerID != serverID {
			continue
		}
		if best == nil || h.Customers > best.Customers {
			cp := h
			best = &cp
		}
	}
	return best
}

func boundServerCustomers(d *sql.DB, serverID int64) (string, int, error) {
	var name string
	var n int
	err := d.QueryRow(`SELECT p.name, COUNT(DISTINCT u.id)
		FROM package_servers ps
		JOIN packages p ON p.id = ps.package_id
		JOIN user_packages up ON up.package_id = p.id
		JOIN users u ON u.id = up.user_id AND u.role != 'admin'
		WHERE ps.server_id = ?
		GROUP BY p.id, p.name
		ORDER BY COUNT(DISTINCT u.id) DESC, p.id
		LIMIT 1`, serverID).Scan(&name, &n)
	if err == sql.ErrNoRows {
		return "", 0, nil
	}
	return name, n, err
}

func inboundIndex(d *sql.DB) (map[int64]inboundRef, error) {
	rows, err := d.Query(`SELECT id, name, server_id FROM inbounds`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]inboundRef{}
	for rows.Next() {
		var r inboundRef
		if err := rows.Scan(&r.ID, &r.Name, &r.ServerID); err != nil {
			return nil, err
		}
		out[r.ID] = r
	}
	return out, rows.Err()
}

func chainServerUse(serverName string, serverID int64, protected map[int64]useHit, idx map[int64]inboundRef) string {
	var localName, entryName string
	customers := 0
	found := false
	for _, h := range protected {
		ids := hopAndExitIDs(h)
		for _, id := range ids {
			ref, ok := idx[id]
			if !ok || ref.ServerID != serverID {
				continue
			}
			if !found || h.Customers > customers {
				localName = ref.Name
				entryName = h.InboundName
				customers = h.Customers
				found = true
			}
		}
	}
	if !found {
		return ""
	}
	return fmt.Sprintf("实例「%s」上的节点「%s」被节点「%s」用作中转，还有 %d 个客户在用，不能删除", serverName, localName, entryName, customers)
}

func hopAndExitIDs(h useHit) []int64 {
	var ids []int64
	if h.ExitInboundID.Valid && h.ExitInboundID.Int64 > 0 {
		ids = append(ids, h.ExitInboundID.Int64)
	}
	ids = append(ids, hopInboundIDs(h.Settings)...)
	return ids
}

func busiestExit(protected map[int64]useHit, serverID int64) *useHit {
	var best *useHit
	for _, h := range protected {
		if h.Profile != wireguardProfile {
			continue
		}
		if settingsExitServerID(h.Settings) != serverID {
			continue
		}
		if best == nil || h.Customers > best.Customers {
			cp := h
			best = &cp
		}
	}
	return best
}

func hopInboundIDs(settings string) []int64 {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settings), &raw); err != nil {
		return nil
	}
	hopsRaw, ok := raw["hops"]
	if !ok {
		return nil
	}
	var hops []map[string]any
	if err := json.Unmarshal(hopsRaw, &hops); err != nil {
		return nil
	}
	var ids []int64
	for _, hop := range hops {
		if id := jsonInt64(hop["inbound_id"]); id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func settingsExitServerID(settings string) int64 {
	var raw map[string]any
	if err := json.Unmarshal([]byte(settings), &raw); err != nil {
		return 0
	}
	return jsonInt64(raw["exit_server_id"])
}

func jsonInt64(v any) int64 {
	switch t := v.(type) {
	case float64:
		if t <= 0 {
			return 0
		}
		return int64(t)
	case int:
		if t <= 0 {
			return 0
		}
		return int64(t)
	case int64:
		if t <= 0 {
			return 0
		}
		return t
	case json.Number:
		n, err := t.Int64()
		if err != nil || n <= 0 {
			return 0
		}
		return n
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		if err != nil || n <= 0 {
			return 0
		}
		return n
	default:
		return 0
	}
}
