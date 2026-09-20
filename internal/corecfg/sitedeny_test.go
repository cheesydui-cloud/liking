package corecfg

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"liking/internal/db"
)

func TestNormalizeSiteDeny(t *testing.T) {
	d, err := NormalizeSiteDeny([]string{"GOOGLE", "nope", "youtube"}, []string{
		"https://WWW.Instagram.com/foo",
		"*.example.com",
		"10.0.0.0/8",
		"8.8.8.8",
		"# comment",
		"",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Categories) != 2 || d.Categories[0] != "google" || d.Categories[1] != "youtube" {
		t.Fatalf("cats %+v", d.Categories)
	}
	want := map[string]bool{"www.instagram.com": true, "example.com": true, "10.0.0.0/8": true, "8.8.8.8/32": true}
	if len(d.Domains) != len(want) {
		t.Fatalf("domains %+v", d.Domains)
	}
	for _, n := range d.Domains {
		if !want[n] {
			t.Fatalf("unexpected %s in %+v", n, d.Domains)
		}
	}
	tooMany := make([]string, MaxSiteDenyCustom+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("n%d.example.com", i)
	}
	if _, err := NormalizeSiteDeny(nil, tooMany); err == nil {
		t.Fatal("51 should fail")
	}
}

func TestResolveSiteDenySeparateGoogleYoutube(t *testing.T) {
	g, gIP := ResolveSiteDeny(SiteDeny{Categories: []string{"google"}})
	y, yIP := ResolveSiteDeny(SiteDeny{Categories: []string{"youtube"}})
	has := func(list []string, want string) bool {
		for _, s := range list {
			if s == want {
				return true
			}
		}
		return false
	}
	if !has(g, "google.com") || has(g, "youtube.com") || has(g, "youtu.be") {
		t.Fatalf("google domains %+v", g)
	}
	if !has(y, "youtube.com") || has(y, "google.com") || has(y, "gstatic.com") {
		t.Fatalf("youtube domains %+v", y)
	}
	if !has(gIP, "8.8.8.8/32") || len(yIP) != 0 {
		t.Fatalf("ips g=%+v y=%+v", gIP, yIP)
	}
}

func TestCollectSiteDenyGroups(t *testing.T) {
	in := &db.Inbound{ID: 7, Core: CoreXray, Enabled: true, Profile: ProfileVLESSRealityVision}
	c1 := &db.Client{UserID: 1, Email: "u1.i7", Enabled: true}
	c2 := &db.Client{UserID: 2, Email: "u2.i7", Enabled: true}
	c3 := &db.Client{UserID: 3, Email: "u3.i7", Enabled: true}
	relay := &db.Client{UserID: 1, Email: "relay.7.1", Enabled: true}
	off := &db.Client{UserID: 4, Email: "u4.i7", Enabled: false}
	denies := map[int64]SiteDeny{
		1: {Categories: []string{"google"}},
		2: {Categories: []string{"google"}},
		3: {Categories: []string{"youtube"}},
		4: {Categories: []string{"google"}},
	}
	groups := collectSiteDenyGroups([]*db.Inbound{in}, map[int64][]*db.Client{7: {c1, c2, c3, relay, off}}, denies, CoreXray)
	if len(groups) != 2 {
		t.Fatalf("groups %d %+v", len(groups), groups)
	}
	google := groups[0]
	if len(google.Users) != 2 {
		t.Fatalf("google users %+v", google.Users)
	}
	joined := strings.Join(google.Users, ",")
	if !strings.Contains(joined, "u1.i7") || !strings.Contains(joined, "u2.i7") || strings.Contains(joined, "relay.") {
		t.Fatalf("users %s", joined)
	}
	if len(google.Tags) != 1 || google.Tags[0] != inboundTag(7) {
		t.Fatalf("tags %+v", google.Tags)
	}
	rules := xraySiteDenyRules(groups)
	var domainRules, ipRules int
	for _, raw := range rules {
		m := raw.(map[string]any)
		if m["outboundTag"] != "block" {
			t.Fatalf("tag %+v", m)
		}
		if m["domain"] != nil {
			domainRules++
		}
		if m["ip"] != nil {
			ipRules++
		}
		if m["domain"] != nil && m["ip"] != nil {
			t.Fatal("domain and ip must be separate rules")
		}
	}
	if domainRules != 2 || ipRules != 1 {
		t.Fatalf("domain=%d ip=%d", domainRules, ipRules)
	}

	mieru := &db.Inbound{ID: 8, Core: CoreMita, Enabled: true, Profile: ProfileMieru}
	if n := collectSiteDenyGroups([]*db.Inbound{mieru}, map[int64][]*db.Client{8: {c1}}, denies, CoreMita); len(n) != 0 {
		t.Fatalf("mieru %+v", n)
	}
}

func TestBuildSiteDenyXrayOrderAndSniff(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, in, _ := provisionDenyFixture(t, d, ProfileVLESSRealityVision, "direct", 443)
	u.SiteDenyCategories = []string{"google"}
	if err := db.UpdateUser(d, u); err != nil {
		t.Fatal(err)
	}

	b, err := Build(d, in.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	var xray map[string]any
	if err := json.Unmarshal(b.Apply.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	foundSniff := false
	for _, raw := range xray["inbounds"].([]any) {
		m := raw.(map[string]any)
		if m["tag"] != inboundTag(in.ID) {
			continue
		}
		sn, _ := m["sniffing"].(map[string]any)
		if sn["enabled"] != true || sn["routeOnly"] != true {
			t.Fatalf("sniff %+v", sn)
		}
		foundSniff = true
	}
	if !foundSniff {
		t.Fatal("missing sniff")
	}

	c, err := db.GetClient(d, in.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	routing, _ := xray["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	apiAt, denyAt, directAt := -1, -1, -1
	var denyUsers []any
	var denyDomains []any
	for i, raw := range rules {
		m, _ := raw.(map[string]any)
		tags, _ := m["inboundTag"].([]any)
		if len(tags) == 1 && tags[0] == "api" {
			apiAt = i
			continue
		}
		if m["outboundTag"] == "block" {
			if denyAt < 0 {
				denyAt = i
				denyUsers, _ = m["user"].([]any)
				denyDomains, _ = m["domain"].([]any)
			}
			continue
		}
		if m["outboundTag"] == "direct" && len(tags) == 1 && tags[0] == inboundTag(in.ID) {
			directAt = i
		}
	}
	if apiAt != 0 || denyAt < 0 || directAt < 0 || !(apiAt < denyAt && denyAt < directAt) {
		t.Fatalf("order api=%d deny=%d direct=%d", apiAt, denyAt, directAt)
	}
	if len(denyUsers) != 1 || denyUsers[0] != c.Email {
		t.Fatalf("users %+v want %s", denyUsers, c.Email)
	}
	joined := fmtJoin(denyDomains)
	if !strings.Contains(joined, "domain:google.com") || strings.Contains(joined, "youtube.com") {
		t.Fatalf("domains %s", joined)
	}
}

func TestBuildSiteDenyBeforeChain(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "vless-in", Profile: ProfileVLESSRealityVision,
		Port: 8443, Enabled: true, LineKind: "chain",
		ExitURI:  "vless://11111111-1111-4111-8111-111111111111@203.0.113.10:443?encryption=none&security=reality&sni=www.microsoft.com&fp=chrome&pbk=abc&sid=abcd&type=tcp&flow=xtls-rprx-vision#ext",
		Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	u := provisionOnInbound(t, d, srv.ID, created)
	u.SiteDenyCategories = []string{"tiktok"}
	if err := db.UpdateUser(d, u); err != nil {
		t.Fatal(err)
	}
	b, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	var xray map[string]any
	if err := json.Unmarshal(b.Apply.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	routing, _ := xray["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	tag := inboundTag(created.ID)
	denyAt, chainAt := -1, -1
	for i, raw := range rules {
		m, _ := raw.(map[string]any)
		tags, _ := m["inboundTag"].([]any)
		if !hasTag(tags, tag) {
			continue
		}
		if m["outboundTag"] == "block" && denyAt < 0 {
			denyAt = i
			continue
		}
		if m["user"] == nil && m["outboundTag"] != "block" && chainAt < 0 {
			chainAt = i
		}
	}
	if denyAt < 0 || chainAt < 0 || denyAt >= chainAt {
		t.Fatalf("deny=%d chain=%d rules=%v", denyAt, chainAt, rules)
	}
}

func TestBuildSiteDenySingbox(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, in, _ := provisionDenyFixture(t, d, ProfileSOCKS5, "direct", 1080)
	u.SiteDenyCategories = []string{"youtube"}
	u.SiteDenyDomains = []string{"blocked.example"}
	if err := db.UpdateUser(d, u); err != nil {
		t.Fatal(err)
	}
	b, err := Build(d, in.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Apply.Singbox) == 0 {
		t.Fatal("singbox")
	}
	var sb map[string]any
	if err := json.Unmarshal(b.Apply.Singbox, &sb); err != nil {
		t.Fatal(err)
	}
	foundSniff := false
	for _, raw := range sb["inbounds"].([]any) {
		m := raw.(map[string]any)
		if m["tag"] != inboundTag(in.ID) {
			continue
		}
		if m["sniff"] != true || m["sniff_override_destination"] != false {
			t.Fatalf("sniff %+v", m)
		}
		foundSniff = true
	}
	if !foundSniff {
		t.Fatal("socks sniff")
	}
	route, _ := sb["route"].(map[string]any)
	rules, _ := route["rules"].([]any)
	if len(rules) == 0 {
		t.Fatal("no rules")
	}
	first, _ := rules[0].(map[string]any)
	if first["action"] != "reject" {
		t.Fatalf("first %+v", first)
	}
	suf, _ := first["domain_suffix"].([]any)
	joined := fmtJoin(suf)
	if !strings.Contains(joined, "youtube.com") || !strings.Contains(joined, "blocked.example") {
		t.Fatalf("suffix %s", joined)
	}
}

func TestSiteDenyCatalogSpeedtestAndIPLookup(t *testing.T) {
	pub := SiteDenyCatalogPublic()
	got := map[string]string{}
	for _, c := range pub {
		got[c["name"]] = c["label"]
	}
	if got["speedtest"] != "测速" || got["iplookup"] != "IP 查询" {
		t.Fatalf("catalog %+v", pub)
	}
	if got["openai"] != "ChatGPT" || got["telegram"] != "Telegram" {
		t.Fatalf("extra cats %+v", pub)
	}
	d, _ := ResolveSiteDeny(SiteDeny{Categories: []string{"speedtest"}})
	if !hasStr(d, "speedtest.net") || !hasStr(d, "fast.com") || !hasStr(d, "speed.cloudflare.com") {
		t.Fatalf("speedtest %+v", d)
	}
	ipd, _ := ResolveSiteDeny(SiteDeny{Categories: []string{"iplookup"}})
	if !hasStr(ipd, "ipinfo.io") || !hasStr(ipd, "icanhazip.com") || !hasStr(ipd, "cip.cc") {
		t.Fatalf("iplookup %+v", ipd)
	}
}

func TestResolveSiteAllowExtras(t *testing.T) {
	d, ips := ResolveSiteDeny(SiteDeny{Mode: SiteFilterAllow, Categories: []string{"tiktok"}})
	if !hasStr(d, "tiktok.com") || !hasStr(d, clientHealthHost()) {
		t.Fatalf("allow domains %+v", d)
	}
	if hasStr(d, "google.com") {
		t.Fatalf("tiktok allow leaked google %+v", d)
	}
	if !hasStr(ips, "1.1.1.1/32") || !hasStr(ips, "8.8.8.8/32") {
		t.Fatalf("allow dns %+v", ips)
	}
	gd, _ := ResolveSiteDeny(SiteDeny{Mode: SiteFilterDeny, Categories: []string{"google"}})
	if hasStr(gd, clientHealthHost()) {
		t.Fatalf("deny should not add health %+v", gd)
	}
}

func TestCollectSiteAllowGroups(t *testing.T) {
	in := &db.Inbound{ID: 7, Core: CoreXray, Enabled: true, Profile: ProfileVLESSRealityVision, LineKind: "direct"}
	c1 := &db.Client{UserID: 1, Email: "u1.i7", Enabled: true}
	c2 := &db.Client{UserID: 2, Email: "u2.i7", Enabled: true}
	c3 := &db.Client{UserID: 3, Email: "u3.i7", Enabled: true}
	filters := map[int64]SiteDeny{
		1: {Mode: SiteFilterAllow, Categories: []string{"tiktok"}},
		2: {Mode: SiteFilterAllow, Categories: []string{"tiktok"}},
		3: {Mode: SiteFilterDeny, Categories: []string{"google"}},
	}
	pass, block := collectSiteAllowGroups([]*db.Inbound{in}, map[int64][]*db.Client{7: {c1, c2, c3}}, filters, nil, CoreXray)
	if len(pass) != 1 {
		t.Fatalf("pass %d %+v", len(pass), pass)
	}
	if pass[0].Outbound != "direct" {
		t.Fatalf("ob %s", pass[0].Outbound)
	}
	if len(pass[0].Users) != 2 {
		t.Fatalf("pass users %+v", pass[0].Users)
	}
	joined := strings.Join(pass[0].Users, ",")
	if !strings.Contains(joined, "u1.i7") || !strings.Contains(joined, "u2.i7") || strings.Contains(joined, "u3.i7") {
		t.Fatalf("users %s", joined)
	}
	if !hasStr(pass[0].Domains, "tiktok.com") || !hasStr(pass[0].Domains, clientHealthHost()) {
		t.Fatalf("pass domains %+v", pass[0].Domains)
	}
	if len(block) != 1 || len(block[0].Users) != 2 {
		t.Fatalf("block %+v", block)
	}

	speeds := map[int64]int64{1: 10}
	pass, block = collectSiteAllowGroups([]*db.Inbound{in}, map[int64][]*db.Client{7: {c1, c2}}, filters, speeds, CoreXray)
	if len(pass) != 2 {
		t.Fatalf("speed split %d %+v", len(pass), pass)
	}
	obs := map[string]int{}
	for _, g := range pass {
		obs[g.Outbound]++
		if g.Outbound != "direct" && !strings.HasPrefix(g.Outbound, "limit-") {
			t.Fatalf("outbound %s", g.Outbound)
		}
	}
	if obs["direct"] != 1 || len(obs) != 2 {
		t.Fatalf("obs %+v", obs)
	}
	if len(block) != 1 {
		t.Fatalf("block after split %+v", block)
	}

	denyOnly := collectSiteDenyGroups([]*db.Inbound{in}, map[int64][]*db.Client{7: {c1, c2, c3}}, filters, CoreXray)
	if len(denyOnly) != 1 || !hasStr(denyOnly[0].Domains, "google.com") {
		t.Fatalf("deny groups %+v", denyOnly)
	}
}

func TestBuildSiteAllowXrayOrder(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, in, _ := provisionDenyFixture(t, d, ProfileVLESSRealityVision, "direct", 443)
	u.SiteFilterMode = SiteFilterAllow
	u.SiteDenyCategories = []string{"tiktok"}
	if err := db.UpdateUser(d, u); err != nil {
		t.Fatal(err)
	}
	b, err := Build(d, in.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	var xray map[string]any
	if err := json.Unmarshal(b.Apply.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	c, err := db.GetClient(d, in.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	routing, _ := xray["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	apiAt, passAt, blockAt, directAt := -1, -1, -1, -1
	var passDomains []any
	for i, raw := range rules {
		m, _ := raw.(map[string]any)
		tags, _ := m["inboundTag"].([]any)
		if len(tags) == 1 && tags[0] == "api" {
			apiAt = i
			continue
		}
		if m["outboundTag"] == "direct" && m["domain"] != nil && passAt < 0 {
			passAt = i
			passDomains, _ = m["domain"].([]any)
			continue
		}
		if m["outboundTag"] == "block" && m["domain"] == nil && m["ip"] == nil && blockAt < 0 {
			users, _ := m["user"].([]any)
			if len(users) == 1 && users[0] == c.Email {
				blockAt = i
			}
			continue
		}
		if m["outboundTag"] == "direct" && m["user"] == nil && len(tags) == 1 && tags[0] == inboundTag(in.ID) {
			directAt = i
		}
	}
	if apiAt != 0 || passAt < 0 || blockAt < 0 || directAt < 0 || !(apiAt < passAt && passAt < blockAt && blockAt < directAt) {
		t.Fatalf("order api=%d pass=%d block=%d direct=%d", apiAt, passAt, blockAt, directAt)
	}
	joined := fmtJoin(passDomains)
	if !strings.Contains(joined, "domain:tiktok.com") || !strings.Contains(joined, "domain:"+clientHealthHost()) {
		t.Fatalf("pass domains %s", joined)
	}
	if strings.Contains(joined, "youtube.com") {
		t.Fatalf("pass leaked youtube %s", joined)
	}
	for _, raw := range rules {
		m, _ := raw.(map[string]any)
		if m["domain"] != nil && m["ip"] != nil {
			t.Fatal("domain and ip must be separate")
		}
	}
}

func TestBuildSiteAllowBeforeChain(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "vless-in", Profile: ProfileVLESSRealityVision,
		Port: 8443, Enabled: true, LineKind: "chain",
		ExitURI:  "vless://11111111-1111-4111-8111-111111111111@203.0.113.10:443?encryption=none&security=reality&sni=www.microsoft.com&fp=chrome&pbk=abc&sid=abcd&type=tcp&flow=xtls-rprx-vision#ext",
		Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	u := provisionOnInbound(t, d, srv.ID, created)
	u.SiteFilterMode = SiteFilterAllow
	u.SiteDenyCategories = []string{"speedtest"}
	if err := db.UpdateUser(d, u); err != nil {
		t.Fatal(err)
	}
	b, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	var xray map[string]any
	if err := json.Unmarshal(b.Apply.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	routing, _ := xray["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	tag := inboundTag(created.ID)
	passAt, blockAt, chainAt := -1, -1, -1
	wantOB := outboundTag(created.ID)
	for i, raw := range rules {
		m, _ := raw.(map[string]any)
		tags, _ := m["inboundTag"].([]any)
		if !hasTag(tags, tag) {
			continue
		}
		if m["outboundTag"] == wantOB && m["domain"] != nil && passAt < 0 {
			passAt = i
			continue
		}
		if m["outboundTag"] == "block" && m["domain"] == nil && m["ip"] == nil && blockAt < 0 {
			blockAt = i
			continue
		}
		if m["user"] == nil && m["outboundTag"] != "block" && chainAt < 0 {
			chainAt = i
		}
	}
	if passAt < 0 || blockAt < 0 || chainAt < 0 || !(passAt < blockAt && blockAt < chainAt) {
		t.Fatalf("pass=%d block=%d chain=%d rules=%v", passAt, blockAt, chainAt, rules)
	}
}

func TestBuildSiteAllowSingbox(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, in, _ := provisionDenyFixture(t, d, ProfileSOCKS5, "direct", 1080)
	u.SiteFilterMode = SiteFilterAllow
	u.SiteDenyCategories = []string{"iplookup"}
	u.SiteDenyDomains = []string{"only.example"}
	if err := db.UpdateUser(d, u); err != nil {
		t.Fatal(err)
	}
	b, err := Build(d, in.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Apply.Singbox) == 0 {
		t.Fatal("singbox")
	}
	var sb map[string]any
	if err := json.Unmarshal(b.Apply.Singbox, &sb); err != nil {
		t.Fatal(err)
	}
	route, _ := sb["route"].(map[string]any)
	rules, _ := route["rules"].([]any)
	if len(rules) < 2 {
		t.Fatalf("rules %+v", rules)
	}
	passAt, blockAt := -1, -1
	for i, raw := range rules {
		m, _ := raw.(map[string]any)
		if m["outbound"] == "direct" && m["domain_suffix"] != nil && passAt < 0 {
			passAt = i
			suf, _ := m["domain_suffix"].([]any)
			joined := fmtJoin(suf)
			if !strings.Contains(joined, "ipinfo.io") || !strings.Contains(joined, "only.example") || !strings.Contains(joined, clientHealthHost()) {
				t.Fatalf("suffix %s", joined)
			}
			continue
		}
		if m["action"] == "reject" && m["domain_suffix"] == nil && m["ip_cidr"] == nil && blockAt < 0 {
			blockAt = i
		}
	}
	if passAt < 0 || blockAt < 0 || passAt >= blockAt {
		t.Fatalf("pass=%d block=%d rules=%v", passAt, blockAt, rules)
	}
}

func TestClientHealthCheckURL(t *testing.T) {
	if strings.Contains(ClientHealthCheckURL, "gstatic") || strings.Contains(ClientHealthCheckURL, "google") {
		t.Fatalf("health %s", ClientHealthCheckURL)
	}
	doc := ClashDocument([]string{"jp"}, "  - name: \"jp\"\n    type: ss\n", nil)
	if !strings.Contains(doc, "url: "+ClientHealthCheckURL) {
		t.Fatalf("clash health\n%s", doc)
	}
	if strings.Contains(doc, "gstatic.com") {
		t.Fatal("clash still uses gstatic")
	}
	sb := SingboxClientDocument(nil, []string{"jp"}, nil)
	found := false
	for _, raw := range sb["outbounds"].([]any) {
		m, _ := raw.(map[string]any)
		if m["type"] != "urltest" {
			continue
		}
		found = true
		if m["url"] != ClientHealthCheckURL {
			t.Fatalf("singbox health %+v", m)
		}
	}
	if !found {
		t.Fatal("no urltest")
	}
}

func provisionDenyFixture(t *testing.T, d *sql.DB, profile, lineKind string, port int) (*db.User, *db.Inbound, *db.Server) {
	t.Helper()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "n", Profile: profile,
		Port: port, Enabled: true, LineKind: lineKind, Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	u := provisionOnInbound(t, d, srv.ID, created)
	return u, created, srv
}

func provisionOnInbound(t *testing.T, d *sql.DB, serverID int64, in *db.Inbound) *db.User {
	t.Helper()
	pkg, err := db.CreatePackage(d, "std", 0, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPackageServers(d, pkg.ID, []int64{serverID}); err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "alice", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BindUserPackage(d, u.ID, pkg.ID, 0); err != nil {
		t.Fatal(err)
	}
	u, _ = db.GetUser(d, u.ID)
	if _, err := ProvisionUser(d, u); err != nil {
		t.Fatal(err)
	}
	return u
}

func hasTag(tags []any, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

func fmtJoin(v []any) string {
	var b strings.Builder
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strings.TrimSpace(toStr(x)))
	}
	return b.String()
}

func toStr(v any) string {
	s, _ := v.(string)
	return s
}

func hasStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
