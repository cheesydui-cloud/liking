package corecfg

import "strings"

func SingboxClientDocument(proxyOutbounds []any, tags []string, selected []string, leak bool) map[string]any {
	if tags == nil {
		tags = []string{}
	}
	autoMembers := append([]string{}, tags...)
	if len(autoMembers) == 0 {
		autoMembers = []string{"direct"}
	}
	selectMembers := append([]string{GroupAuto}, tags...)
	selectMembers = append(selectMembers, "direct")

	outs := []any{
		map[string]any{"type": "selector", "tag": GroupSelect, "outbounds": selectMembers},
		map[string]any{
			"type":      "urltest",
			"tag":       GroupAuto,
			"outbounds": autoMembers,
			"url":       ClientHealthCheckURL,
			"interval":  "5m",
			"timeout":   "10s",
		},
	}
	cats := SelectedCategories(selected)
	for _, c := range cats {
		if c.Name == "ads" {
			continue
		}
		outs = append(outs, map[string]any{
			"type":      "selector",
			"tag":       c.Label,
			"outbounds": categorySingMembers(c.Name, tags),
		})
	}
	outs = append(outs, map[string]any{
		"type":      "selector",
		"tag":       GroupFallback,
		"outbounds": []string{GroupSelect, "direct"},
	})
	outs = append(outs, proxyOutbounds...)
	outs = append(outs, map[string]any{"type": "direct", "tag": "direct"})

	doc := map[string]any{"outbounds": outs}
	if len(cats) == 0 && !leak {
		doc["route"] = map[string]any{"final": GroupFallback}
		return doc
	}

	var ruleSets []any
	seen := map[string]bool{}
	var rules []any
	if leak {
		rules = append(rules,
			map[string]any{"action": "sniff"},
			map[string]any{"protocol": "dns", "action": "hijack-dns"},
			map[string]any{"ip_is_private": true, "outbound": "direct"},
			map[string]any{"network": "udp", "port": []int{3478, 19302}, "outbound": GroupSelect},
		)
	}
	for _, c := range cats {
		if c.Name == "ads" {
			for _, p := range c.SiteRules {
				if tag, ok := appendSingRuleSet(&ruleSets, seen, p); ok {
					rules = append(rules, map[string]any{"rule_set": tag, "action": "reject"})
				}
			}
			continue
		}
		for _, p := range c.SiteRules {
			if tag, ok := appendSingRuleSet(&ruleSets, seen, p); ok {
				rules = append(rules, map[string]any{"rule_set": tag, "outbound": c.Label})
			}
		}
	}
	for _, c := range cats {
		if c.Name == "ads" {
			continue
		}
		for _, p := range c.IPRules {
			if tag, ok := appendSingRuleSet(&ruleSets, seen, p); ok {
				rules = append(rules, map[string]any{"rule_set": tag, "outbound": c.Label})
			}
		}
	}
	final := GroupFallback
	if leak {
		final = GroupSelect
	}
	route := map[string]any{
		"rules": rules,
		"final": final,
	}
	if leak {
		route["auto_detect_interface"] = true
		route["default_domain_resolver"] = "dns-remote"
	}
	if len(ruleSets) > 0 {
		route["rule_set"] = ruleSets
	}
	doc["route"] = route
	if leak {
		doc["inbounds"] = []any{map[string]any{
			"type":          "tun",
			"tag":           "tun-in",
			"inet4_address": "172.19.0.1/30",
			"auto_route":    true,
			"strict_route":  true,
			"stack":         "mixed",
		}}
		doc["dns"] = singboxLeakDNS()
	}
	return doc
}

func singboxLeakDNS() map[string]any {
	return map[string]any{
		"servers": []any{
			map[string]any{
				"type":        "https",
				"tag":         "dns-remote",
				"server":      "1.1.1.1",
				"server_port": 443,
				"path":        "/dns-query",
				"detour":      GroupSelect,
			},
			map[string]any{
				"type":        "udp",
				"tag":         "dns-bootstrap",
				"server":      "1.1.1.1",
				"server_port": 53,
				"detour":      "direct",
			},
		},
		"rules": []any{
			map[string]any{"outbound": "any", "server": "dns-bootstrap"},
		},
		"final":    "dns-remote",
		"strategy": "ipv4_only",
	}
}

func categorySingMembers(name string, tags []string) []string {
	if name == "private" || name == "domestic" {
		return append([]string{"direct", GroupSelect}, tags...)
	}
	return append([]string{GroupSelect, "direct", GroupAuto}, tags...)
}

func appendSingRuleSet(dst *[]any, seen map[string]bool, p RuleProvider) (string, bool) {
	if p.Format != "mrs" || p.Key == "" || seen[p.Key] {
		return "", false
	}
	u := singRuleSetURL(p.URL)
	if u == "" {
		return "", false
	}
	seen[p.Key] = true
	*dst = append(*dst, map[string]any{
		"type":            "remote",
		"tag":             p.Key,
		"format":          "binary",
		"url":             u,
		"download_detour": "direct",
		"update_interval": "24h",
	})
	return p.Key, true
}

func singRuleSetURL(u string) string {
	if !strings.Contains(u, "/geo/") || !strings.HasSuffix(u, ".mrs") {
		return ""
	}
	u = strings.Replace(u, "/refs/heads/meta/geo/", "/sing/geo/", 1)
	u = strings.TrimSuffix(u, ".mrs") + ".srs"
	return u
}
