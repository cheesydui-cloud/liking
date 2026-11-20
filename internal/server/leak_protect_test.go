package server

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"liking/internal/db"
)

func TestSubscribeLeakProtectFollowsPackage(t *testing.T) {
	d := openSubNameDB(t)
	srv := mustSubServer(t, d, "203.0.113.40")
	u := mustSubUser(t, d, "leak", "")
	in := mustSubInbound(t, d, srv.ID, "n1", 39210)
	mustSubClient(t, d, u, in)
	pkg, err := db.CreatePackageConfigured(d, &db.Package{
		Name: "防泄漏", Direction: "oneway", LeakProtect: true,
	}, db.PackageRelationsInbound, []int64{in.ID}, []float64{1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BindUserPackage(d, u.ID, pkg.ID, 0); err != nil {
		t.Fatal(err)
	}
	panel, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(panel.Close)
	ts := httptest.NewServer(panel.Router())
	t.Cleanup(ts.Close)

	clash := mustSubHTTP(t, ts.URL+"/api/sub/"+u.SubToken+"/clash")
	for _, s := range []string{"tun:", "respect-rules: true", "https://1.1.1.1/dns-query", "MATCH,节点选择", "DST-PORT,3478"} {
		if !strings.Contains(clash, s) {
			t.Fatalf("clash on missing %q\n%s", s, clash)
		}
	}
	if strings.Contains(clash, "MATCH,回落") {
		t.Fatalf("clash still falls open\n%s", clash)
	}
	sb := mustSubHTTP(t, ts.URL+"/api/sub/"+u.SubToken+"/singbox")
	for _, s := range []string{`"tag": "tun-in"`, `"action": "hijack-dns"`, `"final": "节点选择"`, `"/dns-query"`} {
		if !strings.Contains(sb, s) {
			t.Fatalf("singbox on missing %s\n%s", s, sb)
		}
	}
	uriBody := mustSubHTTP(t, ts.URL+"/api/sub/"+u.SubToken+"/uri")
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(uriBody))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "tun:") || strings.Contains(string(raw), "hijack-dns") {
		t.Fatalf("uri changed %s", raw)
	}

	pkg.LeakProtect = false
	if err := db.UpdatePackage(d, pkg); err != nil {
		t.Fatal(err)
	}
	clash = mustSubHTTP(t, ts.URL+"/api/sub/"+u.SubToken+"/clash")
	if strings.Contains(clash, "tun:") || strings.Contains(clash, "respect-rules") || strings.Contains(clash, "MATCH,节点选择") {
		t.Fatalf("clash off\n%s", clash)
	}
	if !strings.Contains(clash, "MATCH,回落") {
		t.Fatalf("expected fallback\n%s", clash)
	}
	sb = mustSubHTTP(t, ts.URL+"/api/sub/"+u.SubToken+"/singbox")
	if strings.Contains(sb, "tun-in") || strings.Contains(sb, "hijack-dns") || strings.Contains(sb, `"final": "节点选择"`) {
		t.Fatalf("singbox off\n%s", sb)
	}
}

func mustSubHTTP(t *testing.T, url string) string {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("%s %d %s", url, res.StatusCode, body)
	}
	return string(body)
}
