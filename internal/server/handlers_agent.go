package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"liking/internal/corecfg"
	"liking/internal/db"
	"liking/internal/version"
	"liking/internal/wsproto"
)

// upgradeAllParallel keeps a few agents downloading at once. Unbounded
// parallel downloads share the panel uplink and can all hit the RPC timeout.
const upgradeAllParallel = 4

// locateAgentBinary is replaced in tests so a batch upgrade can run without
// a real release binary beside the test process.
var locateAgentBinary = findAgentBinary

type agentUpgradeError struct {
	status int
	msg    string
	code   string
}

func (e *agentUpgradeError) Error() string { return e.msg }

func failUpgrade(status int, msg, code string) error {
	if status == 0 {
		status = http.StatusBadRequest
	}
	return &agentUpgradeError{status: status, msg: msg, code: code}
}

type agentUpgradeRow struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Code   string `json:"code,omitempty"`
}

func (s *Server) handleUpgradeAgent(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	srv, err := db.GetServer(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "服务器不存在")
		return
	}
	u := userFromCtx(r.Context())
	if err := s.upgradeOnlineAgent(&u.ID, srv); err != nil {
		writeUpgradeErr(w, err)
		return
	}
	jsonOK(w, map[string]any{"ok": true, "version": version.Version})
}

