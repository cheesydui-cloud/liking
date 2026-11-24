package corecfg

import (
	"encoding/json"
	"strings"
	"testing"

	"liking/internal/db"
)

func TestNormalizeAllowIPs(t *testing.T) {
	got, err := NormalizeAllowIPs([]string{
		"1.2.3.4",
		"1.2.3.4/32",
		"10.0.0.0/8",
		" 2001:db8::1 ",
		"10.1.0.0/16, 1.2.3.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1.2.3.4/32", "10.0.0.0/8", "10.1.0.0/16", "2001:db8::1/128"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v", got)
	}
	if _, err := NormalizeAllowIPs([]string{"example.com"}); err == nil {
		t.Fatal("hostname accepted")
	}
	if _, err := NormalizeAllowIPs([]string{"1.2.3.4/33"}); err == nil {
		t.Fatal("bad cidr accepted")
	}
	empty, err := NormalizeAllowIPs(nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty %+v %v", empty, err)
	}
	tooMany := make([]string, MaxUserAllowIPs+1)
	for i := range tooMany {
		tooMany[i] = "10.0.0." + itoa(i)
	}
	if _, err := NormalizeAllowIPs(tooMany); err == nil {
		t.Fatal("33 accepted")
	}
}

func TestXrayAllowIPLeavesPeerOpen(t *testing.T) {
	in := &db.Inbound{
		ID: 7, Core: CoreXray, Profile: ProfileVLESSRealityVision, Enabled: true,
		Listen: "0.0.0.0", Port: 443, LineKind: "direct", Name: "v", Settings: "{}",
	}
	locked := &db.Client{ID: 11, UserID: 1, Email: "u1.i7", UUID: "11111111-1111-4111-8111-111111111111", Enabled: true}
	open := &db.Client{ID: 12, UserID: 2, Email: "u2.i7", UUID: "22222222-2222-4222-8222-222222222222", Enabled: true}
	relay := &db.Client{ID: 13, UserID: 1, Email: "relay.7.1", UUID: "33333333-3333-4333-8333-333333333333", Enabled: true}
	allows := map[int64][]string{1: {"1.2.3.4/32"}}
	cfg, err := buildXray([]*db.Inbound{in}, map[int64][]*db.Client{7: {locked, open, relay}}, nil, nil, 60180, nil, nil, allows)
	if err != nil || cfg == nil {
		t.Fatal(err)
	}
	rules := xrayRules(t, cfg)
	tag := inboundTag(in.ID)
	var lockedPass, lockedBlock, openRule bool
	for _, m := range rules {
		if !hasStr(strList(m["inboundTag"]), tag) {
			continue
		}
		users := strList(m["user"])
		if hasStr(users, relay.Email) {
			t.Fatalf("relay filtered %+v", m)
		}
		if hasStr(users, locked.Email) && m["outboundTag"] == limitTag(AcctMark(locked, in.ID)) {
			if strings.Join(strList(m["source"]), ",") != "1.2.3.4/32" {
				t.Fatalf("locked source %+v", m["source"])
			}
			lockedPass = true
		}
		if hasStr(users, locked.Email) && m["outboundTag"] == "block" {
			lockedBlock = true
		}
		if hasStr(users, open.Email) {
			if m["source"] != nil {
				t.Fatalf("peer constrained %+v", m)
			}
			if m["outboundTag"] == "block" {
				t.Fatalf("peer blocked %+v", m)
			}
			openRule = true
		}
	}
	if !lockedPass || !lockedBlock || !openRule {
		t.Fatalf("pass=%v block=%v peer=%v", lockedPass, lockedBlock, openRule)
	}
	passAt, blockAt, directAt := -1, -1, -1
	for i, m := range rules {
		if !hasStr(strList(m["inboundTag"]), tag) {
			continue
		}
		users := strList(m["user"])
		if hasStr(users, locked.Email) && m["source"] != nil && passAt < 0 {
			passAt = i
		}
		if hasStr(users, locked.Email) && m["outboundTag"] == "block" && blockAt < 0 {
			blockAt = i
		}
		if m["user"] == nil && m["outboundTag"] == "direct" && directAt < 0 {
			directAt = i
		}
	}
	if passAt < 0 || blockAt < 0 || directAt < 0 || !(passAt < blockAt && blockAt < directAt) {
		t.Fatalf("order pass=%d block=%d direct=%d", passAt, blockAt, directAt)
	}
}

func TestXrayChainAllowIPBeforeCatchAll(t *testing.T) {
	in := &db.Inbound{ID: 4, Name: "ch"}
	locked := &db.Client{UserID: 1, Email: "u1.i4", Enabled: true}
	open := &db.Client{UserID: 2, Email: "u2.i4", Enabled: true}
	relay := &db.Client{UserID: 1, Email: "relay.4.9", Enabled: true}
	rules := xrayChainIPRules(in, []*db.Client{locked, open, relay}, map[int64][]string{1: {"9.9.9.9/32"}}, "ob-4")
	if len(rules) != 3 {
		t.Fatalf("rules %d %+v", len(rules), rules)
	}
	pass := rules[0].(map[string]any)
	block := rules[1].(map[string]any)
	catch := rules[2].(map[string]any)
	if strings.Join(strList(pass["user"]), ",") != locked.Email || strings.Join(strList(pass["source"]), ",") != "9.9.9.9/32" || pass["outboundTag"] != "ob-4" {
		t.Fatalf("pass %+v", pass)
	}
	if strings.Join(strList(block["user"]), ",") != locked.Email || block["outboundTag"] != "block" {
		t.Fatalf("block %+v", block)
	}
	if catch["user"] != nil || catch["outboundTag"] != "ob-4" {
		t.Fatalf("catch %+v", catch)
	}
}

func TestXrayOwnerPortAllowIP(t *testing.T) {
	in := &db.Inbound{
		ID: 9, Core: CoreXray, Profile: ProfilePortForward, Enabled: true,
		Listen: "0.0.0.0", Port: 18080, LineKind: "direct", OwnerUserID: 3, Name: "pf",
		Settings: `{"dest_host":"1.1.1.1","dest_port":80,"network":"tcp"}`,
	}
	owner := &db.Client{ID: 1, UserID: 3, Email: "u3.i9", Enabled: true, InboundID: 9}
	cfg, err := buildXray([]*db.Inbound{in}, map[int64][]*db.Client{9: {owner}}, nil, nil, 60180, nil, nil, map[int64][]string{3: {"10.0.0.0/8"}})
	if err != nil || cfg == nil {
		t.Fatal(err)
	}
	tag := inboundTag(in.ID)
	var sawSource, sawBlock bool
	for _, m := range xrayRules(t, cfg) {
		if !hasStr(strList(m["inboundTag"]), tag) {
			continue
		}
		if m["outboundTag"] == limitTag(AcctMark(owner, in.ID)) {
			if strings.Join(strList(m["source"]), ",") != "10.0.0.0/8" {
				t.Fatalf("source %+v", m)
			}
			sawSource = true
		}
		if m["outboundTag"] == "block" && m["user"] == nil {
			sawBlock = true
		}
	}
	if !sawSource || !sawBlock {
		t.Fatalf("source=%v block=%v", sawSource, sawBlock)
	}
}

func TestSingboxAllowIPLeavesPeerOpen(t *testing.T) {
	in := &db.Inbound{ID: 7, Core: CoreSingbox, Profile: ProfileSOCKS5, Enabled: true, Listen: "0.0.0.0", Port: 20000, LineKind: "direct"}
	locked := &db.Client{ID: 1, InboundID: 7, UserID: 3, Email: "u3.i7", Password: "pw", Enabled: true}
	open := &db.Client{ID: 2, InboundID: 7, UserID: 4, Email: "u4.i7", Password: "pw", Enabled: true}
	cfg, err := buildSingbox([]*db.Inbound{in}, map[int64][]*db.Client{7: {locked, open}}, nil, nil, 60190, 60191, nil, nil, map[int64][]string{3: {"1.2.3.4/32"}})
	if err != nil || cfg == nil {
		t.Fatal(err)
	}
	route, _ := cfg["route"].(map[string]any)
	raw, _ := route["rules"].([]any)
	var lockedPass, lockedBlock, openRule bool
	for _, item := range raw {
		m, _ := item.(map[string]any)
		users := strList(m["auth_user"])
		if hasStr(users, locked.Email) && m["outbound"] == limitTag(AcctMark(locked, in.ID)) {
			if strings.Join(strList(m["source_ip_cidr"]), ",") != "1.2.3.4/32" {
				t.Fatalf("source %+v", m)
			}
			lockedPass = true
		}
		if hasStr(users, locked.Email) && m["action"] == "reject" {
			lockedBlock = true
		}
		if hasStr(users, open.Email) {
			if m["source_ip_cidr"] != nil || m["action"] == "reject" {
				t.Fatalf("peer %+v", m)
			}
			openRule = true
		}
	}
	if !lockedPass || !lockedBlock || !openRule {
		t.Fatalf("pass=%v block=%v peer=%v", lockedPass, lockedBlock, openRule)
	}
}

func TestSiteAllowPassCarriesSource(t *testing.T) {
	in := &db.Inbound{ID: 7, Core: CoreXray, Enabled: true, Profile: ProfileVLESSRealityVision, LineKind: "direct"}
	c := &db.Client{ID: 1, UserID: 1, Email: "u1.i7", Enabled: true}
	filters := map[int64]SiteDeny{1: {Mode: SiteFilterAllow, Categories: []string{"tiktok"}}}
	pass, block := collectSiteAllowGroups([]*db.Inbound{in}, map[int64][]*db.Client{7: {c}}, filters, nil, map[int64][]string{1: {"1.2.3.4/32"}}, CoreXray)
	if len(pass) != 1 || strings.Join(pass[0].Source, ",") != "1.2.3.4/32" {
		t.Fatalf("pass %+v", pass)
	}
	if len(block) != 1 || !hasStr(block[0].Users, c.Email) {
		t.Fatalf("block %+v", block)
	}
	rules := xraySiteAllowPassRules(pass)
	if len(rules) == 0 {
		t.Fatal("no pass rules")
	}
	for _, raw := range rules {
		m := raw.(map[string]any)
		if strings.Join(strList(m["source"]), ",") != "1.2.3.4/32" {
			t.Fatalf("rule %+v", m)
		}
	}
}

func TestBuildHonorsUserAllowIPs(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, in, _ := provisionDenyFixture(t, d, ProfileVLESSRealityVision, "direct", 443)
	u.AllowIPs = []string{"1.2.3.4/32"}
	if err := db.UpdateUser(d, u); err != nil {
		t.Fatal(err)
	}
	bob, err := db.CreateUser(d, "bob", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if u.PackageID == nil {
		t.Fatal("package")
	}
	if err := db.BindUserPackage(d, bob.ID, *u.PackageID, 0); err != nil {
		t.Fatal(err)
	}
	bob, err = db.GetUser(d, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionUser(d, bob); err != nil {
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
	alice, err := db.GetClient(d, in.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := db.GetClient(d, in.ID, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	routing, _ := xray["routing"].(map[string]any)
	raw, _ := routing["rules"].([]any)
	var locked, open bool
	for _, item := range raw {
		m, _ := item.(map[string]any)
		users := strList(m["user"])
		if hasStr(users, alice.Email) && strList(m["source"]) != nil {
			locked = strings.Join(strList(m["source"]), ",") == "1.2.3.4/32"
		}
		if hasStr(users, peer.Email) && m["source"] != nil {
			t.Fatalf("peer source %+v", m)
		}
		if hasStr(users, peer.Email) {
			open = true
		}
	}
	if !locked || !open {
		t.Fatalf("locked=%v open=%v", locked, open)
	}
	fresh, err := db.GetUser(d, u.ID)
	if err != nil || strings.Join(fresh.AllowIPs, ",") != "1.2.3.4/32" {
		t.Fatalf("stored %+v %v", fresh, err)
	}
}

func xrayRules(t *testing.T, cfg map[string]any) []map[string]any {
	t.Helper()
	routing, _ := cfg["routing"].(map[string]any)
	raw, _ := routing["rules"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		m, _ := item.(map[string]any)
		out = append(out, m)
	}
	return out
}

func strList(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			s, ok := x.(string)
			if ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
