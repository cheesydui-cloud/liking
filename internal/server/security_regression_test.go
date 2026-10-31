package server

import (
	"testing"

	"liking/internal/db"
)

func TestPanelTLSConfigurationFailsClosed(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := db.SetSetting(d, "panel_tls_cert_id", "999"); err != nil {
		t.Fatal(err)
	}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.panelTLSConfig(); err == nil {
		t.Fatal("missing selected certificate must fail startup")
	}
}

func TestNormalizePanelURL(t *testing.T) {
	got, err := normalizePanelURL("https://panel.example.com:8443/")
	if err != nil || got != "https://panel.example.com:8443" {
		t.Fatalf("valid URL: %q %v", got, err)
	}
	for _, raw := range []string{
		"javascript:alert(1)",
		"https://panel.example.com/path",
		"https://panel.example.com/' bad",
		"https://user:pass@panel.example.com",
	} {
		if _, err := normalizePanelURL(raw); err == nil {
			t.Fatalf("accepted unsafe URL %q", raw)
		}
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("a'b"); got != `'a'"'"'b'` {
		t.Fatalf("quote %q", got)
	}
}
