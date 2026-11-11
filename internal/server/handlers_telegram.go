package server

import (
	"net/http"

	"liking/internal/db"
)

func (s *Server) handleTGTest(w http.ResponseWriter, r *http.Request) {
	if s.tgToken() == "" {
		jsonErr(w, http.StatusBadRequest, "请先填写 Bot Token")
		return
	}
	if err := s.tgAdmin("liking 测试消息"); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) handleTGMe(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	_, bound, err := db.TGChatByUser(s.DB, u.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	name, _ := db.GetSetting(s.DB, "tg_bot_username")
	jsonOK(w, map[string]any{"bound": bound, "bot_username": name})
}

func (s *Server) handleTGBindCode(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	if u.Role == "admin" {
		jsonErr(w, http.StatusForbidden, "管理员不用绑定")
		return
	}
	if !s.tgEnabled() {
		jsonErr(w, http.StatusBadRequest, "管理员还没有配置 Telegram")
		return
	}
	code, exp, err := s.issueTGCode(u.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "生成绑定码失败")
		return
	}
	name, _ := db.GetSetting(s.DB, "tg_bot_username")
	jsonOK(w, map[string]any{
		"code":         code,
		"expires_at":   exp.Unix(),
		"bot_username": name,
	})
}

func (s *Server) handleTGUnbind(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	if err := db.TGUnbindUser(s.DB, u.ID); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	db.AddAudit(s.DB, &u.ID, "tg.unbind", u.Username)
	jsonOK(w, map[string]any{"ok": true})
}
