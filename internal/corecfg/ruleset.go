package corecfg

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// Rule categories and MetaCubeX rule-provider URLs from iluobei/miaomiaowu
// proxy_groups.json (sublink-worker). Group names in generated profiles use
// Label, without emoji.

//go:embed ruleset/proxy_groups.json
var proxyGroupsJSON []byte

const (
	GroupSelect   = "节点选择"
	GroupAuto     = "自动选择"
	GroupFallback = "回落"
)

const DefaultSubRulePreset = "balanced"

type RuleProvider struct {
	Key      string `json:"key"`
	Behavior string `json:"behavior"`
	Type     string `json:"type"`
	Format   string `json:"format"`
	URL      string `json:"url"`
	Path     string `json:"path"`
	Interval int    `json:"interval"`
}

type RuleCategory struct {
	Name      string         `json:"name"`
	Label     string         `json:"label"`
	Presets   []string       `json:"presets"`
	SiteRules []RuleProvider `json:"site_rules"`
	IPRules   []RuleProvider `json:"ip_rules"`
}

var ruleCatalog []RuleCategory
var ruleByName map[string]RuleCategory

func init() {
	if err := json.Unmarshal(proxyGroupsJSON, &ruleCatalog); err != nil {
		panic("corecfg: proxy_groups.json: " + err.Error())
	}
	ruleByName = make(map[string]RuleCategory, len(ruleCatalog))
	for _, c := range ruleCatalog {
		ruleByName[c.Name] = c
	}
}

func RuleCatalog() []RuleCategory {
	out := make([]RuleCategory, len(ruleCatalog))
	copy(out, ruleCatalog)
	return out
}

func SubRuleCatalogPublic() []map[string]any {
	out := make([]map[string]any, 0, len(ruleCatalog))
	for _, c := range ruleCatalog {
		presets := c.Presets
		if presets == nil {
			presets = []string{}
		}
		out = append(out, map[string]any{
			"name":    c.Name,
			"label":   c.Label,
			"presets": presets,
		})
	}
	return out
}

func ValidSubRulePreset(preset string) bool {
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case "", "minimal", "balanced", "comprehensive", "custom":
		return true
	default:
		return false
	}
}

func NormalizeSubRulePreset(preset string) string {
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case "minimal", "balanced", "comprehensive", "custom":
		return strings.ToLower(strings.TrimSpace(preset))
	default:
		return DefaultSubRulePreset
	}
}

func CategoriesForPreset(preset string) []string {
	preset = NormalizeSubRulePreset(preset)
	if preset == "custom" {
		return nil
	}
	var out []string
	for _, c := range ruleCatalog {
		for _, p := range c.Presets {
			if p == preset {
				out = append(out, c.Name)
				break
			}
		}
	}
	return out
}

func NormalizeCategoryNames(names []string) []string {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" {
			want[n] = true
		}
	}
	var out []string
	for _, c := range ruleCatalog {
		if want[c.Name] {
			out = append(out, c.Name)
		}
	}
	return out
}

func ResolveSubRules(preset string, custom []string) (string, []string) {
	preset = NormalizeSubRulePreset(preset)
	if preset == "custom" {
		return preset, NormalizeCategoryNames(custom)
	}
	return preset, CategoriesForPreset(preset)
}

func SelectedCategories(names []string) []RuleCategory {
	names = NormalizeCategoryNames(names)
	out := make([]RuleCategory, 0, len(names))
	for _, n := range names {
		if c, ok := ruleByName[n]; ok {
			out = append(out, c)
		}
	}
	return out
}
