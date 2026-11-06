package server

import (
	"net/http"
	"strings"

	"liking/internal/db"
)

func (s *Server) handleCardOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string  `json:"kind"`
		IDs  []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if err := db.ReorderCards(s.DB, req.Kind, req.IDs); err != nil {
		if msg, ok := cardOrderMessage(err); ok {
			jsonErr(w, http.StatusBadRequest, msg)
			return
		}
		jsonErr(w, http.StatusInternalServerError, "保存顺序失败")
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

func cardOrderMessage(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	msg := err.Error()
	switch msg {
	case "类型无效", "顺序为空", "无效编号", "顺序里有重复", "有不存在的条目", "只能在同一台实例里调整节点":
		return msg, true
	default:
		if strings.HasPrefix(msg, "一次最多调整") {
			return msg, true
		}
		return "", false
	}
}