func writeUpgradeErr(w http.ResponseWriter, err error) {
	op, ok := err.(*agentUpgradeError)
	if !ok {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if op.code != "" {
		jsonErrExtra(w, op.status, op.msg, map[string]any{"code": op.code})
		return
	}
	jsonErr(w, op.status, op.msg)
}

// upgradeOnlineAgent pushes the panel's agent binary and waits for the ack.
// It audits a successful upgrade. Callers decide which machines are eligible.
func (s *Server) upgradeOnlineAgent(actorID *int64, srv *db.Server) error {
	osName, arch, ok := s.Hub.ConnMeta(srv.ID)
	if !ok {
		return failUpgrade(http.StatusBadRequest, "Agent 不在线，无法远程升级", "offline")
	}
	if !version.CanRemoteUpgrade(srv.AgentVer) {
		return failUpgrade(http.StatusBadRequest, "该 Agent 版本太旧，不支持远程升级。请复制安装命令在机器上执行一次", "agent_too_old")
	}
	if osName == "" {
		osName = srv.OS
	}
	if arch == "" {
		arch = srv.Arch
	}
	if osName == "" {
		osName = "linux"
	}
	arch = normalizeArch(arch)
	p := locateAgentBinary(osName, arch)
	if p == "" {
		return failUpgrade(http.StatusBadRequest, "面板没有对应架构的 agent 二进制。请在面板机再执行 liking-upgrade，确认有 sha256: OK (liking-agent-linux-"+arch+")", "")
	}
	match, err := agentBinaryHasVersion(p, version.Version)
	if err != nil {
		return failUpgrade(http.StatusInternalServerError, "读取 agent 失败", "")
	}
	if !match {
		return failUpgrade(http.StatusBadRequest, "面板旁边的 agent 文件不是 "+version.Version+"。请在面板机再执行 liking-upgrade，确认有 sha256: OK (liking-agent-linux-"+arch+") 后再点一键升级", "")
	}
	sum, err := sha256Path(p)
	if err != nil {
		return failUpgrade(http.StatusInternalServerError, "计算校验失败", "")
	}
	raw, err := s.Hub.SendRPC(srv.ID, wsproto.TypeUpgrade, wsproto.Upgrade{
		Version:     version.Version,
		SHA256:      sum,
		URL:         agentBinDownloadURL(osName, arch),
		FallbackURL: agentFallbackURL(arch),
	}, 3*time.Minute)
	if err != nil {
		return failUpgrade(http.StatusBadRequest, err.Error(), "")
	}
	var ack wsproto.UpgradeAck
	_ = json.Unmarshal(raw, &ack)
	if !ack.OK {
		msg := strings.TrimSpace(ack.Error)
		if msg == "" {
			msg = "升级被拒绝"
		}
		return failUpgrade(http.StatusBadRequest, msg, "")
	}
	db.AddAudit(s.DB, actorID, "agent.upgrade", srv.Name+" -> "+version.Version)
	return nil
}

func (s *Server) handleUpgradeAgents(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListServers(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reconcileOnline(list)
	rows := make([]agentUpgradeRow, len(list))
	var jobs []int
	for i, srv := range list {
		rows[i] = agentUpgradeRow{ID: srv.ID, Name: srv.Name, Status: "skipped"}
		switch {
		case srv.Online != 1:
			rows[i].Error = "离线"
			rows[i].Code = "offline"
		case !version.CanRemoteUpgrade(srv.AgentVer):
			rows[i].Error = "版本太旧，请用安装命令重装"
			rows[i].Code = "agent_too_old"
		case srv.AgentVer == version.Version:
			rows[i].Error = "已是当前版本"
			rows[i].Code = "current"
		default:
			jobs = append(jobs, i)
		}
	}
	u := userFromCtx(r.Context())
	var wg sync.WaitGroup
	sem := make(chan struct{}, upgradeAllParallel)
	for _, i := range jobs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			srv := list[i]
			row := agentUpgradeRow{ID: srv.ID, Name: srv.Name, Status: "upgraded"}
			if err := s.upgradeOnlineAgent(&u.ID, srv); err != nil {
				row.Status = "failed"
				row.Error = err.Error()
				if op, ok := err.(*agentUpgradeError); ok {
					row.Code = op.code
				}
			}
			rows[i] = row
		}(i)
	}
	wg.Wait()
	upgraded, failed, skipped := 0, 0, 0
	for _, row := range rows {
		switch row.Status {
		case "upgraded":
			upgraded++
		case "failed":
			failed++
		default:
			skipped++
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	jsonOK(w, map[string]any{
		"ok":       true,
		"version":  version.Version,
		"upgraded": upgraded,
		"failed":   failed,
		"skipped":  skipped,
		"results":  rows,
	})
}

func (s *Server) handleUninstallAgent(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	var req struct {
		Confirm bool `json:"confirm"`
	}
	_ = decodeJSON(r, &req)
	if !req.Confirm {
		jsonErr(w, http.StatusBadRequest, "请确认卸载")
		return
	}
	srv, err := db.GetServer(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "服务器不存在")
		return
	}
	if !s.Hub.IsOnline(id) {
		jsonErr(w, http.StatusBadRequest, "Agent 不在线，无法远程卸载")
		return
	}
	if !version.CanRemoteUpgrade(srv.AgentVer) {
		jsonErrExtra(w, http.StatusBadRequest, "该 Agent 版本太旧，不支持远程卸载。请复制安装命令在机器上执行一次", map[string]any{"code": "agent_too_old"})
		return
	}
	raw, err := s.Hub.SendRPC(id, wsproto.TypeUninstall, map[string]any{}, 30*time.Second)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var ack wsproto.UninstallAck
	_ = json.Unmarshal(raw, &ack)
	if !ack.OK {
		msg := strings.TrimSpace(ack.Error)
		if msg == "" {
			msg = "卸载被拒绝"
		}
		jsonErr(w, http.StatusBadRequest, msg)
		return
	}
	u := userFromCtx(r.Context())
	db.AddAudit(s.DB, &u.ID, "agent.uninstall", srv.Name)
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) handlePushNft(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	srv, err := db.GetServer(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "服务器不存在")
		return
	}
	if !s.Hub.IsOnline(id) {
		jsonErr(w, http.StatusBadRequest, "Agent 不在线，无法安装 nftables")
		return
	}
	osName, _, _ := s.Hub.ConnMeta(id)
	if osName != "" && osName != "linux" {
		jsonErr(w, http.StatusBadRequest, "需要 Linux 才能安装 nftables")
		return
	}
	if !s.Hub.HasCap(id, wsproto.CapNFT) {
		jsonErrExtra(w, http.StatusBadRequest, "该 Agent 还不支持安装 nftables。请先一键升级 Agent。", map[string]any{"code": "agent_too_old"})
		return
	}
	raw, err := s.Hub.SendRPC(id, wsproto.TypeEnsureNFT, map[string]any{}, 3*time.Minute+30*time.Second)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var ack wsproto.EnsureNFTAck
	_ = json.Unmarshal(raw, &ack)
	if !ack.OK {
		msg := strings.TrimSpace(ack.Error)
		if msg == "" {
			msg = "安装 nftables 失败"
		}
		jsonErr(w, http.StatusBadRequest, msg)
		return
	}
	u := userFromCtx(r.Context())
	db.AddAudit(s.DB, &u.ID, "agent.ensure_nft", srv.Name)
	out := map[string]any{"ok": true, "have": ack.Have}
	if err := s.pushServer(id); err != nil {
		out["apply_error"] = err.Error()
	}
	jsonOK(w, out)
}

