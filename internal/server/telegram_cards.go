package server

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"liking/internal/corecfg"
	"liking/internal/db"
	"liking/internal/version"
)

const tgTextLimit = 3900

func (s *Server) tgHandleText(chatID int64, cmd, arg, raw string) {
	switch cmd {
	case "/start", "/help":
		if cmd == "/start" && arg != "" {
			s.tgApplyCode(chatID, arg)
			return
		}
		s.tgOpen(chatID, "home", 0)
	case "/status":
		if !s.tgIsAdmin(chatID) {
			s.tgReply(chatID, "这个对话还不是管理员。"+tgOpenHint)
			return
		}
		s.tgOpen(chatID, "servers", 0)
	case "/user":
		if !s.tgIsAdmin(chatID) {
			s.tgReply(chatID, "这个对话还不是管理员。"+tgOpenHint)
			return
		}
		if arg == "" {
			s.tgOpen(chatID, "ask", 0)
			return
		}
		s.tgLookupName(chatID, arg)
	case "/bind":
		if arg == "" {
			s.tgReply(chatID, tgOpenHint+"打不开时发送 /bind 和 6 位绑定码。")
			return
		}
		s.tgApplyCode(chatID, arg)
	case "/me":
		if _, ok := s.tgBoundUser(chatID); !ok {
			s.tgReply(chatID, "还没有绑定")
			return
		}
		s.tgOpen(chatID, "home", 0)
	case "/sub":
		if _, ok := s.tgBoundUser(chatID); !ok {
			s.tgReply(chatID, "还没有绑定")
			return
		}
		s.tgOpen(chatID, "sub", 0)
	case "/unbind":
		s.tgCmdUnbind(chatID)
	default:
		if tgDigits(raw) {
			s.tgApplyCode(chatID, raw)
			return
		}
		if act := tgButtonAction(raw); act != "" {
			s.tgOpen(chatID, act, 0)
			return
		}
		if strings.HasPrefix(cmd, "/") {
			s.tgReply(chatID, "不认识这个命令。发送 /help 查看可用命令。")
			return
		}
		if s.tgIsAdmin(chatID) {
			s.tgLookupName(chatID, raw)
			return
		}
		if _, ok := s.tgBoundUser(chatID); ok {
			if _, err := db.GetUserByName(s.DB, strings.TrimSpace(raw)); err == nil {
				s.tgReply(chatID, "没有权限")
				return
			}
			s.tgReply(chatID, "点底部按钮，或发送 /help。")
			return
		}
		s.tgReply(chatID, tgOpenHint)
	}
}

func tgButtonAction(text string) string {
	switch strings.TrimSpace(text) {
	case "首页":
		return "home"
	case "节点":
		return "nodes"
	case "订阅":
		return "sub"
	case "近7日":
		return "week"
	case "总览":
		return "overview"
	case "机器":
		return "servers"
	case "到期":
		return "due"
	case "流量":
		return "quota"
	case "查用户":
		return "ask"
	default:
		return ""
	}
}

func (s *Server) tgRouteCallback(chatID, msgID int64, data string) {
	switch {
	case data == "sub" || strings.HasPrefix(data, "sub:"):
		s.tgSubAction(chatID, msgID, data)
	case strings.HasPrefix(data, "user:"):
		s.tgUserAction(chatID, msgID, data)
	case strings.HasPrefix(data, "srv:"):
		s.tgServerAction(chatID, msgID, data)
	case strings.HasPrefix(data, "nlink:"):
		s.tgNodeLinkAction(chatID, msgID, data)
	default:
		if tgButtonAction(data) == "" && data != "home" && data != "nodes" && data != "week" && data != "overview" && data != "servers" && data != "due" && data != "quota" && data != "ask" && data != "sub" {
			s.tgReply(chatID, "不认识这个按钮")
			return
		}
		s.tgOpen(chatID, data, msgID)
	}
}

func (s *Server) tgOpen(chatID int64, action string, editID int64) {
	card, deny := s.tgBuild(chatID, action)
	if deny != "" {
		if editID > 0 {
			_ = s.tgEdit(chatID, editID, deny, nil)
			return
		}
		s.tgReply(chatID, deny)
		return
	}
	if editID > 0 {
		markup := card.Inline
		if markup == nil {
			markup = s.tgInlineNav(chatID)
		}
		s.tgEditOrSend(chatID, editID, card.Text, markup)
		return
	}
	_ = s.tgSend(chatID, card.Text, card.sendMarkup(chatID, s))
}

