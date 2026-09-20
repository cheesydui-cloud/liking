package corecfg

import (
	"strings"
	"testing"
)

func TestApplySiteFilterToSubRulesDropsDomesticOnAllow(t *testing.T) {
	_, cats := ResolveUserSubRules("", nil, "balanced", nil)
	if !strings.Contains(strings.Join(cats, ","), "domestic") {
		t.Fatalf("balanced needs domestic %v", cats)
	}
	got := ApplySiteFilterToSubRules(cats, SiteFilterAllow)
	joined := strings.Join(got, ",")
	if strings.Contains(joined, "domestic") {
		t.Fatalf("allow still has domestic %v", got)
	}
	if !strings.Contains(joined, "private") || !strings.Contains(joined, "youtube") {
		t.Fatalf("allow dropped too much %v", got)
	}
	same := ApplySiteFilterToSubRules(cats, SiteFilterDeny)
	if strings.Join(same, ",") != strings.Join(cats, ",") {
		t.Fatalf("deny should keep %v", same)
	}
	doc := ClashDocument([]string{"jp"}, "  - name: \"jp\"\n    type: ss\n", got)
	if strings.Contains(doc, "国内服务") || strings.Contains(doc, "geolocation-cn") {
		t.Fatalf("allow clash still domestic\n%s", doc)
	}
	if !strings.Contains(doc, "私有网络") {
		t.Fatalf("allow clash dropped private\n%s", doc)
	}
	sb := SingboxClientDocument(nil, []string{"jp"}, got)
	route, _ := sb["route"].(map[string]any)
	raw, _ := route["rule_set"].([]any)
	for _, r := range raw {
		m, _ := r.(map[string]any)
		tag, _ := m["tag"].(string)
		if tag == "geolocation-cn" || tag == "cn-domains" || tag == "cn" {
			t.Fatalf("allow singbox still domestic %+v", raw)
		}
	}
}

func TestResolveUserSubRulesInheritAndOverride(t *testing.T) {
	preset, cats := ResolveUserSubRules("", nil, "minimal", nil)
	if preset != "minimal" || strings.Join(cats, ",") != "private,domestic" {
		t.Fatalf("inherit global %+v %v", preset, cats)
	}
	preset, cats = ResolveUserSubRules("inherit", nil, "custom", []string{"ads", "bogus"})
	if preset != "custom" || strings.Join(cats, ",") != "ads" {
		t.Fatalf("inherit custom %+v %v", preset, cats)
	}
	preset, cats = ResolveUserSubRules("balanced", nil, "minimal", nil)
	if preset != "balanced" {
		t.Fatalf("override preset %s", preset)
	}
	if strings.Join(cats, ",") != "ai,youtube,google,private,domestic,telegram,github" {
		t.Fatalf("override cats %v", cats)
	}
	preset, cats = ResolveUserSubRules("custom", []string{"ads", "private"}, "balanced", nil)
	if preset != "custom" || strings.Join(cats, ",") != "ads,private" {
		t.Fatalf("custom override %+v %v", preset, cats)
	}
}

func TestNormalizeUserSubRulePreset(t *testing.T) {
	if NormalizeUserSubRulePreset("") != "" || NormalizeUserSubRulePreset("inherit") != "" {
		t.Fatal("inherit should stay empty")
	}
	if NormalizeUserSubRulePreset("BALANCED") != "balanced" {
		t.Fatal("balanced")
	}
	if ValidUserSubRulePreset("nope") {
		t.Fatal("invalid")
	}
	if !ValidUserSubRulePreset("inherit") || !ValidUserSubRulePreset("") {
		t.Fatal("inherit valid")
	}
}

func TestResolveSubRulesDefaultBalanced(t *testing.T) {
	preset, cats := ResolveSubRules("", nil)
	if preset != "balanced" {
		t.Fatalf("preset %s", preset)
	}
	want := []string{"ai", "youtube", "google", "private", "domestic", "telegram", "github"}
	if strings.Join(cats, ",") != strings.Join(want, ",") {
		t.Fatalf("balanced %v", cats)
	}
}

