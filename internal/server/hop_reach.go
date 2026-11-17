package server

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"liking/internal/corecfg"
	"liking/internal/db"
	"liking/internal/wsproto"
)

const (
	reachUp   = "up"
	reachDown = "down"
	reachUDP  = "udp"
	reachSkip = "skip"

	reachUDPNote     = "UDP，TCP 通断不能确认"
	reachUDPUntested = "UDP 未测"
	reachExternal    = "外部跳，测不到下一段"
	reachOffline     = "上一台不在线"
	reachNeedAgent   = "需升级 Agent"
	reachNoAddr      = "没有地址"
	reachNoDest      = "没有目标地址"
)

// reachSeg is one dial the previous machine actually makes.
// Status udp and skip are not TCP-dialed. down is not an HTTP error.
type reachSeg struct {
	Index     int    `json:"index"`
	FromID    int64  `json:"from_id,omitempty"`
	FromName  string `json:"from_name"`
	ToName    string `json:"to_name"`
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	Note      string `json:"note,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

func (s *Server) handleInboundHopProbe(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	in, err := db.GetInbound(s.DB, id)
	if err != nil || in == nil {
		jsonErr(w, http.StatusNotFound, "入站不存在")
		return
	}
	var segs []reachSeg
	switch {
	case in.Profile == corecfg.ProfilePortForward:
		segs = planPortReach(in)
	case in.LineKind == "chain" || strings.TrimSpace(in.ExitURI) != "":
		list, err := db.ListInbounds(s.DB)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "读取失败")
			return
		}
		byID := map[int64]*db.Inbound{}
		for _, x := range list {
			if x != nil {
				byID[x.ID] = x
			}
		}
		srvs, err := db.ListServers(s.DB)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "读取失败")
			return
		}
		segs = planChainReach(in, byID, serversByID(srvs))
	default:
		jsonErr(w, http.StatusBadRequest, "只有链式和端口中转可以探测")
		return
	}
	s.fillReach(segs)
	writeReach(w, segs)
}

func (s *Server) handleTrafficHopProbe(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	fw, err := db.GetTrafficForward(s.DB, id)
	if err != nil || fw == nil {
		jsonErr(w, http.StatusNotFound, "流量中转不存在")
		return
	}
	srvs, err := db.ListServers(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "读取失败")
		return
	}
	var landing *db.Inbound
	if fw.LandingInboundID > 0 {
		landing, _ = db.GetInbound(s.DB, fw.LandingInboundID)
	}
	segs := planTrafficReach(fw, serversByID(srvs), landing)
	s.fillReach(segs)
	writeReach(w, segs)
}

func (s *Server) handleServerReach(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	srv, err := db.GetServer(s.DB, id)
	if err != nil || srv == nil {
		jsonErr(w, http.StatusNotFound, "服务器不存在")
		return
	}
	var req struct {
		Host string `json:"host"`
		Port int    `json:"port"`
		UDP  bool   `json:"udp"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	host := strings.TrimSpace(req.Host)
	seg := reachSeg{
		FromID:   id,
		FromName: srv.Name,
		ToName:   hostPortName(host, req.Port),
		Host:     host,
		Port:     req.Port,
	}
	if req.UDP {
		seg.Status = reachUDP
		seg.Note = reachUDPNote
		writeReach(w, []reachSeg{seg})
		return
	}
	if host == "" || req.Port < 1 || req.Port > 65535 {
		seg.Status = reachSkip
		seg.Error = reachNoAddr
		writeReach(w, []reachSeg{seg})
		return
	}
	segs := []reachSeg{seg}
	s.fillReach(segs)
	writeReach(w, segs)
}

func writeReach(w http.ResponseWriter, segs []reachSeg) {
	if segs == nil {
		segs = []reachSeg{}
	}
	jsonOK(w, map[string]any{"hops": segs})
}

func serversByID(list []*db.Server) map[int64]*db.Server {
	out := map[int64]*db.Server{}
	for _, s := range list {
		if s != nil {
			out[s.ID] = s
		}
	}
	return out
}