type tgCard struct {
	Text   string
	Inline *tgMarkup
	Reply  bool
}

func (c tgCard) sendMarkup(chatID int64, s *Server) any {
	if c.Inline != nil {
		return c.Inline
	}
	if c.Reply {
		return s.tgReplyKeyboard(chatID)
	}
	return nil
}

func (s *Server) tgBuild(chatID int64, action string) (tgCard, string) {
	admin := s.tgIsAdmin(chatID)
	user, bound := s.tgBoundUser(chatID)
	switch action {
	case "home":
		if bound {
			return tgCard{Text: s.tgAccountText(user), Reply: true}, ""
		}
		if admin {
			return tgCard{Text: "已连接\n" + s.tgOverviewText(), Reply: true}, ""
		}
		return tgCard{}, tgOpenHint
	case "overview":
		if !admin {
			return tgCard{}, "没有权限"
		}
		return tgCard{Text: s.tgOverviewText(), Reply: true}, ""
	case "nodes":
		if !bound {
			return tgCard{}, "没有权限"
		}
		return s.tgNodesCard(user), ""
	case "week":
		if !bound {
			return tgCard{}, "没有权限"
		}
		return tgCard{Text: s.tgWeekText(user), Inline: s.tgInlineNav(chatID)}, ""
	case "sub":
		if !bound {
			return tgCard{}, "还没有绑定"
		}
		return tgCard{Text: tgSubMenuText, Inline: tgSubMenuMarkup()}, ""
	case "servers":
		if !admin {
			return tgCard{}, "没有权限"
		}
		return s.tgServersCard(), ""
	case "due":
		if !admin {
			return tgCard{}, "没有权限"
		}
		return s.tgDueCard(), ""
	case "quota":
		if !admin {
			return tgCard{}, "没有权限"
		}
		return s.tgQuotaCard(), ""
	case "ask":
		if !admin {
			return tgCard{}, "没有权限"
		}
		return tgCard{Text: "发送用户名即可查询。", Inline: s.tgInlineNav(chatID)}, ""
	default:
		return tgCard{}, "不认识这个按钮"
	}
}

func (s *Server) tgLookupName(chatID int64, name string) {
	if !s.tgIsAdmin(chatID) {
		s.tgReply(chatID, "没有权限")
		return
	}
	name = strings.TrimSpace(name)
	u, err := db.GetUserByName(s.DB, name)
	if err != nil || u == nil || u.Role == "admin" {
		s.tgReply(chatID, "没有这个用户")
		return
	}
	s.tgSend(chatID, s.tgAccountText(u), s.tgAccountMarkup(chatID))
}

func (s *Server) tgUserAction(chatID, msgID int64, data string) {
	id, err := strconv.ParseInt(strings.TrimPrefix(data, "user:"), 10, 64)
	if err != nil || id <= 0 {
		s.tgReply(chatID, "不认识这个按钮")
		return
	}
	if !s.tgIsAdmin(chatID) {
		u, ok := s.tgBoundUser(chatID)
		if !ok || u.ID != id {
			s.tgReply(chatID, "没有权限")
			return
		}
	}
	u, err := db.GetUser(s.DB, id)
	if err != nil || u == nil || u.Role == "admin" {
		s.tgReply(chatID, "没有这个用户")
		return
	}
	s.tgEditOrSend(chatID, msgID, s.tgAccountText(u), s.tgAccountMarkup(chatID))
}

func (s *Server) tgServerAction(chatID, msgID int64, data string) {
	if !s.tgIsAdmin(chatID) {
		s.tgReply(chatID, "没有权限")
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(data, "srv:"), 10, 64)
	if err != nil || id <= 0 {
		s.tgReply(chatID, "不认识这个按钮")
		return
	}
	srv, err := db.GetServer(s.DB, id)
	if err != nil || srv == nil {
		s.tgReply(chatID, "没有这台机器")
		return
	}
	list := []*db.Server{srv}
	s.decorateServers(list)
	text := s.tgServerDetail(list[0])
	markup := &tgMarkup{Keyboard: [][]tgButton{
		{{Text: "刷新", Data: "srv:" + strconv.FormatInt(id, 10)}},
		{{Text: "回总览", Data: "overview"}},
	}}
	s.tgEditOrSend(chatID, msgID, text, markup)
}

