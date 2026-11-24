package corecfg

import (
	"database/sql"
	"fmt"
	"net"
	"sort"
	"strings"

	"liking/internal/db"
)

const MaxUserAllowIPs = 32

// NormalizeAllowIPs accepts IPs and CIDRs. A bare IP becomes /32 or /128.
// Empty input clears the list. Hostnames are rejected.
func NormalizeAllowIPs(lines []string) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	for _, raw := range lines {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == ';' || r == '\t'
		}) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			cidr, err := canonicalCIDR(part)
			if err != nil {
				return nil, fmt.Errorf("IP 无效：%s", part)
			}
			if _, ok := seen[cidr]; ok {
				continue
			}
			seen[cidr] = struct{}{}
			out = append(out, cidr)
		}
	}
	if len(out) > MaxUserAllowIPs {
		return nil, fmt.Errorf("IP 白名单最多 %d 条", MaxUserAllowIPs)
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out, nil
}

func canonicalCIDR(s string) (string, error) {
	if strings.Contains(s, "/") {
		ip, n, err := net.ParseCIDR(s)
		if err != nil || ip == nil {
			return "", fmt.Errorf("bad")
		}
		return n.String(), nil
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return "", fmt.Errorf("bad")
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String() + "/32", nil
	}
	return ip.String() + "/128", nil
}

func loadUserAllowIPs(d *sql.DB, clients map[int64][]*db.Client) map[int64][]string {
	out := map[int64][]string{}
	if d == nil {
		return out
	}
	seen := map[int64]struct{}{}
	for _, cs := range clients {
		for _, c := range cs {
			if c == nil || c.UserID < 1 {
				continue
			}
			if _, ok := seen[c.UserID]; ok {
				continue
			}
			seen[c.UserID] = struct{}{}
			u, err := db.GetUser(d, c.UserID)
			if err != nil || u == nil || len(u.AllowIPs) == 0 {
				continue
			}
			out[u.ID] = append([]string{}, u.AllowIPs...)
		}
	}
	return out
}

func allowOf(allows map[int64][]string, userID int64) []string {
	if len(allows) == 0 || userID < 1 {
		return nil
	}
	return allows[userID]
}

func xrayIPPass(tag, user, outbound string, cidrs []string) map[string]any {
	return map[string]any{
		"type":        "field",
		"inboundTag":  []string{tag},
		"user":        []string{user},
		"source":      cidrs,
		"outboundTag": outbound,
	}
}

func xrayIPBlock(tag, user string) map[string]any {
	return map[string]any{
		"type":        "field",
		"inboundTag":  []string{tag},
		"user":        []string{user},
		"outboundTag": "block",
	}
}

func xrayInboundBlock(tag string) map[string]any {
	return map[string]any{
		"type":        "field",
		"inboundTag":  []string{tag},
		"outboundTag": "block",
	}
}

// xrayChainIPRules lets listed source IPs through to the chain outbound and
// blocks that user from any other source. Other users on the same inbound
// still hit the catch-all.
func xrayChainIPRules(in *db.Inbound, clients []*db.Client, allows map[int64][]string, obTag string) []any {
	tag := inboundTag(in.ID)
	var rules []any
	for _, c := range clients {
		user := clientRouteUser(in, c)
		cidrs := allowOf(allows, userIDOf(c))
		if user == "" || len(cidrs) == 0 {
			continue
		}
		rules = append(rules, xrayIPPass(tag, user, obTag, cidrs))
		rules = append(rules, xrayIPBlock(tag, user))
	}
	rules = append(rules, map[string]any{
		"type": "field", "inboundTag": []string{tag}, "outboundTag": obTag,
	})
	return rules
}

func singIPPass(tag, user, outbound string, cidrs []string) map[string]any {
	return map[string]any{
		"action":         "route",
		"inbound":        []string{tag},
		"auth_user":      []string{user},
		"source_ip_cidr": cidrs,
		"outbound":       outbound,
	}
}

func singIPBlock(tag, user string) map[string]any {
	return map[string]any{
		"action":    "reject",
		"inbound":   []string{tag},
		"auth_user": []string{user},
	}
}

func singChainIPRules(in *db.Inbound, clients []*db.Client, allows map[int64][]string, obTag string) []any {
	tag := inboundTag(in.ID)
	var rules []any
	for _, c := range clients {
		user := clientRouteUser(in, c)
		cidrs := allowOf(allows, userIDOf(c))
		if user == "" || len(cidrs) == 0 {
			continue
		}
		rules = append(rules, singIPPass(tag, user, obTag, cidrs))
		rules = append(rules, singIPBlock(tag, user))
	}
	rules = append(rules, map[string]any{
		"action":   "route",
		"inbound":  []string{tag},
		"outbound": obTag,
	})
	return rules
}

func userIDOf(c *db.Client) int64 {
	if c == nil {
		return 0
	}
	return c.UserID
}
