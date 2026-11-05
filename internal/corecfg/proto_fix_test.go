package corecfg

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"liking/internal/db"
)

func b64n(n int) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{byte(n)}, n))
}

func TestSSKeyLenKnownMethods(t *testing.T) {
	if SSKeyLen("2022-blake3-aes-128-gcm") != 16 {
		t.Fatal("128")
	}
	if SSKeyLen("2022-blake3-aes-256-gcm") != 32 {
		t.Fatal("256")
	}
	if SSKeyLen("2022-blake3-chacha20-poly1305") != 32 {
		t.Fatal("chacha")
	}
	if KnownSS2022Method("aes-256-gcm") || !KnownSS2022Method("2022-blake3-chacha20-poly1305") {
		t.Fatal("known")
	}
}

func TestNormalizeSS2022KeepsMatchingKey(t *testing.T) {
	pw := b64n(16)
	in := &db.Inbound{
		Name: "ss", Profile: ProfileSS2022, Port: 8388, LineKind: "direct",
		Settings: fmt.Sprintf(`{"method":"2022-blake3-aes-128-gcm","server_password":"%s"}`, pw),
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if got := ParseSettings(in.Settings).String("server_password"); got != pw {
		t.Fatalf("rotated %s", got)
	}
	bad := &db.Inbound{
		Name: "ss", Profile: ProfileSS2022, Port: 8388, LineKind: "direct",
		Settings: `{"method":"not-a-method"}`,
	}
	if err := Normalize(bad, nil); err == nil || !strings.Contains(err.Error(), "加密方法无效") {
		t.Fatal(err)
	}
}

func TestNormalizeSS2022MethodSwitchRegenerates(t *testing.T) {
	pw := b64n(16)
	in := &db.Inbound{
		Name: "ss", Profile: ProfileSS2022, Port: 8388, LineKind: "direct",
		Settings: fmt.Sprintf(`{"method":"2022-blake3-aes-256-gcm","server_password":"%s"}`, pw),
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	got := ParseSettings(in.Settings).String("server_password")
	if got == pw || !ssPasswordOK(got, 32) {
		t.Fatalf("server %s", got)
	}
	chacha := &db.Inbound{
		Name: "ss", Profile: ProfileSS2022, Port: 8388, LineKind: "direct",
		Settings: `{"method":"2022-blake3-chacha20-poly1305"}`,
	}
	if err := Normalize(chacha, nil); err != nil {
		t.Fatal(err)
	}
	if !ssPasswordOK(ParseSettings(chacha.Settings).String("server_password"), 32) {
		t.Fatal("chacha key")
	}
}

func TestNormalizeChainSS2022RelayKey(t *testing.T) {
	short := b64n(16)
	long := b64n(32)
	land := &db.Inbound{
		ID: 9, Name: "land", Profile: ProfileSS2022, Port: 8388, LineKind: "direct",
		Settings: fmt.Sprintf(`{"method":"2022-blake3-aes-256-gcm","server_password":"%s"}`, long),
	}
	entry := &db.Inbound{
		Name: "in", Profile: ProfileVLESSReality, Port: 443, LineKind: "chain",
		Settings: fmt.Sprintf(`{"relay_password":"%s","dest":"www.microsoft.com:443"}`, short),
	}
	entry.ExitInboundID = &land.ID
	if err := Normalize(entry, land); err != nil {
		t.Fatal(err)
	}
	got := ParseSettings(entry.Settings).String("relay_password")
	if got == short || !ssPasswordOK(got, 32) {
		t.Fatalf("relay %s", got)
	}
	if ParseSettings(land.Settings).String("server_password") != long {
		t.Fatal("landing key rotated")
	}
}

func TestApplyHopRelaysSS2022Key(t *testing.T) {
	short := b64n(16)
	long := b64n(32)
	land := &db.Inbound{
		ID: 9, Profile: ProfileSS2022, LineKind: "direct",
		Settings: fmt.Sprintf(`{"method":"2022-blake3-aes-256-gcm","server_password":"%s"}`, long),
	}
	st := Settings{"hops": []any{map[string]any{
		"kind": "panel", "inbound_id": int64(9), "relay_password": short,
	}}}
	if err := ApplyHopRelays(st, map[int64]*db.Inbound{9: land}, 1, 2); err != nil {
		t.Fatal(err)
	}
	hops := ParseHops(st)
	if len(hops) != 1 || hops[0].RelayPassword == short || !ssPasswordOK(hops[0].RelayPassword, 32) {
		t.Fatalf("%+v", hops)
	}
}

func TestRepairSS2022RewritesOnlyBadKeys(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	short := b64n(16)
	long := b64n(32)
	land := &db.Inbound{
		ServerID: srv.ID, Name: "ss", Profile: ProfileSS2022, Listen: "0.0.0.0",
		Port: 8388, Enabled: true, LineKind: "direct",
		Settings: fmt.Sprintf(`{"method":"2022-blake3-aes-256-gcm","server_password":"%s"}`, short),
	}
	land, err = db.CreateInbound(d, land)
	if err != nil {
		t.Fatal(err)
	}
	mid := &db.Inbound{
		ServerID: srv.ID, Name: "mid", Profile: ProfileSS2022, Listen: "0.0.0.0",
		Port: 8389, Enabled: true, LineKind: "direct",
		Settings: fmt.Sprintf(`{"method":"2022-blake3-aes-256-gcm","server_password":"%s"}`, long),
	}
	mid, err = db.CreateInbound(d, mid)
	if err != nil {
		t.Fatal(err)
	}
	plain := &db.Inbound{
		ServerID: srv.ID, Name: "old", Profile: ProfileSS2022, Listen: "0.0.0.0",
		Port: 8390, Enabled: true, LineKind: "direct",
		Settings: fmt.Sprintf(`{"server_password":"%s"}`, short),
	}
	plain, err = db.CreateInbound(d, plain)
	if err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "alice", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertClient(d, &db.Client{
		InboundID: land.ID, UserID: u.ID, Email: "a", Password: short, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	entry := &db.Inbound{
		ServerID: srv.ID, Name: "chain", Profile: ProfileVLESSReality, Listen: "0.0.0.0",
		Port: 443, Enabled: true, LineKind: "chain", ExitInboundID: &land.ID,
		Settings: fmt.Sprintf(`{"dest":"www.microsoft.com:443","relay_password":"%s","hops":[{"kind":"panel","inbound_id":%d,"relay_password":"%s"}]}`, short, mid.ID, short),
	}
	entry, err = db.CreateInbound(d, entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := RepairSS2022(d); err != nil {
		t.Fatal(err)
	}
	land, _ = db.GetInbound(d, land.ID)
	mid, _ = db.GetInbound(d, mid.ID)
	plain, _ = db.GetInbound(d, plain.ID)
	entry, _ = db.GetInbound(d, entry.ID)
	landPW := ParseSettings(land.Settings).String("server_password")
	if landPW == short || !ssPasswordOK(landPW, 32) {
		t.Fatalf("land %s", landPW)
	}
	if ParseSettings(mid.Settings).String("server_password") != long {
		t.Fatal("valid 256 key rotated")
	}
	pst := ParseSettings(plain.Settings)
	if pst.String("method") != "2022-blake3-aes-128-gcm" || pst.String("server_password") != short {
		t.Fatalf("empty method %+v", pst)
	}
	clients, err := db.ListClientsByInbound(d, land.ID)
	if err != nil || len(clients) != 1 || clients[0].Password == short || !ssPasswordOK(clients[0].Password, 32) {
		t.Fatalf("client %+v %v", clients, err)
	}
	est := ParseSettings(entry.Settings)
	if est.String("relay_password") == short || !ssPasswordOK(est.String("relay_password"), 32) {
		t.Fatalf("exit relay %s", est.String("relay_password"))
	}
	hops := ParseHops(est)
	if len(hops) != 1 || hops[0].RelayPassword == short || !ssPasswordOK(hops[0].RelayPassword, 32) {
		t.Fatalf("hop %+v", hops)
	}
	againLand, againClient, againRelay, againHop := landPW, clients[0].Password, est.String("relay_password"), hops[0].RelayPassword
	if err := RepairSS2022(d); err != nil {
		t.Fatal(err)
	}
	land, _ = db.GetInbound(d, land.ID)
	entry, _ = db.GetInbound(d, entry.ID)
	clients, _ = db.ListClientsByInbound(d, land.ID)
	if ParseSettings(land.Settings).String("server_password") != againLand || clients[0].Password != againClient {
		t.Fatal("second repair rotated keys")
	}
	est = ParseSettings(entry.Settings)
	if est.String("relay_password") != againRelay || ParseHops(est)[0].RelayPassword != againHop {
		t.Fatal("second repair rotated relays")
	}
}

func TestProvisionUserRekeysBadSS2022(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	long := b64n(32)
	short := b64n(16)
	in := &db.Inbound{
		ServerID: srv.ID, Name: "ss", Profile: ProfileSS2022, Listen: "0.0.0.0",
		Port: 8388, Enabled: true, LineKind: "direct",
		Settings: fmt.Sprintf(`{"method":"2022-blake3-aes-256-gcm","server_password":"%s"}`, long),
	}
	in, err = db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "root", "h", "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	email := db.EmailFor(u.ID, in.ID)
	if err := db.UpsertClient(d, &db.Client{
		InboundID: in.ID, UserID: u.ID, Email: email, Password: short, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionUser(d, u); err != nil {
		t.Fatal(err)
	}
	c, err := db.GetClient(d, in.ID, u.ID)
	if err != nil || c.Password == short || !ssPasswordOK(c.Password, 32) {
		t.Fatalf("rekey %+v %v", c, err)
	}
	kept := c.Password
	if _, err := ProvisionUser(d, u); err != nil {
		t.Fatal(err)
	}
	c, _ = db.GetClient(d, in.ID, u.ID)
	if c.Password != kept {
		t.Fatal("second provision rotated a good key")
	}
}

func TestMieruConflictOnePerServer(t *testing.T) {
	a := &db.Inbound{ID: 1, ServerID: 7, Profile: ProfileMieru, Enabled: true}
	b := &db.Inbound{ID: 2, ServerID: 7, Profile: ProfileMieru, Enabled: true}
	other := &db.Inbound{ID: 3, ServerID: 8, Profile: ProfileMieru, Enabled: true}
	if err := MieruConflict(a, []*db.Inbound{a, other}); err != nil {
		t.Fatal(err)
	}
	err := MieruConflict(a, []*db.Inbound{b})
	if err == nil || !strings.Contains(err.Error(), "一台实例只能有一条 Mieru") {
		t.Fatal(err)
	}
	b.Enabled = false
	if err := MieruConflict(a, []*db.Inbound{b}); err != nil {
		t.Fatal(err)
	}
	off := &db.Inbound{ID: 4, ServerID: 7, Profile: ProfileMieru, Enabled: false}
	if err := MieruConflict(off, []*db.Inbound{a}); err != nil {
		t.Fatal(err)
	}
	x := &db.Inbound{ID: 1, ServerID: 7, Core: CoreMita, Profile: ProfileMieru, Enabled: true, Port: 1, LineKind: "direct", Settings: `{"transport":"BOTH"}`}
	y := &db.Inbound{ID: 2, ServerID: 7, Core: CoreMita, Profile: ProfileMieru, Enabled: true, Port: 2, LineKind: "direct", Settings: `{"transport":"TCP"}`}
	if _, err := buildMita([]*db.Inbound{x, y}, nil); err == nil || !strings.Contains(err.Error(), "一台实例只能有一条 Mieru") {
		t.Fatal(err)
	}
	y.Enabled = false
	cfg, err := buildMita([]*db.Inbound{x, y}, nil)
	if err != nil || cfg == nil {
		t.Fatalf("disabled second %v", err)
	}
}

func TestSocksInboundNoHoldPassword(t *testing.T) {
	in := &db.Inbound{ID: 3, Listen: "0.0.0.0", Port: 1080, Settings: `{"hold_user":"hold","hold_pass":"hold"}`}
	cfg, err := singSocksInbound(in, []*db.Client{{Enabled: true, Email: "a@b", Password: "secret"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	users, _ := cfg["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("users %+v", cfg["users"])
	}
	uu, _ := users[0].(map[string]any)
	if uu["username"] != "a@b" || uu["password"] != "secret" {
		t.Fatalf("%+v", uu)
	}
	cfg, err = singSocksInbound(in, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	users, _ = cfg["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("empty %+v", cfg["users"])
	}
	uu, _ = users[0].(map[string]any)
	name, _ := uu["username"].(string)
	pass, _ := uu["password"].(string)
	if name == "" || name == "hold" || pass == "" || pass == "hold" || !strings.HasPrefix(name, "hold.") {
		t.Fatalf("fallback %s %s", name, pass)
	}
	in.Settings = `{"hold_user":"keep.me","hold_pass":"longsecret"}`
	cfg, err = singSocksInbound(in, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	users, _ = cfg["users"].([]any)
	uu, _ = users[0].(map[string]any)
	if uu["username"] != "keep.me" || uu["password"] != "longsecret" {
		t.Fatalf("hold creds %+v", uu)
	}
}

func TestRejectAppleRealityDest(t *testing.T) {
	blocked := []string{
		"www.apple.com:443", "apple.com:443", "store.apple.com:443",
		"www.icloud.com:443", "icloud.com", "foo.icloud.com:443",
	}
	for _, dest := range blocked {
		in := &db.Inbound{
			Name: "r", Profile: ProfileVLESSReality, Port: 443, LineKind: "direct",
			Settings: fmt.Sprintf(`{"dest":"%s"}`, dest),
		}
		err := Normalize(in, nil)
		if err == nil || !strings.Contains(err.Error(), "苹果") {
			t.Fatalf("%s %v", dest, err)
		}
	}
	local := &db.Inbound{
		Name: "r", Profile: ProfileVLESSReality, Port: 443, LineKind: "direct",
		Settings: `{"dest":"127.0.0.1:443"}`,
	}
	if err := Normalize(local, nil); err == nil || !strings.Contains(err.Error(), "本机") {
		t.Fatal(err)
	}
	for _, dest := range []string{"www.microsoft.com:443", "notapple.com:443", "apple.com.example.com:443"} {
		in := &db.Inbound{
			Name: "r", Profile: ProfileVLESSReality, Port: 443, LineKind: "direct",
			Settings: fmt.Sprintf(`{"dest":"%s"}`, dest),
		}
		if err := Normalize(in, nil); err != nil {
			t.Fatalf("%s %v", dest, err)
		}
	}
}