func (s *Server) tgSubAction(chatID, msgID int64, data string) {
	u, ok := s.tgBoundUser(chatID)
	if !ok {
		s.tgReply(chatID, "还没有绑定")
		return
	}
	if data == "sub:hide" || data == "sub:menu" {
		s.tgEditOrSend(chatID, msgID, tgSubMenuText, tgSubMenuMarkup())
		return
	}
	kind := ""
	switch data {
	case "sub", "sub:uri":
		kind = ""
	case "sub:rocket":
		kind = "uri"
	case "sub:clash":
		kind = "clash"
	case "sub:box":
		kind = "singbox"
	default:
		s.tgReply(chatID, "不认识这个按钮")
		return
	}
	link, err := s.tgSubLink(u)
	if err != nil {
		s.tgReply(chatID, err.Error())
		return
	}
	if kind != "" {
		link += "/" + kind
	}
	text := "订阅链接：\n" + link
	markup := &tgMarkup{Keyboard: [][]tgButton{{{Text: "隐藏", Data: "sub:hide"}}}}
	if msgID > 0 {
		if err := s.tgEdit(chatID, msgID, text, markup); err == nil || tgSameMessage(err) {
			db.AddAudit(s.DB, &u.ID, "tg.sub", u.Username)
			return
		}
	}
	_ = s.tgSend(chatID, text, markup)
	db.AddAudit(s.DB, &u.ID, "tg.sub", u.Username)
}

