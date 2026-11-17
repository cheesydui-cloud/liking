package server

import (
	"database/sql"
	"fmt"
	"strings"

	"liking/internal/corecfg"
	"liking/internal/db"
)

type subItem struct {
	in *db.Inbound
	c  *db.Client
}

func collectSubItems(d *sql.DB, u *db.User, clients []*db.Client, over map[int64]bool) ([]subItem, error) {
	hide, extras, err := trafficSubExtras(d, u, clients, over)
	if err != nil {
		return nil, err
	}
	used := map[string]struct{}{}
	var out []subItem
	add := func(in *db.Inbound, c *db.Client) {
		if in == nil || c == nil {
			return
		}
		name := uniqueSubName(in.Name, used)
		out = append(out, subItem{in: withSubName(in, name), c: c})
	}
	for _, c := range clients {
		if c == nil || !c.Enabled {
			continue
		}
		if _, skip := hide[c.InboundID]; skip {
			continue
		}
		in, err := db.GetInbound(d, c.InboundID)
		if err != nil {
			continue
		}
		if !inboundLive(d, in, over) {
			continue
		}
		add(in, c)
	}
	for _, ex := range extras {
		add(ex.in, ex.c)
	}
	applyUserSubNames(u, out)
	return out, nil
}

// applyUserSubNames 让订阅里的节点名显示用户名。只有一条时就是用户名；
// 多条时加上原来的节点名，避免客户端把两条收成同一个名字。
func applyUserSubNames(u *db.User, items []subItem) {
	if u == nil || len(items) == 0 {
		return
	}
	user := strings.TrimSpace(u.Username)
	if user == "" {
		return
	}
	multi := len(items) > 1
	used := map[string]struct{}{}
	for i := range items {
		if items[i].in == nil {
			continue
		}
		items[i].in = withSubName(items[i].in, uniqueSubName(subDisplayName(user, items[i].in.Name, multi), used))
	}
}

func subDisplayName(user, node string, multi bool) string {
	user = strings.TrimSpace(user)
	node = strings.TrimSpace(node)
	if user == "" {
		if node == "" {
			return "线路"
		}
		return node
	}
	if !multi || node == "" || node == user {
		return user
	}
	return user + " · " + node
}

func trafficSubExtras(d *sql.DB, u *db.User, clients []*db.Client, over map[int64]bool) (map[int64]struct{}, []subItem, error) {
	hide := map[int64]struct{}{}
	if u == nil || u.Role == "admin" || u.PackageID == nil {
		return hide, nil, nil
	}
	p, err := db.GetPackage(d, *u.PackageID)
	if err != nil || p == nil {
		return hide, nil, nil
	}
	direct, err := db.PackageInboundIDs(d, p)
	if err != nil {
		return nil, nil, err
	}
	directSet := map[int64]struct{}{}
	for _, id := range direct {
		directSet[id] = struct{}{}
	}
	all, err := db.AllLandingIDsForPackage(d, p.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, id := range all {
		if _, ok := directSet[id]; !ok {
			hide[id] = struct{}{}
		}
	}
	list, err := db.TrafficForwardsByPackage(d, p.ID)
	if err != nil {
		return nil, nil, err
	}
	byClient := map[int64]*db.Client{}
	for _, c := range clients {
		if c != nil {
			byClient[c.InboundID] = c
		}
	}
	var extras []subItem
	for _, fw := range list {
		if fw == nil || !fw.Enabled {
			continue
		}
		c := byClient[fw.LandingInboundID]
		landing, err := db.GetInbound(d, fw.LandingInboundID)
		if err != nil || landing == nil {
			continue
		}
		share := trafficShareInbound(d, fw, landing, c, over)
		if share == nil {
			continue
		}
		extras = append(extras, subItem{in: share, c: c})
	}
	return hide, extras, nil
}

func trafficShareInbound(d *sql.DB, fw *db.TrafficForward, landing *db.Inbound, c *db.Client, over map[int64]bool) *db.Inbound {
	if c == nil || !c.Enabled {
		return nil
	}
	return trafficDisplayInbound(d, fw, landing, over)
}

// trafficDisplayInbound is the share copy without a user account. Package
// preview uses it so the entry address shows before anyone is bound.
func trafficDisplayInbound(d *sql.DB, fw *db.TrafficForward, landing *db.Inbound, over map[int64]bool) *db.Inbound {
	if fw == nil || !fw.Enabled || landing == nil {
		return nil
	}
	if !inboundLive(d, landing, over) {
		return nil
	}
	host := fw.EntryHost
	if host == "" {
		srv, err := db.GetServer(d, fw.EntryServerID)
		if err != nil || srv == nil {
			return nil
		}
		host = srv.PublicHost
	}
	if host == "" || fw.EntryPort < 1 {
		return nil
	}
	return corecfg.RewriteForwardInbound(landing, fw, host)
}

func withSubName(in *db.Inbound, name string) *db.Inbound {
	if in == nil || in.Name == name {
		return in
	}
	cp := *in
	cp.Name = name
	return &cp
}

func uniqueSubName(name string, used map[string]struct{}) string {
	if name == "" {
		name = "线路"
	}
	if _, ok := used[name]; !ok {
		used[name] = struct{}{}
		return name
	}
	for i := 2; ; i++ {
		next := fmt.Sprintf("%s %d", name, i)
		if _, ok := used[next]; !ok {
			used[next] = struct{}{}
			return next
		}
	}
}
