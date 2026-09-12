package agent

import (
	"fmt"
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