func (s *Server) tgNodeLinkAction(chatID, msgID int64, data string) {
	u, ok := s.tgBoundUser(chatID)
	if !ok {
		s.tgReply(chatID, "没有权限")
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(data, "nlink:"), 10, 64)
	if err != nil || id == 0 {
		s.tgReply(chatID, "不认识这个按钮")
		return
	}
	nodes, _ := s.tgUserNodes(u, true)
	var found *facingNode
	for i := range nodes {
		if nodes[i].ID == id {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		s.tgReply(chatID, "没有这条节点")
		return
	}
	body := strings.TrimSpace(found.URI)
	if body == "" {
		body = strings.TrimSpace(found.Conf)
	}
	if body == "" {
		s.tgReply(chatID, "这条节点还没有链接")
		return
	}
	text := found.Name + "\n" + body
	markup := &tgMarkup{Keyboard: [][]tgButton{{{Text: "返回节点", Data: "nodes"}}}}
	s.tgEditOrSend(chatID, msgID, text, markup)
}

func (s *Server) tgAccountText(u *db.User) string {
	if u == nil {
		return "没有这个用户"
	}
	var b strings.Builder
	b.WriteString(u.Username)
	if remark := strings.TrimSpace(u.Remark); remark != "" {
		b.WriteString("\n备注 " + remark)
	}
	b.WriteString("\n" + tgPackageLine(u))
	b.WriteString("\n" + s.tgExpiryLine(u))
	b.WriteString("\n" + tgTrafficLine(u))
	live, hidden := s.tgUserNodes(u, false)
	fmt.Fprintf(&b, "\n节点 %d 条可用，%d 条未进订阅", len(live), len(hidden))
	return b.String()
}

func tgPackageLine(u *db.User) string {
	name := strings.TrimSpace(u.PackageName)
	if name == "" {
		name = "未绑定套餐"
	}
	switch u.Direction {
	case "oneway":
		name += " · 单向"
	case "twoway":
		name += " · 双向"
	}
	return "套餐 " + name
}

func (s *Server) tgExpiryLine(u *db.User) string {
	exp := soonerExpiry(u.ExpiresAt, u.PkgExpires)
	if exp <= 0 {
		return "到期 不限期"
	}
	now := db.ClockNow(s.DB)
	date := time.Unix(exp, 0).In(now.Location()).Format("2006-01-02")
	delta := dayDelta(now, exp)
	switch {
	case delta > 0:
		return fmt.Sprintf("到期 %s · 还剩 %d 天", date, delta)
	case delta == 0:
		return "到期 " + date + " · 今天到期"
	default:
		return fmt.Sprintf("到期 %s · 已到期 %d 天", date, -delta)
	}
}

func tgTrafficLine(u *db.User) string {
	used := tgBytes(u.BilledBytes)
	if u.TrafficCap <= 0 {
		return "流量 " + used + "，不限额度"
	}
	return fmt.Sprintf("流量 %s / %s · %d%%", used, tgBytes(u.TrafficCap), u.QuotaRatio)
}

func (s *Server) tgUserNodes(u *db.User, withURI bool) ([]facingNode, []hiddenNode) {
	if u == nil {
		return nil, nil
	}
	ids, err := db.InboundIDsForUser(s.DB, u)
	if err != nil {
		return nil, nil
	}
	nodes, hidden := s.listFacingNodes(ids, u, withURI, nil)
	extra, extraHidden := s.trafficNodesForUser(u, withURI)
	return append(nodes, extra...), append(hidden, extraHidden...)
}

func (s *Server) tgNodesCard(u *db.User) tgCard {
	live, hidden := s.tgUserNodes(u, false)
	var b strings.Builder
	b.WriteString("可用")
	if len(live) == 0 {
		b.WriteString("\n没有可用线路")
	}
	for _, n := range live {
		fmt.Fprintf(&b, "\n· %s · %s · %s\n  %s", tgNodeTitle(n), tgProfileLabel(n.Profile), tgOnlineWord(n.ServerOnline), tgHostPort(n))
	}
	if len(hidden) > 0 {
		b.WriteString("\n\n未进订阅")
		for _, n := range hidden {
			name := strings.TrimSpace(n.Name)
			if name == "" {
				name = "线路"
			}
			fmt.Fprintf(&b, "\n· %s · %s", name, n.Reason)
		}
	}
	var rows [][]tgButton
	n := 0
	for _, node := range live {
		if n >= 8 {
			break
		}
		label := "这条链接"
		if len(live) > 1 {
			label = tgShort(tgNodeTitle(node), 10) + " · 这条链接"
		}
		rows = append(rows, []tgButton{{Text: label, Data: "nlink:" + strconv.FormatInt(node.ID, 10)}})
		n++
	}
	rows = append(rows, []tgButton{{Text: "刷新", Data: "nodes"}, {Text: "首页", Data: "home"}})
	return tgCard{Text: b.String(), Inline: &tgMarkup{Keyboard: rows}}
}

func tgNodeTitle(n facingNode) string {
	name := strings.TrimSpace(n.Name)
	if name == "" {
		name = "线路"
	}
	return name
}

func tgHostPort(n facingNode) string {
	host := strings.TrimSpace(n.Host)
	if host == "" {
		host = "未填地址"
	}
	if n.Port > 0 {
		return host + ":" + strconv.Itoa(n.Port)
	}
	return host
}

func tgOnlineWord(online int) string {
	if online == 1 {
		return "在线"
	}
	return "离线"
}

func tgProfileLabel(profile string) string {
	switch profile {
	case corecfg.ProfileMieru:
		return "Mieru"
	case corecfg.ProfileHysteria2:
		return "Hysteria2"
	case corecfg.ProfileWireGuard:
		return "WireGuard"
	case corecfg.ProfileSOCKS5:
		return "SOCKS5"
	case corecfg.ProfileSS2022:
		return "SS2022"
	case corecfg.ProfileAnyTLS:
		return "AnyTLS"
	case corecfg.ProfileTrojanTLS:
		return "Trojan"
	case corecfg.ProfileVLESSReality, corecfg.ProfileVLESSRealityVision, corecfg.ProfileVLESSXHTTP, corecfg.ProfileVLESSXHTTPReality:
		return "VLESS"
	default:
		if strings.TrimSpace(profile) == "" {
			return "节点"
		}
		return profile
	}
}

func (s *Server) tgWeekText(u *db.User) string {
	if u == nil {
		return "还没有绑定"
	}
	from, to := dayRangeDB(s.DB, 7)
	days, err := db.TrafficSeries(s.DB, from, to, u.ID, 0)
	if err != nil {
		return "近 7 日查询失败"
	}
	filled := db.FillTrafficDays(from, to, days)
	now := db.ClockNow(s.DB)
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	var b strings.Builder
	b.WriteString("近 7 日")
	var up, down int64
	for _, p := range filled {
		up += p.Up
		down += p.Down
		label := p.Day
		if len(label) >= 10 {
			label = label[5:10]
		}
		switch p.Day {
		case today:
			label = "今天"
		case yesterday:
			label = "昨天"
		}
		fmt.Fprintf(&b, "\n%s %s", label, tgBytes(p.Up+p.Down))
	}
	fmt.Fprintf(&b, "\n合计 %s · 上行 %s · 下行 %s", tgBytes(up+down), tgBytes(up), tgBytes(down))
	return b.String()
}

func (s *Server) tgOverviewText() string {
	list, err := db.ListServers(s.DB)
	online, total := 0, 0
	if err == nil {
		s.reconcileOnline(list)
		total = len(list)
		for _, x := range list {
			if x != nil && x.Online == 1 {
				online++
			}
		}
	}
	due, quota := 0, 0
	now := db.ClockNow(s.DB)
	if users, err := db.ListUsers(s.DB); err == nil {
		for _, u := range users {
			if !tgCustomer(u) {
				continue
			}
			exp := soonerExpiry(u.ExpiresAt, u.PkgExpires)
			if exp > 0 {
				d := dayDelta(now, exp)
				if d >= 0 && d <= tgExpireSoonDay {
					due++
				}
			}
			if u.TrafficCap > 0 && u.QuotaRatio >= 80 {
				quota++
			}
		}
	}
	certN := 0
	soon := time.Now().Unix() + int64(tgCertSoon/time.Second)
	if certs, err := db.ListCerts(s.DB); err == nil {
		for _, c := range certs {
			if c != nil && c.ExpiresAt > 0 && c.ExpiresAt <= soon {
				certN++
			}
		}
	}
	return fmt.Sprintf("在线 %d / %d\n3 天内到期 %d\n流量达到 80%% %d\n30 天内证书 %d\n命令：/status /user 用户名", online, total, due, quota, certN)
}

func (s *Server) tgServersCard() tgCard {
	list, err := db.ListServers(s.DB)
	if err != nil {
		return tgCard{Text: "查询失败", Inline: &tgMarkup{Keyboard: [][]tgButton{{{Text: "回总览", Data: "overview"}}}}}
	}
	s.decorateServers(list)
	online := 0
	var b strings.Builder
	var rows [][]tgButton
	shown := 0
	for _, x := range list {
		if x == nil {
			continue
		}
		if x.Online == 1 {
			online++
		}
		if shown >= 30 {
			continue
		}
		line := "· " + x.Name + " · " + tgOnlineWord(x.Online)
		if x.Online != 1 && x.LastSeen > 0 {
			line += " · " + tgAgo(time.Now().Unix(), x.LastSeen)
		}
		if x.AgentVer != "" && x.AgentVer != version.Version {
			line += " · 需升级"
		}
		b.WriteString("\n" + line)
		if shown < 20 {
			rows = append(rows, []tgButton{{Text: tgShort(x.Name, 20), Data: "srv:" + strconv.FormatInt(x.ID, 10)}})
		}
		shown++
	}
	head := fmt.Sprintf("在线 %d / %d", online, len(list))
	if shown == 0 {
		head += "\n还没有机器"
	}
	if len(list) > shown {
		head += fmt.Sprintf("\n还有 %d 台", len(list)-shown)
	}
	rows = append(rows, []tgButton{{Text: "回总览", Data: "overview"}})
	return tgCard{Text: head + b.String(), Inline: &tgMarkup{Keyboard: rows}}
}

func (s *Server) tgServerDetail(x *db.Server) string {
	if x == nil {
		return "没有这台机器"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s", x.Name, tgOnlineWord(x.Online))
	if x.LastSeen > 0 {
		b.WriteString("\n上次在线 " + tgAgo(time.Now().Unix(), x.LastSeen))
	} else {
		b.WriteString("\n上次在线 还没有上线")
	}
	host := strings.TrimSpace(x.PublicHost)
	if host == "" {
		host = "未填地址"
	}
	b.WriteString("\n地址 " + host)
	if ver := strings.TrimSpace(x.AgentVer); ver != "" {
		line := "版本 " + ver
		if ver != version.Version {
			line += " · 需升级"
		}
		b.WriteString("\n" + line)
	}
	up, down := s.tgServerToday(x.ID)
	b.WriteString("\n今日流量 " + tgBytes(up+down))
	if msg := strings.TrimSpace(x.LastError); msg != "" {
		b.WriteString("\n最后错误 " + tgShort(msg, 120))
	}
	return b.String()
}

func (s *Server) tgServerToday(id int64) (int64, int64) {
	day := db.ClockNow(s.DB).Format("2006-01-02")
	var up, down int64
	err := s.DB.QueryRow(`SELECT COALESCE(SUM(t.up),0), COALESCE(SUM(t.down),0) FROM traffic_daily t JOIN inbounds i ON i.id=t.inbound_id WHERE i.server_id=? AND t.day=?`, id, day).Scan(&up, &down)
	if err != nil {
		return 0, 0
	}
	return up, down
}

func (s *Server) tgServerAlert(x *db.Server, up bool) (string, *tgMarkup) {
	lead := x.Name + " 离线"
	if up {
		lead = x.Name + " 已恢复在线"
	}
	text := lead + "\n" + s.tgServerDetail(x)
	markup := &tgMarkup{Keyboard: [][]tgButton{{{Text: "查看机器", Data: "srv:" + strconv.FormatInt(x.ID, 10)}}}}
	return text, markup
}

func (s *Server) tgDueCard() tgCard {
	return s.tgUserListCard("3 天内到期", "3 天内没有到期的用户", func(u *db.User, now time.Time) (bool, string) {
		exp := soonerExpiry(u.ExpiresAt, u.PkgExpires)
		if exp <= 0 {
			return false, ""
		}
		d := dayDelta(now, exp)
		if d < 0 || d > tgExpireSoonDay {
			return false, ""
		}
		if d == 0 {
			return true, "今天到期"
		}
		return true, fmt.Sprintf("还剩 %d 天", d)
	})
}

func (s *Server) tgQuotaCard() tgCard {
	return s.tgUserListCard("流量达到 80%", "没有达到 80% 的用户", func(u *db.User, _ time.Time) (bool, string) {
		if u.TrafficCap <= 0 || u.QuotaRatio < 80 {
			return false, ""
		}
		return true, strconv.Itoa(u.QuotaRatio) + "%"
	})
}

func (s *Server) tgUserListCard(title, empty string, match func(*db.User, time.Time) (bool, string)) tgCard {
	users, err := db.ListUsers(s.DB)
	if err != nil {
		return tgCard{Text: "查询失败", Inline: &tgMarkup{Keyboard: [][]tgButton{{{Text: "回总览", Data: "overview"}}}}}
	}
	now := db.ClockNow(s.DB)
	var b strings.Builder
	b.WriteString(title)
	var rows [][]tgButton
	n := 0
	for _, u := range users {
		if !tgCustomer(u) {
			continue
		}
		ok, extra := match(u, now)
		if !ok {
			continue
		}
		if n < 20 {
			fmt.Fprintf(&b, "\n· %s · %s", u.Username, extra)
			rows = append(rows, []tgButton{{Text: tgShort(u.Username, 20), Data: "user:" + strconv.FormatInt(u.ID, 10)}})
		}
		n++
	}
	if n == 0 {
		b.WriteString("\n" + empty)
	} else if n > 20 {
		fmt.Fprintf(&b, "\n还有 %d 个", n-20)
	}
	rows = append(rows, []tgButton{{Text: "回总览", Data: "overview"}})
	return tgCard{Text: b.String(), Inline: &tgMarkup{Keyboard: rows}}
}

func (s *Server) tgExpireAlert(u *db.User, delta int) (string, *tgMarkup) {
	lead := u.Username + " 已到期"
	if delta >= 0 {
		if delta <= 0 {
			lead = u.Username + " 今天到期"
		} else {
			lead = u.Username + " 将于 " + strconv.Itoa(delta) + " 天后到期"
		}
	}
	return lead + "\n\n" + s.tgAccountText(u), tgViewUser(u.ID)
}

func (s *Server) tgTrafficAlert(u *db.User, ratio int, restored bool) (string, *tgMarkup) {
	lead := u.Username + " 流量已用尽"
	if restored {
		lead = u.Username + " 流量已回到额度内"
	} else if ratio < 100 {
		lead = u.Username + " 流量已用 " + strconv.Itoa(ratio) + "%"
	}
	return lead + "\n\n" + s.tgAccountText(u), tgViewUser(u.ID)
}

func (s *Server) tgAccountMarkup(chatID int64) *tgMarkup {
	if s.tgIsAdmin(chatID) {
		return &tgMarkup{Keyboard: [][]tgButton{{{Text: "回总览", Data: "overview"}}}}
	}
	return s.tgInlineNav(chatID)
}

func tgViewUser(id int64) *tgMarkup {
	return &tgMarkup{Keyboard: [][]tgButton{{{Text: "查看账号", Data: "user:" + strconv.FormatInt(id, 10)}}}}
}

func tgCustomer(u *db.User) bool {
	return u != nil && u.Role != "admin" && u.Enabled
}

const tgSubMenuText = "订阅链接默认不显示。选客户端后再发到这条私聊。"

func tgSubMenuMarkup() *tgMarkup {
	return &tgMarkup{Keyboard: [][]tgButton{
		{{Text: "通用", Data: "sub:uri"}, {Text: "小火箭", Data: "sub:rocket"}},
		{{Text: "Clash", Data: "sub:clash"}, {Text: "sing-box", Data: "sub:box"}},
	}}
}

func (s *Server) tgReplyKeyboard(chatID int64) *tgReplyKeyboard {
	admin := s.tgIsAdmin(chatID)
	_, bound := s.tgBoundUser(chatID)
	var rows [][]tgReplyKey
	if bound {
		rows = append(rows, []tgReplyKey{{Text: "首页"}, {Text: "节点"}})
		rows = append(rows, []tgReplyKey{{Text: "订阅"}, {Text: "近7日"}})
	}
	if admin {
		rows = append(rows, []tgReplyKey{{Text: "总览"}, {Text: "机器"}})
		rows = append(rows, []tgReplyKey{{Text: "到期"}, {Text: "流量"}, {Text: "查用户"}})
	}
	if len(rows) == 0 {
		return nil
	}
	return &tgReplyKeyboard{Keyboard: rows, Resize: true, Persistent: true}
}

func (s *Server) tgInlineNav(chatID int64) *tgMarkup {
	admin := s.tgIsAdmin(chatID)
	_, bound := s.tgBoundUser(chatID)
	var row []tgButton
	if bound {
		row = append(row, tgButton{Text: "首页", Data: "home"}, tgButton{Text: "节点", Data: "nodes"})
	}
	if admin {
		row = append(row, tgButton{Text: "总览", Data: "overview"})
	}
	if len(row) == 0 {
		return nil
	}
	return &tgMarkup{Keyboard: [][]tgButton{row}}
}

type tgReplyKeyboard struct {
	Keyboard   [][]tgReplyKey `json:"keyboard"`
	Resize     bool           `json:"resize_keyboard"`
	Persistent bool           `json:"is_persistent"`
}

type tgReplyKey struct {
	Text string `json:"text"`
}

func tgAgo(now, unix int64) string {
	if unix <= 0 {
		return "还没有上线"
	}
	d := now - unix
	if d < 60 {
		return "刚刚"
	}
	if d < 3600 {
		return fmt.Sprintf("%d 分钟前", d/60)
	}
	if d < 86400 {
		return fmt.Sprintf("%d 小时前", d/3600)
	}
	if d < 86400*30 {
		return fmt.Sprintf("%d 天前", d/86400)
	}
	return time.Unix(unix, 0).Format("2006-01-02")
}

func tgShort(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if n < 2 || len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}

func tgClip(s string) string {
	r := []rune(s)
	if len(r) <= tgTextLimit {
		return s
	}
	return string(r[:tgTextLimit]) + "…"
}
