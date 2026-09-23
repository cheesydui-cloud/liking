package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"liking/internal/db"
	"liking/internal/wsproto"
)

func (s *Server) handleKickUser(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	u, err := db.GetUser(s.DB, id)
	if err != nil || u == nil {
		jsonErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	var req struct {
		InboundID int64 `json:"inbound_id"`
	}
	_ = decodeJSON(r, &req)
	clients, err := db.ListClientsByUser(s.DB, u.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	emailsByServer := map[int64][]string{}
	for _, c := range clients {
		if c == nil || !c.Enabled || strings.TrimSpace(c.Email) == "" {
			continue
		}
		if req.InboundID != 0 && c.InboundID != req.InboundID {
			continue
		}
		in, err := db.GetInbound(s.DB, c.InboundID)
		if err != nil || in == nil {
			continue
		}
		emailsByServer[in.ServerID] = append(emailsByServer[in.ServerID], c.Email)
	}
	if len(emailsByServer) == 0 {
		jsonErr(w, http.StatusBadRequest, "这个用户没有节点账号")
		return
	}

	type row struct {
		ServerID   int64  `json:"server_id"`
		ServerName string `json:"server_name"`
		OK         bool   `json:"ok"`
		Closed     int    `json:"closed"`
		Error      string `json:"error,omitempty"`
	}
	out := make([]row, 0, len(emailsByServer))
	online := 0
	kicked := 0
	tooOld := 0
	for sid, emails := range emailsByServer {
		rec := row{ServerID: sid}
		if srv, err := db.GetServer(s.DB, sid); err == nil && srv != nil {
			rec.ServerName = srv.Name
		}
		if !s.Hub.IsOnline(sid) {
			rec.Error = "Agent 不在线"
			out = append(out, rec)
			continue
		}
		online++
		if !s.Hub.HasCap(sid, wsproto.CapKick) {
			tooOld++
			rec.Error = "该 Agent 还不支持踢下线。请先一键升级 Agent。"
			out = append(out, rec)
			continue
		}
		raw, err := s.Hub.SendRPC(sid, wsproto.TypeKick, wsproto.Kick{Emails: emails}, 30*time.Second)
		if err != nil {
			rec.Error = err.Error()
			out = append(out, rec)
			continue
		}
		var ack wsproto.KickAck
		_ = json.Unmarshal(raw, &ack)
		rec.Closed = ack.Closed
		if !ack.OK {
			msg := strings.TrimSpace(ack.Error)
			if msg == "" {
				msg = "踢下线失败"
			}
			rec.Error = msg
			out = append(out, rec)
			continue
		}
		rec.OK = true
		kicked++
		out = append(out, rec)
	}
	if online == 0 {
		jsonErr(w, http.StatusBadRequest, "没有在线实例")
		return
	}
	if kicked == 0 && tooOld == online {
		jsonErrExtra(w, http.StatusBadRequest, "该 Agent 还不支持踢下线。请先一键升级 Agent。", map[string]any{"code": "agent_too_old", "results": out})
		return
	}
	admin := userFromCtx(r.Context())
	if admin != nil {
		db.AddAudit(s.DB, &admin.ID, "user.kick", u.Username)
	}
	jsonOK(w, map[string]any{"ok": kicked > 0, "results": out})
}
