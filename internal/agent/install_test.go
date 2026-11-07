package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestChecksumInLine(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	for _, line := range []string{
		sum + "  asset.tar.gz",
		"SHA256 (asset.tar.gz) = " + sum,
		"SHA2-256= " + sum,
	} {
		if got := checksumInLine(line); got != sum {
			t.Fatalf("checksum %q = %q", line, got)
		}
	}
	if got := checksumInLine("MD5 = deadbeef"); got != "" {
		t.Fatalf("accepted short checksum %q", got)
	}
}

func TestWriteCoreBinaryPreservesExistingFileOnFailure(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "core")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeCoreBinary(bytes.NewReader([]byte("12345")), dest, "core", 4); err == nil {
		t.Fatal("expected size error")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("existing binary changed: %q", got)
	}
}

func TestWithGHProxy(t *testing.T) {
	t.Setenv("LIKING_GITHUB_PROXY", "https://gh-proxy.com/")
	u := withGHProxy("https://github.com/enfein/mieru/releases/download/v3.36.1/mita_3.36.1_amd64.deb")
	if u != "https://gh-proxy.com/https://github.com/enfein/mieru/releases/download/v3.36.1/mita_3.36.1_amd64.deb" {
		t.Fatal(u)
	}
}

func TestAssetSHA256FromRelease(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	other := strings.Repeat("cd", 32)
	body := []byte(`{"assets":[
		{"name":"sing-box-1.14.0-linux-amd64.tar.gz.1","digest":"sha256:` + other + `","state":"uploaded"},
		{"name":"sing-box-1.14.0-linux-amd64.tar.gz","digest":"SHA256:` + sum + `","state":"uploaded"},
		{"name":"sing-box-1.14.0-linux-amd64.tar.gz","digest":"sha256:` + other + `","state":"starter"}
	]}`)
	got, err := assetSHA256FromRelease(body, "sing-box-1.14.0-linux-amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != sum {
		t.Fatalf("got %s", got)
	}
}

func TestAssetSHA256FromReleaseRejects(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	asset := "sing-box-1.14.0-linux-amd64.tar.gz"
	cases := []struct {
		name string
		body string
		want string
	}{
		{"missing", `{"assets":[]}`, "没有 " + asset},
		{"bad digest", `{"assets":[{"name":"` + asset + `","digest":"md5:abcd","state":"uploaded"}]}`, "没有给出"},
		{"duplicate", `{"assets":[{"name":"` + asset + `","digest":"sha256:` + sum + `","state":"uploaded"},{"name":"` + asset + `","digest":"sha256:` + sum + `","state":"uploaded"}]}`, "多份摘要"},
		{"not json", `{`, "无法解析"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := assetSHA256FromRelease([]byte(tc.body), asset)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestGitHubReleaseAPIIgnoresDownloadProxy(t *testing.T) {
	t.Setenv("LIKING_GITHUB_PROXY", "https://gh-proxy.example/")
	u, err := githubReleaseAPI("SagerNet", "sing-box", "v"+singboxVer)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://api.github.com/repos/SagerNet/sing-box/releases/tags/v" + singboxVer
	if u != want {
		t.Fatal(u)
	}
	if strings.Contains(u, "gh-proxy") || strings.Contains(u, "checksums.txt") {
		t.Fatal(u)
	}
	for _, bad := range [][3]string{{"../Sager", "sing-box", "v1"}, {"Sager", "sing/box", "v1"}, {"Sager", "sing-box", "v1 2"}} {
		if _, err := githubReleaseAPI(bad[0], bad[1], bad[2]); err == nil {
			t.Fatalf("accepted %q", bad)
		}
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
