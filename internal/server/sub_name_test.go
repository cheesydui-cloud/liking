package server

import (
	"database/sql"
	"net/url"
	"strings"
	"testing"

	"liking/internal/corecfg"
	"liking/internal/db"
)

func TestSubNodeNameUsesUsername(t *testing.T) {
	d := openSubNameDB(t)
	srv := mustSubServer(t, d, "203.0.113.10")
	alice := mustSubUser(t, d, "alice", "上海")
	in := mustSubInbound(t, d, srv.ID, "default", 39198)
	mustSubClient(t, d, alice, in)

	body := mustSubURI(t, d, alice)
	got := parseSubLine(t, body)
	if got.fragment != "alice" || got.profile != "alice" {
		t.Fatalf("name %+v body %s", got, body)
	}
	if strings.Contains(body, "profile=default") || strings.Contains(body, "#default") || strings.Contains(body, "上海") {
		t.Fatalf("old name leaked %s", body)
	}
	if !strings.Contains(body, "udp=1") || !strings.Contains(body, "203.0.113.10") {
		t.Fatalf("link %s", body)
	}

	clash := mustSubClash(t, d, alice)
	if !strings.Contains(clash, "name: \"alice\"") || strings.Contains(clash, "default") {
		t.Fatalf("clash %s", clash)
	}

	raw, err := corecfg.ShareURI(in, &db.Client{Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "profile=default") || !strings.HasSuffix(raw, "#default") {
		t.Fatalf("copy link changed %s", raw)
	}
}

func TestSubNodeNameMultiKeepsNode(t *testing.T) {
	d := openSubNameDB(t)
	srv := mustSubServer(t, d, "203.0.113.11")
	alice := mustSubUser(t, d, "alice", "")
	first := mustSubInbound(t, d, srv.ID, "default", 39198)
	second := mustSubInbound(t, d, srv.ID, "hk", 39199)
	mustSubClient(t, d, alice, first)
	mustSubClient(t, d, alice, second)

	body := mustSubURI(t, d, alice)
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines %d %s", len(lines), body)
	}
	want := []string{"alice · default", "alice · hk"}
	for i, line := range lines {
		got := parseSubLine(t, line)
		if got.fragment != want[i] || got.profile != want[i] {
			t.Fatalf("line %d %+v", i, got)
		}
		if strings.Contains(line, "profile=default") {
			t.Fatalf("profile default %s", line)
		}
	}
	clash := mustSubClash(t, d, alice)
	for _, name := range want {
		if !strings.Contains(clash, "name: \""+name+"\"") {
			t.Fatalf("clash missing %s in %s", name, clash)
		}
	}
}

func TestSubNodeNameChinese(t *testing.T) {
	d := openSubNameDB(t)
	srv := mustSubServer(t, d, "203.0.113.12")
	u := mustSubUser(t, d, "李雷", "备注")
	in := mustSubInbound(t, d, srv.ID, "default", 8964)
	mustSubClient(t, d, u, in)

	got := parseSubLine(t, mustSubURI(t, d, u))
	if got.fragment != "李雷" || got.profile != "李雷" {
		t.Fatalf("%+v", got)
	}
}

func TestRewriteSubURILeavesOtherProfiles(t *testing.T) {
	uri := "vless://uuid@203.0.113.10:443?security=reality#default"
	got := rewriteSubURI(uri, &db.Inbound{Name: "alice", Profile: corecfg.ProfileVLESSReality})
	if got != "vless://uuid@203.0.113.10:443?security=reality#alice" {
		t.Fatal(got)
	}
	if strings.Contains(got, "profile=") {
		t.Fatal(got)
	}
}

func openSubNameDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func mustSubServer(t *testing.T, d *sql.DB, host string) *db.Server {
	t.Helper()
	srv, err := db.CreateServer(d, host, host, "tok-"+host)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func mustSubUser(t *testing.T, d *sql.DB, name, remark string) *db.User {
	t.Helper()
	u, err := db.CreateUser(d, name, "hash", "user", remark)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mustSubInbound(t *testing.T, d *sql.DB, serverID int64, name string, port int) *db.Inbound {
	t.Helper()
	proto, network, security := corecfg.Spec(corecfg.ProfileMieru)
	in, err := db.CreateInbound(d, &db.Inbound{
		ServerID: serverID,
		Name:     name,
		Profile:  corecfg.ProfileMieru,
		Protocol: proto,
		Network:  network,
		Security: security,
		Core:     corecfg.CoreMita,
		Port:     port,
		Enabled:  true,
		Settings: `{"transport":"UDP"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func mustSubClient(t *testing.T, d *sql.DB, u *db.User, in *db.Inbound) {
	t.Helper()
	email := db.EmailFor(u.ID, in.ID)
	if err := db.UpsertClient(d, &db.Client{
		InboundID: in.ID,
		UserID:    u.ID,
		Email:     email,
		Username:  email,
		Password:  "secret",
		Enabled:   true,
	}); err != nil {
		t.Fatal(err)
	}
}

func mustSubURI(t *testing.T, d *sql.DB, u *db.User) string {
	t.Helper()
	clients, err := db.ListClientsByUser(d, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := buildURIList(d, u, clients, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(body) == "" {
		t.Fatal("empty sub")
	}
	return body
}

func mustSubClash(t *testing.T, d *sql.DB, u *db.User) string {
	t.Helper()
	clients, err := db.ListClientsByUser(d, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := buildClash(d, u, clients, nil)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

type subParsed struct {
	fragment string
	profile  string
}

func parseSubLine(t *testing.T, line string) subParsed {
	t.Helper()
	u, err := url.Parse(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	return subParsed{fragment: u.Fragment, profile: u.Query().Get("profile")}
}
