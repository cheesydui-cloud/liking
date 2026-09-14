package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMitaAssetURLs(t *testing.T) {
	if u := mitaDebURL("amd64"); u != fmt.Sprintf("https://github.com/enfein/mieru/releases/download/v%s/mita_%s_amd64.deb", mitaVer, mitaVer) {
		t.Fatal(u)
	}
	if u := mitaDebURL("arm64"); u != fmt.Sprintf("https://github.com/enfein/mieru/releases/download/v%s/mita_%s_arm64.deb", mitaVer, mitaVer) {
		t.Fatal(u)
	}
	if u := mitaTarURL("amd64"); u != fmt.Sprintf("https://github.com/enfein/mieru/releases/download/v%s/mita_%s_linux_amd64.tar.gz", mitaVer, mitaVer) {
		t.Fatal(u)
	}
	if u := mitaRPMURL("arm64"); u != fmt.Sprintf("https://github.com/enfein/mieru/releases/download/v%s/mita-%s-1.aarch64.rpm", mitaVer, mitaVer) {
		t.Fatal(u)
	}
}

func TestWithGHProxy(t *testing.T) {
	t.Setenv("LIKING_GITHUB_PROXY", "https://gh-proxy.com/")
	u := withGHProxy("https://github.com/enfein/mieru/releases/download/v3.36.1/mita_3.36.1_amd64.deb")
	if u != "https://gh-proxy.com/https://github.com/enfein/mieru/releases/download/v3.36.1/mita_3.36.1_amd64.deb" {
		t.Fatal(u)
	}
}

func TestWithGHProxyFromFile(t *testing.T) {
	t.Setenv("LIKING_GITHUB_PROXY", "")
	f := filepath.Join(t.TempDir(), "gh-proxy")
	if err := os.WriteFile(f, []byte("https://gh-proxy.com/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := githubProxyFile
	githubProxyFile = f
	t.Cleanup(func() { githubProxyFile = old })
	u := withGHProxy("https://github.com/XTLS/Xray-core/releases/download/v1/x.zip")
	want := "https://gh-proxy.com/https://github.com/XTLS/Xray-core/releases/download/v1/x.zip"
	if u != want {
		t.Fatal(u)
	}
}
