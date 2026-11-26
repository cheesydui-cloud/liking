package server

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"liking/internal/db"
)

type tgTemplate struct {
	Key      string
	Label    string
	Hint     string
	Audience string
	Default  string
}

const tgTplMax = 400

// tgTemplateCatalog is the push copy an admin can edit. Empty stored text uses Default.
// Server and certificate lines are admin-only. User copies never receive node status.
var tgTemplateCatalog = []tgTemplate{
	{Key: "tg_tpl_expire_soon", Label: "即将到期", Audience: "user", Hint: "{user} 用户名，{days} 天数，{date} 日期，{account} 账号摘要", Default: "{user} 将于 {days} 天后到期\n\n{account}"},
	{Key: "tg_tpl_expire_today", Label: "今天到期", Audience: "user", Hint: "{user} {date} {account}", Default: "{user} 今天到期\n\n{account}"},
	{Key: "tg_tpl_expire_done", Label: "已到期", Audience: "user", Hint: "{user} {date} {account}", Default: "{user} 已到期\n\n{account}"},
	{Key: "tg_tpl_traffic_warn", Label: "流量达到 80%", Audience: "user", Hint: "{user} {ratio} 已用百分比，{account}", Default: "{user} 流量已用 {ratio}%\n\n{account}"},
	{Key: "tg_tpl_traffic_over", Label: "流量用尽", Audience: "user", Hint: "{user} {ratio} {account}", Default: "{user} 流量已用尽\n\n{account}"},
	{Key: "tg_tpl_traffic_back", Label: "流量恢复", Audience: "user", Hint: "{user} {ratio} {account}", Default: "{user} 流量已回到额度内\n\n{account}"},
	{Key: "tg_tpl_server_down", Label: "机器离线", Audience: "admin", Hint: "{server} 机器名，{detail} 状态。只推给管理员", Default: "{server} 离线\n{detail}"},
	{Key: "tg_tpl_server_up", Label: "机器恢复", Audience: "admin", Hint: "{server} {detail}。只推给管理员", Default: "{server} 已恢复在线\n{detail}"},
	{Key: "tg_tpl_cert_soon", Label: "证书即将到期", Audience: "admin", Hint: "{name} {date}。只推给管理员", Default: "证书 {name} 即将到期\n到期 {date}"},
	{Key: "tg_tpl_cert_done", Label: "证书已过期", Audience: "admin", Hint: "{name} {date}。只推给管理员", Default: "证书 {name} 已过期\n到期 {date}"},
	{Key: "tg_tpl_backup", Label: "备份失败", Audience: "admin", Hint: "{error}。只推给管理员", Default: "定时备份失败：{error}"},
}

func tgTemplateByKey(key string) (tgTemplate, bool) {
	for _, item := range tgTemplateCatalog {
		if item.Key == key {
			return item, true
		}
	}
	return tgTemplate{}, false
}

func (s *Server) tgTpl(key string, vars map[string]string) string {
	item, ok := tgTemplateByKey(key)
	raw := ""
	if ok {
		raw = item.Default
	}
	if stored, err := db.GetSetting(s.DB, key); err == nil && strings.TrimSpace(stored) != "" {
		raw = stored
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	for name, value := range vars {
		raw = strings.ReplaceAll(raw, "{"+name+"}", value)
	}
	return strings.TrimSpace(raw)
}

func normalizeTGTemplate(raw string) (string, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if utf8.RuneCountInString(raw) > tgTplMax {
		return "", fmt.Errorf("推送模板最多 400 字")
	}
	for _, r := range raw {
		if r == '\n' || r == '\t' {
			continue
		}
		if r < 32 {
			return "", fmt.Errorf("推送模板含有无法显示的字符")
		}
	}
	return raw, nil
}

func (s *Server) tgTemplatePublic() []map[string]string {
	out := make([]map[string]string, 0, len(tgTemplateCatalog))
	for _, item := range tgTemplateCatalog {
		stored, _ := db.GetSetting(s.DB, item.Key)
		out = append(out, map[string]string{
			"key":      item.Key,
			"label":    item.Label,
			"hint":     item.Hint,
			"audience": item.Audience,
			"value":    strings.TrimSpace(stored),
			"fallback": item.Default,
		})
	}
	return out
}