func TestCategoriesForPreset(t *testing.T) {
	min := CategoriesForPreset("minimal")
	if strings.Join(min, ",") != "private,domestic" {
		t.Fatalf("minimal %v", min)
	}
	all := CategoriesForPreset("comprehensive")
	if len(all) < 18 {
		t.Fatalf("comprehensive %d %v", len(all), all)
	}
	for _, n := range all {
		if n == "ehentai" || n == "pttracker" {
			t.Fatalf("comprehensive should skip extra %s", n)
		}
	}
}

func TestNormalizeCategoryNamesKeepsCatalogOrder(t *testing.T) {
	got := NormalizeCategoryNames([]string{"github", "nope", "ads", "ads"})
	if strings.Join(got, ",") != "ads,github" {
		t.Fatalf("%v", got)
	}
}

func TestClashDocumentRules(t *testing.T) {
	_, cats := ResolveSubRules("balanced", nil)
	doc := ClashDocument([]string{"jp"}, "  - name: \"jp\"\n    type: ss\n", cats)
	for _, s := range []string{
		"proxy-groups:",
		`name: "节点选择"`,
		`name: "自动选择"`,
		`name: "回落"`,
		`name: "AI 服务"`,
		`name: "国内服务"`,
		`RULE-SET,youtube,油管视频`,
		`RULE-SET,cn,国内服务,no-resolve`,
		`MATCH,回落`,
		"rule-providers:",
		"category-ai-!cn",
		"geolocation-cn",
		"format: mrs",
		"gh-proxy.com",
		"MetaCubeX/meta-rules-dat",
	} {
		if !strings.Contains(doc, s) {
			t.Fatalf("missing %q\n%s", s, doc)
		}
	}
	if strings.Contains(doc, "MATCH,liking") {
		t.Fatal("old MATCH,liking")
	}
	if strings.Contains(doc, "广告拦截") {
		t.Fatal("ads should not be in balanced")
	}
}

func TestClashDocumentAdsRejectFirst(t *testing.T) {
	doc := ClashDocument(nil, "", []string{"ads"})
	want := "name: \"广告拦截\"\n    type: select\n    proxies:\n      - \"REJECT\"\n"
	if !strings.Contains(doc, want) {
		t.Fatalf("ads not REJECT-first\n%s", doc)
	}
	if !strings.Contains(doc, `RULE-SET,category-ads-all,广告拦截`) {
		t.Fatal(doc)
	}
}

func TestSingboxClientDocument(t *testing.T) {
	outs := []any{map[string]any{"type": "shadowsocks", "tag": "jp"}}
	doc := SingboxClientDocument(outs, []string{"jp"}, []string{"youtube", "private", "ads"})
	route, _ := doc["route"].(map[string]any)
	if route["final"] != GroupFallback {
		t.Fatalf("final %+v", route["final"])
	}
	rules, _ := route["rules"].([]any)
	if len(rules) == 0 {
		t.Fatal("no rules")
	}
	foundReject := false
	foundYT := false
	for _, r := range rules {
		m, _ := r.(map[string]any)
		if m["rule_set"] == "category-ads-all" && m["action"] == "reject" {
			foundReject = true
		}
		if m["rule_set"] == "youtube" && m["outbound"] == "油管视频" {
			foundYT = true
		}
	}
	if !foundReject || !foundYT {
		t.Fatalf("rules %+v", rules)
	}
	sets, _ := route["rule_set"].([]any)
	if len(sets) == 0 {
		t.Fatal("no rule_set")
	}
	u, _ := sets[0].(map[string]any)["url"].(string)
	if !strings.Contains(u, "/sing/geo/") || !strings.HasSuffix(u, ".srs") {
		t.Fatalf("url %s", u)
	}
}

func TestOverseasProviderNotColliding(t *testing.T) {
	cats := SelectedCategories([]string{"overseas", "pttracker"})
	if len(cats) != 2 {
		t.Fatalf("%d", len(cats))
	}
	if cats[0].SiteRules[0].Key == cats[1].SiteRules[0].Key {
		t.Fatalf("collision %s", cats[0].SiteRules[0].Key)
	}
	if cats[0].SiteRules[0].Key != "geolocation-!cn" {
		t.Fatalf("overseas key %s", cats[0].SiteRules[0].Key)
	}
}