func (s *Server) handlePushWG(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	srv, err := db.GetServer(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "服务器不存在")
		return
	}
	if !s.Hub.IsOnline(id) {
		jsonErr(w, http.StatusBadRequest, "Agent 不在线，无法安装 WireGuard")
		return
	}
	osName, _, _ := s.Hub.ConnMeta(id)
	if osName != "" && osName != "linux" {
		jsonErr(w, http.StatusBadRequest, "需要 Linux 才能安装 WireGuard")
		return
	}
	if !s.Hub.HasCap(id, wsproto.CapWG) {
		jsonErrExtra(w, http.StatusBadRequest, "该 Agent 还不支持安装 WireGuard。请先一键升级 Agent。", map[string]any{"code": "agent_too_old"})
		return
	}
	raw, err := s.Hub.SendRPC(id, wsproto.TypeEnsureWG, map[string]any{}, 3*time.Minute+30*time.Second)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var ack wsproto.EnsureWGAck
	_ = json.Unmarshal(raw, &ack)
	if !ack.OK {
		msg := strings.TrimSpace(ack.Error)
		if msg == "" {
			msg = "安装 WireGuard 失败"
		}
		jsonErr(w, http.StatusBadRequest, msg)
		return
	}
	u := userFromCtx(r.Context())
	db.AddAudit(s.DB, &u.ID, "agent.ensure_wg", srv.Name)
	out := map[string]any{"ok": true, "have": ack.Have}
	if err := s.pushServer(id); err != nil {
		out["apply_error"] = err.Error()
	}
	jsonOK(w, out)
}

