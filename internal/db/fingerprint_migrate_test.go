package db

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestChromeFingerprintMigratesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DELETE FROM schema_migrations WHERE version = ?`, "0027_fingerprint_safari.sql"); err != nil {
		t.Fatal(err)
	}
	tok, err := RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := CreateServer(d, "n1", "1.2.3.4", tok)
	if err != nil {
		t.Fatal(err)
	}
	chrome, err := CreateInbound(d, &Inbound{
		ServerID: srv.ID, Name: "chrome", Profile: "vless-reality-vision", Enabled: true, Port: 1,
		Settings: `{"fingerprint":"chrome","public_key":"abc"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	fox, err := CreateInbound(d, &Inbound{
		ServerID: srv.ID, Name: "firefox", Profile: "vless-reality-vision", Enabled: true, Port: 2,
		Settings: `{"fingerprint":"firefox"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	chrome, err = GetInbound(d, chrome.ID)
	if err != nil {
		t.Fatal(err)
	}
	fox, err = GetInbound(d, fox.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := settingsFingerprint(t, chrome.Settings); got != "safari" {
		t.Fatalf("chrome row %s", chrome.Settings)
	}
	if got := settingsFingerprint(t, fox.Settings); got != "firefox" {
		t.Fatalf("firefox row %s", fox.Settings)
	}
	if _, err := d.Exec(`UPDATE inbounds SET settings = json_set(settings, '$.fingerprint', 'chrome') WHERE id = ?`, chrome.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	chrome, err = GetInbound(d, chrome.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := settingsFingerprint(t, chrome.Settings); got != "chrome" {
		t.Fatalf("later chrome %s", chrome.Settings)
	}
}

func settingsFingerprint(t *testing.T, settings string) string {
	t.Helper()
	var st map[string]any
	if err := json.Unmarshal([]byte(settings), &st); err != nil {
		t.Fatal(err)
	}
	fp, _ := st["fingerprint"].(string)
	return fp
}
