package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"liking/internal/db"
	"liking/internal/version"
)

func TestInstallCmdRetriesOverHTTP1(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &Server{DB: d}
	req := httptest.NewRequest(http.MethodGet, "http://panel.example/api/servers/1/install", nil)
	cmd := s.installCmd(req, &db.Server{Token: "00112233445566778899aabbccddeeff"})
	if !strings.Contains(cmd, "--http1.1") || !strings.Contains(cmd, "--retry 5") {
		t.Fatalf("cmd %s", cmd)
	}
	if !strings.Contains(cmd, "http://panel.example/v1/install-agent") {
		t.Fatalf("cmd %s", cmd)
	}
	rec := httptest.NewRecorder()
	s.handleInstallAgent(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "__LIKING_VERSION__") || strings.Contains(body, "__LIKING_PANEL_URL__") {
		t.Fatal("placeholders left in install script")
	}
	if !strings.Contains(body, "LIKING_VERSION_BAKED="+version.Version) {
		t.Fatal("version not baked")
	}
	if !strings.Contains(body, "改从 GitHub 镜像下载") {
		t.Fatal("script has no mirror fallback")
	}
}

func TestAgentFallbackURL(t *testing.T) {
	u := agentFallbackURL("amd64")
	want := "https://github.com/cheesydui-cloud/liking/releases/download/v" + version.Version + "/liking-agent-linux-amd64"
	if u != want {
		t.Fatalf("fallback %s", u)
	}
}