func (s *Server) handleAllowPort(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	srv, err := db.GetServer(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "服务器不存在")
		return
	}
	var req struct {
		Port  int    `json:"port"`
		Proto string `json:"proto"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if req.Port < 1 || req.Port > 65535 {
		jsonErr(w, http.StatusBadRequest, "端口无效")
		return
	}
	proto := strings.ToLower(strings.TrimSpace(req.Proto))
	if proto == "" {
		proto = "both"
	}
	if proto != "tcp" && proto != "udp" && proto != "both" {
		jsonErr(w, http.StatusBadRequest, "协议只能是 tcp、udp 或 both")
		return
	}
	if !s.Hub.IsOnline(id) {
		jsonErr(w, http.StatusBadRequest, "Agent 不在线")
		return
	}
	osName, _, _ := s.Hub.ConnMeta(id)
	if osName != "" && osName != "linux" {
		jsonErr(w, http.StatusBadRequest, "需要 Linux")
		return
	}
	if !s.Hub.HasCap(id, wsproto.CapAllowPort) {
		jsonErrExtra(w, http.StatusBadRequest, "该 Agent 还不支持放行端口。请先一键升级。", map[string]any{"code": "agent_too_old"})
		return
	}
	raw, err := s.Hub.SendRPC(id, wsproto.TypeAllowPort, wsproto.AllowPort{Port: req.Port, Proto: proto}, 45*time.Second)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var ack wsproto.AllowPortAck
	_ = json.Unmarshal(raw, &ack)
	if !ack.OK {
		msg := strings.TrimSpace(ack.Error)
		if msg == "" {
			msg = "放行失败"
		}
		jsonErr(w, http.StatusBadRequest, msg)
		return
	}
	u := userFromCtx(r.Context())
	db.AddAudit(s.DB, &u.ID, "agent.allow_port", fmt.Sprintf("%s %d/%s %s", srv.Name, req.Port, proto, ack.How))
	jsonOK(w, map[string]any{"ok": true, "port": req.Port, "how": ack.How})
}

func (s *Server) handlePushCore(w http.ResponseWriter, r *http.Request) {
	s.handleCoreOp(w, r, wsproto.TypeEnsureCore, "推送")
}

func (s *Server) handleRemoveCore(w http.ResponseWriter, r *http.Request) {
	s.handleCoreOp(w, r, wsproto.TypeRemoveCore, "卸载")
}

func (s *Server) handleCoreOp(w http.ResponseWriter, r *http.Request, typ, verb string) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	var req struct {
		Core string `json:"core"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	core := corecfg.NormalizeCore(req.Core)
	if !corecfg.KnownCore(core) {
		jsonErr(w, http.StatusBadRequest, "未知内核")
		return
	}
	srv, err := db.GetServer(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "服务器不存在")
		return
	}
	if !s.Hub.IsOnline(id) {
		jsonErr(w, http.StatusBadRequest, "Agent 不在线，无法"+verb+"核心")
		return
	}
	if !s.Hub.HasCap(id, wsproto.CapCores) {
		jsonErrExtra(w, http.StatusBadRequest, "该 Agent 还不支持推送/卸载核心。请先一键升级 Agent。", map[string]any{"code": "agent_too_old"})
		return
	}
	if typ == wsproto.TypeRemoveCore {
		ins, err := db.ListInboundsByServer(s.DB, id)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		n := 0
		for _, in := range ins {
			if corecfg.NormalizeCore(in.Core) == core {
				n++
			}
		}
		if n > 0 {
			jsonErr(w, http.StatusBadRequest, fmt.Sprintf("还有 %d 个节点在用这个核心，先删节点或改协议再卸载", n))
			return
		}
	}
	timeout := 3 * time.Minute
	if typ == wsproto.TypeRemoveCore {
		timeout = 30 * time.Second
	}
	raw, err := s.Hub.SendRPC(id, typ, wsproto.CoreOp{Core: core}, timeout)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var ack wsproto.CoreOpAck
	_ = json.Unmarshal(raw, &ack)
	if len(ack.Cores) > 0 {
		_ = db.SetServerCores(s.DB, id, ack.Cores)
	}
	if !ack.OK {
		msg := strings.TrimSpace(ack.Error)
		if msg == "" {
			msg = verb + "被拒绝"
		}
		jsonErr(w, http.StatusBadRequest, msg)
		return
	}
	u := userFromCtx(r.Context())
	db.AddAudit(s.DB, &u.ID, "agent."+typ, srv.Name+" "+core)
	jsonOK(w, map[string]any{"ok": true, "core": core, "cores": ack.Cores})
}

func (s *Server) handleRotateToken(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	srv, err := db.GetServer(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "服务器不存在")
		return
	}
	tok, err := db.RotateServerToken(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Hub.Drop(id)
	srv.Token = tok
	u := userFromCtx(r.Context())
	db.AddAudit(s.DB, &u.ID, "server.rotate_token", srv.Name)
	jsonOK(w, s.installFields(r, srv, map[string]any{"token": tok, "command": s.installCmd(r, srv)}))
}

func sha256Path(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return sha256File(f)
}
