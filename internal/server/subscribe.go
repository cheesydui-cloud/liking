package server

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"liking/internal/corecfg"
	"liking/internal/db"
)

func (s *Server) handleSub(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	fmtName := strings.ToLower(chi.URLParam(r, "fmt"))
	if fmtName == "" {
		fmtName = strings.ToLower(r.URL.Query().Get("format"))
	}
	if fmtName == "" {
		fmtName = detectSubFormat(r.UserAgent())
	}
	u, err := db.GetUserBySubToken(s.DB, token)
	if err != nil || u == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var pkg *db.Package
	if u.PackageID != nil {
		pkg, _ = db.GetPackage(s.DB, *u.PackageID)
	}
	if !db.UserAccessOK(u, pkg) {
		http.Error(w, "expired", http.StatusForbidden)
		return
	}
	clients, err := db.ListClientsByUser(s.DB, u.ID)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	clients = filterStarredClients(s.DB, u, clients)
	writeSubInfo(w, u)
	over := s.serverOverMap()

	switch fmtName {
	case "clash", "meta", "mihomo":
		body, err := buildClash(s.DB, u, clients, over)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.Header().Set("Profile-Title", "liking")
		_, _ = w.Write([]byte(body))
	case "singbox", "sing-box", "sfa":
		body, err := buildSingboxSub(s.DB, u, clients, over)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	default:
		body, err := buildURIList(s.DB, u, clients, over)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(body))))
	}
}

func writeSubInfo(w http.ResponseWriter, u *db.User) {
	if u == nil {
		return
	}
	w.Header().Set("Profile-Update-Interval", "24")
	expire := soonerUnix(u.ExpiresAt, u.PkgExpires)
	info := "upload=0; download=" + strconv.FormatInt(u.BilledBytes, 10)
	if u.TrafficCap > 0 {
		info += "; total=" + strconv.FormatInt(u.TrafficCap, 10)
	}
	if expire > 0 {
		info += "; expire=" + strconv.FormatInt(expire, 10)
	}
	w.Header().Set("Subscription-Userinfo", info)
}

func soonerUnix(a, b int64) int64 {
	switch {
	case a <= 0:
		return b
	case b <= 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}

func inboundLive(d *sql.DB, in *db.Inbound, over map[int64]bool) bool {
	return inboundSkipReason(d, in, over) == ""
}

// inboundSkipReason 空字符串表示进订阅。skip 表示端口中转等，不进列表也不进「未进订阅」。
func inboundSkipReason(d *sql.DB, in *db.Inbound, over map[int64]bool) string {
	if in == nil {
		return "skip"
	}
	if !corecfg.UserFacing(in.Profile) {
		return "skip"
	}
	if !in.Enabled {
		return "停用"
	}
	if over != nil && over[in.ServerID] {
		return "实例已满"
	}
	srv, err := db.GetServer(d, in.ServerID)
	if err != nil || srv == nil {
		return "缺核心"
	}
	if !db.ServerHasCore(srv, in.Core) {
		return "缺核心"
	}
	return ""
}

func starredSettingKey(userID int64) string {
	return "starred_inbounds." + strconv.FormatInt(userID, 10)
}

func loadStarredIDs(d *sql.DB, userID int64) []int64 {
	raw, _ := db.GetSetting(d, starredSettingKey(userID))
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []int64{}
	}
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return []int64{}
	}
	out := make([]int64, 0, len(ids))
	seen := map[int64]struct{}{}
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func filterStarredClients(d *sql.DB, u *db.User, clients []*db.Client) []*db.Client {
	if u == nil || u.Role != "admin" || len(clients) == 0 {
		return clients
	}
	ids := loadStarredIDs(d, u.ID)
	if len(ids) == 0 {
		return clients
	}
	allow := map[int64]struct{}{}
	for _, id := range ids {
		allow[id] = struct{}{}
	}
	out := make([]*db.Client, 0, len(ids))
	for _, c := range clients {
		if c == nil {
			continue
		}
		if _, ok := allow[c.InboundID]; ok {
			out = append(out, c)
		}
	}
	return out
}