func planChainReach(entry *db.Inbound, byID map[int64]*db.Inbound, servers map[int64]*db.Server) []reachSeg {
	if entry == nil {
		return []reachSeg{{Status: reachSkip, Error: "链式线路不存在"}}
	}
	path, err := corecfg.ChainPath(entry, byID)
	if err != nil {
		return []reachSeg{{
			FromID:   entry.ServerID,
			FromName: firstNonEmpty(entry.ServerName, reachServerName(servers, entry.ServerID)),
			Status:   reachSkip,
			Error:    err.Error(),
		}}
	}
	segs := make([]reachSeg, 0, len(path))
	var prev *db.Inbound
	for i, ph := range path {
		seg := reachSeg{Index: i}
		if i == 0 {
			seg.FromID = entry.ServerID
			seg.FromName = firstNonEmpty(entry.ServerName, reachServerName(servers, entry.ServerID))
		} else if prev == nil {
			seg.Status = reachSkip
			seg.Error = reachExternal
			seg.FromName = "外部跳"
		} else {
			seg.FromID = prev.ServerID
			seg.FromName = firstNonEmpty(prev.ServerName, reachServerName(servers, prev.ServerID))
		}
		host, port, name, proto := pathTarget(ph)
		seg.Host = host
		seg.Port = port
		seg.ToName = name
		if seg.Status == "" {
			applyReachProto(&seg, proto)
		}
		if seg.Status == "" && !reachHostOK(host, port) {
			seg.Status = reachSkip
			seg.Error = reachNoAddr
		}
		segs = append(segs, seg)
		if ph.Land != nil {
			prev = ph.Land
		} else {
			prev = nil
		}
	}
	return segs
}

func planPortReach(entry *db.Inbound) []reachSeg {
	if entry == nil {
		return []reachSeg{{Status: reachSkip, Error: reachNoDest}}
	}
	st := corecfg.ParseSettings(entry.Settings)
	host := strings.TrimSpace(st.String("dest_host"))
	port := st.Int("dest_port", 0)
	network := strings.ToLower(strings.TrimSpace(st.String("network")))
	if network == "" {
		network = strings.ToLower(strings.TrimSpace(entry.Network))
	}
	seg := reachSeg{
		FromID:   entry.ServerID,
		FromName: entry.ServerName,
		ToName:   hostPortName(host, port),
		Host:     host,
		Port:     port,
	}
	if network == "udp" {
		seg.Status = reachUDP
		seg.Note = reachUDPNote
		return []reachSeg{seg}
	}
	if !reachHostOK(host, port) {
		seg.Status = reachSkip
		seg.Error = reachNoDest
		return []reachSeg{seg}
	}
	if strings.Contains(network, "udp") {
		seg.Note = reachUDPUntested
	}
	return []reachSeg{seg}
}

func planTrafficReach(fw *db.TrafficForward, servers map[int64]*db.Server, landing *db.Inbound) []reachSeg {
	if fw == nil {
		return []reachSeg{{Status: reachSkip, Error: "缺少转发"}}
	}
	fromName := firstNonEmpty(fw.EntryServerName, reachServerName(servers, fw.EntryServerID))
	if landing == nil {
		return []reachSeg{{
			FromID:   fw.EntryServerID,
			FromName: fromName,
			Status:   reachSkip,
			Error:    "落地节点不存在",
		}}
	}
	proto := inboundReachProto(landing)
	type hop struct {
		serverID int64
		port     int
		dial     string
		name     string
	}
	chain := []hop{{
		serverID: fw.EntryServerID,
		port:     fw.EntryPort,
		name:     fromName,
	}}
	for _, h := range fw.Hops {
		chain = append(chain, hop{
			serverID: h.ServerID,
			port:     h.ListenPort,
			dial:     h.DialHost,
			name:     firstNonEmpty(h.ServerName, reachServerName(servers, h.ServerID)),
		})
	}
	segs := make([]reachSeg, 0, len(chain))
	for i, cur := range chain {
		seg := reachSeg{Index: i, FromID: cur.serverID, FromName: cur.name}
		var destHost string
		var destPort int
		var toName string
		if i+1 < len(chain) {
			next := chain[i+1]
			pub := ""
			if srv := servers[next.serverID]; srv != nil {
				pub = srv.PublicHost
			}
			destHost = corecfg.DialHost(next.dial, pub)
			destPort = next.port
			toName = next.name
		} else {
			pub := ""
			if srv := servers[landing.ServerID]; srv != nil {
				pub = srv.PublicHost
			}
			if pub == "" {
				pub = landing.ServerHost
			}
			destHost = corecfg.DialHost(fw.LandingDialHost, pub)
			destPort = landing.Port
			toName = firstNonEmpty(landing.Name, fw.LandingName)
		}
		if toName == "" {
			toName = hostPortName(destHost, destPort)
		}
		seg.ToName = toName
		seg.Host = destHost
		seg.Port = destPort
		applyReachProto(&seg, proto)
		if seg.Status == "" && !reachHostOK(destHost, destPort) {
			seg.Status = reachSkip
			seg.Error = reachNoAddr
		}
		segs = append(segs, seg)
	}
	return segs
}

