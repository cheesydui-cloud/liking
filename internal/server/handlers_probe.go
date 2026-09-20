package server

import (
	"database/sql"
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

type probeReq struct {
	ServerID int64  `json:"server_id"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	URI      string `json:"uri"`
}

func (s *Server) handleParseURI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URI string `json:"uri"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	t, err := corecfg.ParseShareURI(req.URI)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true, "target": t.Preview()})
}

func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	var req probeReq
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	host := strings.TrimSpace(req.Host)
	port := req.Port
	if strings.TrimSpace(req.URI) != "" {
		t, err := corecfg.ParseShareURI(req.URI)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		host, port = t.Host, t.Port
	}
	s.runProbe(w, req.ServerID, host, port)
}

var listenDialTimeout = func(network, address string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, address, timeout)
}

func (s *Server) handleListenProbe(w http.ResponseWriter, r *http.Request) {
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
	host := corecfg.ShareHost(in)
	port := in.Port
	if host == "" {
		jsonErr(w, http.StatusBadRequest, "没有公开地址")
		return
	}
	if port < 1 || port > 65535 {
		jsonErr(w, http.StatusBadRequest, "端口无效")
		return
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	start := time.Now()
	conn, err := listenDialTimeout("tcp", addr, 4*time.Second)
	out := map[string]any{
		"ok":   false,
		"host": host,
		"port": port,
	}
	if err != nil {
		msg := "不通"
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			msg = "超时"
		} else if strings.TrimSpace(err.Error()) != "" {
			msg = err.Error()
		}
		out["error"] = msg
		jsonOK(w, out)
		return
	}
	_ = conn.Close()
	ms := time.Since(start).Milliseconds()
	if ms < 1 {
		ms = 1
	}
	out["ok"] = true
	out["latency_ms"] = ms
	jsonOK(w, out)
}

func (s *Server) handleInboundProbe(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	in, err := db.GetInbound(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "入站不存在")
		return
	}
	host, port, err := probeDialTarget(s.DB, in)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.runProbe(w, in.ServerID, host, port)
}

func probeDialTarget(d *sql.DB, in *db.Inbound) (string, int, error) {
	if in == nil {
		return "", 0, errNoProbeTarget
	}
	if in.Profile == corecfg.ProfilePortForward {
		st := corecfg.ParseSettings(in.Settings)
		host := strings.TrimSpace(st.String("dest_host"))
		port := st.Int("dest_port", 0)
		if host == "" || port < 1 {
			return "", 0, errNoProbeTarget
		}
		return host, port, nil
	}
	if strings.TrimSpace(in.ExitURI) != "" {
		t, err := corecfg.ParseShareURI(in.ExitURI)
		if err != nil {
			return "", 0, err
		}
		return t.Host, t.Port, nil
	}
	if in.LineKind != "chain" || in.ExitInboundID == nil || *in.ExitInboundID == 0 {
		return "", 0, errNoProbeTarget
	}
	land, err := db.GetInbound(d, *in.ExitInboundID)
	if err != nil || land == nil {
		return "", 0, errLandingGone
	}
	host := corecfg.ShareHost(land)
	if host == "" {
		return "", 0, errLandingNoHost
	}
	if land.Port < 1 {
		return "", 0, errNoProbeTarget
	}
	return host, land.Port, nil
}

var (
	errNoProbeTarget = simpleError("只有链式和端口中转可以探测")
	errLandingGone   = simpleError("落地节点不存在")
	errLandingNoHost = simpleError("落地节点没有公开地址")
)

func parseDestAddr(s string) (string, int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, false
	}
	if host, portStr, err := net.SplitHostPort(s); err == nil {
		port, err := strconv.Atoi(portStr)
		if err != nil || host == "" || port < 1 || port > 65535 {
			return "", 0, false
		}
		return host, port, true
	}
	if strings.Contains(s, ":") {
		return "", 0, false
	}
	return s, 443, true
}

func (s *Server) handleDestProbe(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	var req struct {
		Dests []string `json:"dests"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	dests := make([]string, 0, len(req.Dests))
	seen := map[string]struct{}{}
	for _, raw := range req.Dests {
		d := strings.TrimSpace(raw)
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		dests = append(dests, d)
		if len(dests) >= 24 {
			break
		}
	}
	if len(dests) == 0 {
		jsonErr(w, http.StatusBadRequest, "请提供伪装目标")
		return
	}
	if !s.Hub.IsOnline(id) {
		jsonErr(w, http.StatusBadRequest, "入口 Agent 不在线")
		return
	}
	if !s.Hub.HasCap(id, wsproto.CapProbe) {
		jsonErrExtra(w, http.StatusBadRequest, "该 Agent 还不支持延迟探测。请先一键升级 Agent。", map[string]any{"code": "agent_too_old"})
		return
	}

	type row struct {
		Dest      string `json:"dest"`
		Host      string `json:"host,omitempty"`
		Port      int    `json:"port,omitempty"`
		OK        bool   `json:"ok"`
		LatencyMS int64  `json:"latency_ms,omitempty"`
		Error     string `json:"error,omitempty"`
	}
	out := make([]row, len(dests))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, dest := range dests {
		wg.Add(1)
		go func(i int, dest string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			host, port, ok := parseDestAddr(dest)
			if !ok {
				out[i] = row{Dest: dest, Error: "无效"}
				return
			}
			raw, err := s.Hub.SendRPC(id, wsproto.TypeProbe, wsproto.Probe{Host: host, Port: port}, 3*time.Second)
			if err != nil {
				out[i] = row{Dest: dest, Host: host, Port: port, Error: err.Error()}
				return
			}
			var ack wsproto.ProbeAck
			_ = json.Unmarshal(raw, &ack)
			item := row{Dest: dest, Host: host, Port: port, OK: ack.OK, LatencyMS: ack.LatencyMS}
			if !ack.OK {
				msg := strings.TrimSpace(ack.Error)
				if msg == "" {
					msg = "timeout"
				}
				item.Error = msg
			}
			out[i] = item
		}(i, dest)
	}
	wg.Wait()
	jsonOK(w, map[string]any{"results": out})
}

func (s *Server) runProbe(w http.ResponseWriter, serverID int64, host string, port int) {
	host = strings.TrimSpace(host)
	if serverID == 0 {
		jsonErr(w, http.StatusBadRequest, "请选择入口实例")
		return
	}
	if host == "" || port < 1 || port > 65535 {
		jsonErr(w, http.StatusBadRequest, "主机或端口无效")
		return
	}
	if !s.Hub.IsOnline(serverID) {
		jsonErr(w, http.StatusBadRequest, "入口 Agent 不在线")
		return
	}
	if !s.Hub.HasCap(serverID, wsproto.CapProbe) {
		jsonErrExtra(w, http.StatusBadRequest, "该 Agent 还不支持延迟探测。请先一键升级 Agent。", map[string]any{"code": "agent_too_old"})
		return
	}
	raw, err := s.Hub.SendRPC(serverID, wsproto.TypeProbe, wsproto.Probe{Host: host, Port: port}, 8*time.Second)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var ack wsproto.ProbeAck
	_ = json.Unmarshal(raw, &ack)
	out := map[string]any{
		"ok":         ack.OK,
		"host":       host,
		"port":       port,
		"latency_ms": ack.LatencyMS,
	}
	if !ack.OK {
		msg := strings.TrimSpace(ack.Error)
		if msg == "" {
			msg = "探测失败"
		}
		out["error"] = msg
	}
	jsonOK(w, out)
}