func detectSubFormat(ua string) string {
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "clash") || strings.Contains(l, "stash") || strings.Contains(l, "mihomo") || strings.Contains(l, "meta"):
		return "clash"
	case strings.Contains(l, "sing-box") || strings.Contains(l, "singbox") || strings.Contains(l, "sfa") || strings.Contains(l, "sfi"):
		return "singbox"
	default:
		return "uri"
	}
}

func buildURIList(d *sql.DB, u *db.User, clients []*db.Client, over map[int64]bool) (string, error) {
	items, err := collectSubItems(d, u, clients, over)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, item := range items {
		uri, err := corecfg.ShareURI(item.in, item.c)
		if err != nil {
			continue
		}
		lines = append(lines, rewriteSubURI(uri, item.in))
	}
	return strings.Join(lines, "\n"), nil
}

// rewriteSubURI 把订阅链接的备注改成展示名。小火箭的 Mieru 列表用 profile 当节点名，
// 写死 default 时看到的就是 default。mita 不校验这个字段。复制链接仍保持 profile=default。
func rewriteSubURI(uri string, in *db.Inbound) string {
	if in == nil {
		return uri
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return uri
	}
	if in.Profile == corecfg.ProfileMieru {
		uri = replaceMieruProfile(uri, name)
	}
	return replaceFragment(uri, name)
}

func replaceMieruProfile(uri, name string) string {
	hash := strings.LastIndex(uri, "#")
	head, tail := uri, ""
	if hash >= 0 {
		head, tail = uri[:hash], uri[hash:]
	}
	const key = "profile=default"
	if !strings.Contains(head, key) {
		return uri
	}
	return strings.Replace(head, key, "profile="+subEscape(name), 1) + tail
}

func replaceFragment(uri, name string) string {
	esc := subEscape(name)
	i := strings.LastIndex(uri, "#")
	if i < 0 {
		return uri + "#" + esc
	}
	return uri[:i+1] + esc
}

func subEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func buildClash(d *sql.DB, u *db.User, clients []*db.Client, over map[int64]bool) (string, error) {
	items, err := collectSubItems(d, u, clients, over)
	if err != nil {
		return "", err
	}
	var names []string
	var b strings.Builder
	for _, item := range items {
		name, yaml, err := corecfg.ClashProxyYAML(item.in, item.c)
		if err != nil {
			continue
		}
		names = append(names, name)
		b.WriteString(yaml)
	}
	return corecfg.ClashDocument(names, b.String(), subRuleNames(d, u)), nil
}

func subRuleNames(d *sql.DB, u *db.User) []string {
	preset, _ := db.GetSetting(d, "sub_rule_preset")
	raw, _ := db.GetSetting(d, "sub_rule_categories")
	var custom []string
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &custom); err != nil {
			custom = nil
		}
	}
	userPreset := ""
	var userCustom []string
	if u != nil {
		userPreset = u.SubRulePreset
		userCustom = u.SubRuleCategories
	}
	_, names := corecfg.ResolveUserSubRules(userPreset, userCustom, preset, custom)
	return corecfg.ApplySiteFilterToSubRules(names, corecfg.SiteFilterFromUser(u).Mode)
}

func buildSingboxSub(d *sql.DB, u *db.User, clients []*db.Client, over map[int64]bool) ([]byte, error) {
	items, err := collectSubItems(d, u, clients, over)
	if err != nil {
		return nil, err
	}
	var tags []string
	var outs []any
	var endpoints []any
	for _, item := range items {
		in, c := item.in, item.c
		ob, err := corecfg.SingboxOutbound(in, c)
		if err != nil {
			if errors.Is(err, corecfg.ErrSkip) {
				continue
			}
			continue
		}
		if ep := corecfg.SingboxEndpoint(ob); ep != nil {
			endpoints = append(endpoints, ep)
			if tag, ok := ep["tag"].(string); ok && tag != "" {
				tags = append(tags, tag)
			}
			continue
		}
		outs = append(outs, ob)
		if tag, ok := ob["tag"].(string); ok {
			tags = append(tags, tag)
		}
	}
	doc := corecfg.SingboxClientDocument(outs, tags, subRuleNames(d, u))
	if len(endpoints) > 0 {
		doc["endpoints"] = endpoints
	}
	return json.MarshalIndent(doc, "", "  ")
}