func (s *Server) fillReach(segs []reachSeg) {
	if len(segs) == 0 {
		return
	}
	if s == nil || s.Hub == nil {
		for i := range segs {
			if segs[i].Status == "" {
				segs[i].Status = reachSkip
				segs[i].Error = reachOffline
			}
		}
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range segs {
		if segs[i].Status != "" {
			continue
		}
		if !reachHostOK(segs[i].Host, segs[i].Port) {
			segs[i].Status = reachSkip
			if segs[i].Error == "" {
				segs[i].Error = reachNoAddr
			}
			continue
		}
		if segs[i].FromID == 0 || !s.Hub.IsOnline(segs[i].FromID) {
			segs[i].Status = reachSkip
			segs[i].Error = reachOffline
			continue
		}
		if !s.Hub.HasCap(segs[i].FromID, wsproto.CapProbe) {
			segs[i].Status = reachSkip
			segs[i].Error = reachNeedAgent
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			raw, err := s.Hub.SendRPC(segs[i].FromID, wsproto.TypeProbe, wsproto.Probe{Host: segs[i].Host, Port: segs[i].Port}, 6*time.Second)
			if err != nil {
				segs[i].Status = reachDown
				segs[i].Error = err.Error()
				return
			}
			var ack wsproto.ProbeAck
			if json.Unmarshal(raw, &ack) != nil {
				segs[i].Status = reachDown
				segs[i].Error = "探测失败"
				return
			}
			if !ack.OK {
				msg := strings.TrimSpace(ack.Error)
				if msg == "" {
					msg = "不通"
				}
				segs[i].Status = reachDown
				segs[i].Error = msg
				return
			}
			segs[i].Status = reachUp
			segs[i].LatencyMS = ack.LatencyMS
		}(i)
	}
	wg.Wait()
}

func pathTarget(ph corecfg.PathHop) (host string, port int, name, proto string) {
	if ph.Land != nil {
		host = corecfg.ShareHost(ph.Land)
		port = ph.Land.Port
		name = firstNonEmpty(ph.Land.Name, hostPortName(host, port))
		proto = inboundReachProto(ph.Land)
		return
	}
	if ph.Share != nil {
		host = ph.Share.Host
		port = ph.Share.Port
		name = firstNonEmpty(ph.Share.Name, hostPortName(host, port))
		if ph.Share.Scheme == "socks5" || ph.Share.Scheme == "socks" || ph.Share.Scheme == "socks5h" {
			proto = "tcp+udp"
		} else {
			proto = "tcp"
		}
		return
	}
	if ph.Socks != nil {
		host = ph.Socks.Host
		port = ph.Socks.Port
		name = hostPortName(host, port)
		proto = "tcp+udp"
	}
	return
}

func inboundReachProto(in *db.Inbound) string {
	if in == nil {
		return "tcp"
	}
	netw := in.Network
	if in.Profile == corecfg.ProfileMieru {
		netw = corecfg.ParseSettings(in.Settings).String("transport")
	}
	return corecfg.ForwardProto(in.Profile, netw)
}

func applyReachProto(seg *reachSeg, proto string) {
	if seg == nil {
		return
	}
	switch proto {
	case "udp":
		seg.Status = reachUDP
		seg.Note = reachUDPNote
	case "tcp+udp":
		seg.Note = reachUDPUntested
	}
}

func reachHostOK(host string, port int) bool {
	return strings.TrimSpace(host) != "" && port >= 1 && port <= 65535
}

func reachServerName(servers map[int64]*db.Server, id int64) string {
	if servers == nil || id == 0 {
		return ""
	}
	s := servers[id]
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.Name)
}

func hostPortName(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" || port < 1 {
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if t := strings.TrimSpace(s); t != "" {
			return t
		}
	}
	return ""
}
