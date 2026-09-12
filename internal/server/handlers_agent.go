package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"liking/internal/db"
	"liking/internal/version"
	"liking/internal/wsproto"
)

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
	osName, arch, ok := s.Hub.ConnMeta(id)
	if !ok {
		jsonErr(w, http.StatusBadRequest, "Agent 不在线，无法远程升级")
		return
	}
	if !version.CanRemoteUpgrade(srv.AgentVer) {
		jsonErrExtra(w, http.StatusBadRequest, "该 Agent 版本太旧，不支持远程升级。请复制安装命令在机器上执行一次", map[string]any{"code": "agent_too_old"})
		return
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
	p := findAgentBinary(osName, arch)
	if p == "" {
		jsonErr(w, http.StatusBadRequest, "面板没有对应架构的 agent 二进制")
		return
	}
	sum, err := sha256Path(p)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "计算校验失败")
		return
	}
	base := panelURL(s.DB, r)
	dl := fmt.Sprintf("%s/v1/agent-bin?os=%s&arch=%s", base, osName, arch)
	raw, err := s.Hub.SendRPC(id, wsproto.TypeUpgrade, wsproto.Upgrade{
		Version: version.Version,
		SHA256:  sum,
		URL:     dl,
	}, 3*time.Minute)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var ack wsproto.UpgradeAck
	_ = json.Unmarshal(raw, &ack)
	if !ack.OK {
		msg := strings.TrimSpace(ack.Error)
		if msg == "" {
			msg = "升级被拒绝"
		}
		jsonErr(w, http.StatusBadRequest, msg)
		return
	}
	u := userFromCtx(r.Context())
	db.AddAudit(s.DB, &u.ID, "agent.upgrade", srv.Name+" -> "+version.Version)
	jsonOK(w, map[string]any{"ok": true, "version": version.Version})
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
	jsonOK(w, map[string]any{"token": tok, "command": s.installCmd(r, srv)})
}

func sha256Path(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return sha256File(f)
}
