package db

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

const (
	CardUsers    = "users"
	CardPackages = "packages"
	CardServers  = "servers"
	CardInbounds = "inbounds"
	maxCardOrder = 2000
)

// sort_order is display order for the card pages only. ListUsers, ListPackages,
// ListServers, and ListInbounds stay ORDER BY id so provisioning and config
// builds do not follow a dragged layout.

func cardTable(kind string) (string, error) {
	switch kind {
	case CardUsers, CardPackages, CardServers, CardInbounds:
		return kind, nil
	default:
		return "", fmt.Errorf("类型无效")
	}
}

// SpliceDisplayIDs rewrites the relative order of moved inside current.
// moved must be a permutation of a subset of current. Other ids keep their slots.
func SpliceDisplayIDs(current, moved []int64) ([]int64, error) {
	if len(moved) == 0 {
		return nil, fmt.Errorf("顺序为空")
	}
	if len(moved) > maxCardOrder {
		return nil, fmt.Errorf("一次最多调整 %d 个", maxCardOrder)
	}
	pos := make(map[int64]struct{}, len(current))
	for _, id := range current {
		pos[id] = struct{}{}
	}
	seen := make(map[int64]struct{}, len(moved))
	for _, id := range moved {
		if id <= 0 {
			return nil, fmt.Errorf("无效编号")
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("顺序里有重复")
		}
		seen[id] = struct{}{}
		if _, ok := pos[id]; !ok {
			return nil, fmt.Errorf("有不存在的条目")
		}
	}
	next := make([]int64, len(current))
	copy(next, current)
	k := 0
	for i, id := range current {
		if _, ok := seen[id]; ok {
			next[i] = moved[k]
			k++
		}
	}
	return next, nil
}

func displayIDs(d *sql.DB, kind string) ([]int64, error) {
	table, err := cardTable(kind)
	if err != nil {
		return nil, err
	}
	rows, err := d.Query(`SELECT id FROM ` + table + ` ORDER BY sort_order, id`)
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
	if ids == nil {
		ids = []int64{}
	}
	return ids, rows.Err()
}

func sameInboundServer(d *sql.DB, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	ph := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := d.Query(`SELECT id, server_id FROM inbounds WHERE id IN (`+ph+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := map[int64]int64{}
	for rows.Next() {
		var id, serverID int64
		if err := rows.Scan(&id, &serverID); err != nil {
			return err
		}
		found[id] = serverID
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var sid int64
	set := false
	for _, id := range ids {
		s, ok := found[id]
		if !ok {
			return fmt.Errorf("有不存在的条目")
		}
		if !set {
			sid = s
			set = true
			continue
		}
		if s != sid {
			return fmt.Errorf("只能在同一台实例里调整节点")
		}
	}
	return nil
}

// ReorderCards stores a new display order. ids is the new order of a subset
// (the cards on screen). Hidden rows keep their slots.
func ReorderCards(d *sql.DB, kind string, ids []int64) error {
	table, err := cardTable(kind)
	if err != nil {
		return err
	}
	current, err := displayIDs(d, kind)
	if err != nil {
		return err
	}
	next, err := SpliceDisplayIDs(current, ids)
	if err != nil {
		return err
	}
	if kind == CardInbounds {
		if err := sameInboundServer(d, ids); err != nil {
			return err
		}
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range next {
		if _, err := tx.Exec(`UPDATE `+table+` SET sort_order=? WHERE id=?`, int64(i+1), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ArrangeByDisplay sorts a card-page slice by sort_order. Callers that build
// config must keep using the id-ordered list helpers instead.
func ArrangeByDisplay[T any](d *sql.DB, kind string, items []T, idFn func(T) int64) error {
	if len(items) < 2 {
		return nil
	}
	ids, err := displayIDs(d, kind)
	if err != nil {
		return err
	}
	rank := make(map[int64]int, len(ids))
	for i, id := range ids {
		rank[id] = i
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := idFn(items[i]), idFn(items[j])
		ra, oka := rank[a]
		rb, okb := rank[b]
		if oka != okb {
			return oka
		}
		if ra != rb {
			return ra < rb
		}
		return a < b
	})
	return nil
}
