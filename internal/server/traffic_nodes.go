package server

import (
	"database/sql"

	"liking/internal/corecfg"
	"liking/internal/db"
)

// trafficNodesForUser lists rewritten entry lines for a customer's package.
// The landing node stays out unless the package also checks it.
func (s *Server) trafficNodesForUser(u *db.User, withURI bool) ([]facingNode, []hiddenNode) {
	if s == nil || u == nil || u.Role == "admin" || u.PackageID == nil {
		return nil, nil
	}
	clients, err := db.ListClientsByUser(s.DB, u.ID)
	if err != nil {
		return nil, nil
	}
	byClient := map[int64]*db.Client{}
	for _, c := range clients {
		if c != nil {
			byClient[c.InboundID] = c
		}
	}
	return s.trafficFacing(*u.PackageID, byClient, u.ID, withURI, true)
}

// trafficNodesForPackage is the admin preview of what a package puts in a subscription.
func (s *Server) trafficNodesForPackage(packageID int64) ([]facingNode, []hiddenNode) {
	if s == nil || packageID <= 0 {
		return nil, nil
	}
	return s.trafficFacing(packageID, nil, 0, false, false)
}

func (s *Server) trafficFacing(packageID int64, byClient map[int64]*db.Client, userID int64, withURI, needClient bool) ([]facingNode, []hiddenNode) {
	list, err := db.TrafficForwardsByPackage(s.DB, packageID)
	if err != nil {
		return nil, nil
	}
	over := s.serverOverMap()
	var totals map[int64]db.TrafficSum
	period := map[int64]db.TrafficSum{}
	if userID > 0 {
		totals, _ = db.UserInboundTrafficTotals(s.DB, userID)
		from, to := dayRangeDB(s.DB, 14)
		if rows, err := db.TrafficByInbound(s.DB, from, to, userID); err == nil {
			for _, t := range rows {
				period[t.ID] = db.TrafficSum{Up: t.Up, Down: t.Down}
			}
		}
	}
	var nodes []facingNode
	var hidden []hiddenNode
	for _, fw := range list {
		if fw == nil {
			continue
		}
		landing, _ := db.GetInbound(s.DB, fw.LandingInboundID)
		var c *db.Client
		if byClient != nil && landing != nil {
			c = byClient[landing.ID]
		}
		if reason := trafficHiddenReason(s.DB, fw, landing, c, over, needClient); reason != "" {
			name := fw.Name
			if name == "" && landing != nil {
				name = landing.Name
			}
			hidden = append(hidden, hiddenNode{
				ID:         -fw.ID,
				Name:       name,
				ServerName: fw.EntryServerName,
				Reason:     reason,
			})
			continue
		}
		share := trafficDisplayInbound(s.DB, fw, landing, over)
		if share == nil {
			continue
		}
		n := facingNode{
			ID:         -fw.ID,
			Name:       share.Name,
			Host:       corecfg.ShareHost(share),
			Port:       share.Port,
			Profile:    share.Profile,
			ServerName: fw.EntryServerName,
			ServerID:   fw.EntryServerID,
			LineKind:   "traffic",
		}
		if entry, err := db.GetServer(s.DB, fw.EntryServerID); err == nil && entry != nil {
			if entry.Name != "" {
				n.ServerName = entry.Name
			}
			n.ServerOnline = entry.Online
		}
		if totals != nil && landing != nil {
			if t, ok := totals[landing.ID]; ok {
				n.UsedUp = t.Up
				n.UsedDown = t.Down
			}
		}
		if landing != nil {
			if t, ok := period[landing.ID]; ok {
				n.PeriodUp = t.Up
				n.PeriodDown = t.Down
			}
		}
		if withURI && c != nil && c.Enabled {
			if uri, err := corecfg.ShareURI(share, c); err == nil {
				n.URI = uri
			}
			if share.Profile == corecfg.ProfileWireGuard {
				if conf, err := corecfg.WireGuardClientConf(share, c); err == nil {
					n.Conf = conf
				}
			}
		}
		nodes = append(nodes, n)
	}
	return nodes, hidden
}

func trafficHiddenReason(d *sql.DB, fw *db.TrafficForward, landing *db.Inbound, c *db.Client, over map[int64]bool, needClient bool) string {
	if fw == nil {
		return "流量中转不存在"
	}
	if !fw.Enabled {
		return "停用"
	}
	if landing == nil {
		return "落地节点不存在"
	}
	if reason := inboundSkipReason(d, landing, over); reason != "" {
		if reason == "skip" {
			return "不能进订阅"
		}
		return reason
	}
	if needClient && (c == nil || !c.Enabled) {
		return "还没有账号"
	}
	if fw.EntryHost == "" || fw.EntryPort < 1 {
		return "入口没有公网地址"
	}
	return ""
}
