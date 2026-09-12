package db

import (
	"bytes"
	"strings"
	"testing"
)

func TestBackupRoundTrip(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	admin, err := CreateUser(d, "admin", "hash-admin", "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateUser(d, "alice", "hash-alice", "user", "vip"); err != nil {
		t.Fatal(err)
	}
	tok, err := RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := CreateServer(d, "hk", "hk.example.com", tok)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := CreateCert(d, &Certificate{
		Name: "ex", CertPEM: "CERT", KeyPEM: "KEY", Domains: "example.com",
		Source: "acme-cf", AutoRenew: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cid := cert.ID
	in, err := CreateInbound(d, &Inbound{
		ServerID: srv.ID, Name: "vless-1", Profile: "vless-reality-vision",
		Protocol: "vless", Network: "tcp", Security: "reality", Core: "xray",
		Listen: "0.0.0.0", Port: 8443, Enabled: true, Settings: `{"dest":"www.microsoft.com:443"}`,
		CertID: &cid, LineKind: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	chain, err := CreateInbound(d, &Inbound{
		ServerID: srv.ID, Name: "entry", Profile: "ss2022",
		Protocol: "shadowsocks", Network: "tcp", Security: "none", Core: "xray",
		Listen: "0.0.0.0", Port: 8444, Enabled: true, Settings: `{}`,
		LineKind: "chain", ExitInboundID: &in.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreatePackage(d, "std", 0, 30, 0, "oneway"); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(d, "cf_api_token", "secret-cf"); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(d, "panel_url", "https://old.example"); err != nil {
		t.Fatal(err)
	}

	snap, err := Export(d, "0.1.29")
	if err != nil {
		t.Fatal(err)
	}
	sum := snap.Summary()
	if sum.Users != 2 || sum.Servers != 1 || sum.Inbounds != 2 || sum.Certs != 1 || sum.Packages != 1 || !sum.HasCFToken {
		t.Fatalf("summary %+v", sum)
	}
	raw, err := snap.EncodeGzip()
	if err != nil || len(raw) < 32 {
		t.Fatalf("gzip %d %v", len(raw), err)
	}
	got, err := DecodeSnapshot(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := CreateUser(d, "intruder", "x", "user", ""); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(d, "cf_api_token", "changed"); err != nil {
		t.Fatal(err)
	}

	if err := Import(d, got); err != nil {
		t.Fatal(err)
	}

	n, err := CountUsers(d)
	if err != nil || n != 2 {
		t.Fatalf("users %d %v", n, err)
	}
	if _, err := GetUserByName(d, "intruder"); err == nil {
		t.Fatal("intruder survived")
	}
	alice, err := GetUserByName(d, "alice")
	if err != nil || alice.Remark != "vip" || alice.PasswordHash != "hash-alice" {
		t.Fatalf("alice %+v %v", alice, err)
	}
	if admin.ID == 0 {
		t.Fatal("admin")
	}
	s2, err := GetServer(d, srv.ID)
	if err != nil || s2.Token != tok || s2.PublicHost != "hk.example.com" {
		t.Fatalf("server %+v %v", s2, err)
	}
	c2, err := GetCert(d, cert.ID)
	if err != nil || c2.KeyPEM != "KEY" || c2.CertPEM != "CERT" {
		t.Fatalf("cert %+v %v", c2, err)
	}
	entry, err := GetInbound(d, chain.ID)
	if err != nil || entry.ExitInboundID == nil || *entry.ExitInboundID != in.ID {
		t.Fatalf("chain %+v %v", entry, err)
	}
	cf, _ := GetSetting(d, "cf_api_token")
	if cf != "secret-cf" {
		t.Fatalf("token %q", cf)
	}
	url, _ := GetSetting(d, "panel_url")
	if url != "https://old.example" {
		t.Fatalf("url %q", url)
	}
	bob, err := CreateUser(d, "bob", "hash-bob", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if bob.ID <= alice.ID {
		t.Fatalf("sqlite sequence %d after alice %d", bob.ID, alice.ID)
	}

	if _, err := DecodeSnapshot(strings.NewReader("not-a-backup")); err == nil {
		t.Fatal("junk")
	}
	bad := &Snapshot{Format: BackupFormat, FormatVersion: 1, Tables: map[string][]map[string]any{
		"users": {{"username": "x", "role": "user"}},
	}}
	if err := ValidateSnapshot(bad); err == nil {
		t.Fatal("no admin")
	}
}

func TestBackupRejectsFutureFormat(t *testing.T) {
	body := `{"format":"liking-backup","format_version":99,"tables":{}}`
	_, err := DecodeSnapshot(strings.NewReader(body))
	if err == nil || !strings.Contains(err.Error(), "太新") {
		t.Fatalf("err %v", err)
	}
}
