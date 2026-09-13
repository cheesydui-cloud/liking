package corecfg

import "strings"

func SingboxClientDocument(proxyOutbounds []any, tags []string, selected []string) map[string]any {
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
			"url":       "https://www.gstatic.com/generate_204",
			"interval":  "5m",
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
	if len(cats) == 0 {
		doc["route"] = map[string]any{"final": GroupFallback}
		return doc
	}

	var ruleSets []any
	seen := map[string]bool{}
	var rules []any
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
	route := map[string]any{
		"rules": rules,
		"final": GroupFallback,
	}
	if len(ruleSets) > 0 {
		route["rule_set"] = ruleSets
	}
	doc["route"] = route
	return doc
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
