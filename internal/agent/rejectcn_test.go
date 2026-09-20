package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"liking/internal/wsproto"
)

func TestNftRejectCNScriptAllowBeforeDrop(t *testing.T) {
	s := nftRejectCNScript([]int{8443, 443}, []string{"203.0.113.10/32"}, nil, []string{"1.2.3.0/24"}, []string{"2400:cb00::/32"})
	allowAt := strings.Index(s, "ip saddr @allow4 accept")
	dropAt := strings.Index(s, "ip saddr @cn4 drop")
	if allowAt < 0 || dropAt < 0 || allowAt > dropAt {
		t.Fatalf("order allow=%d drop=%d\n%s", allowAt, dropAt, s)
	}
	if !strings.Contains(s, "tcp dport @ports") || !strings.Contains(s, "udp dport @ports") {
		t.Fatalf("missing tcp/udp\n%s", s)
	}
	if !strings.Contains(s, "203.0.113.10/32") || !strings.Contains(s, "1.2.3.0/24") {
		t.Fatalf("missing cidrs\n%s", s)
	}
	if strings.Contains(s, "geoip") {
		t.Fatal("must not use dest geoip")
	}
}

func TestParseCIDRList(t *testing.T) {
	got := parseCIDRList("# c\n1.2.3.0/24\n10.0.0.1\n2400::/32\nnot-an-ip\n", true)
	if len(got) != 2 || got[0] != "1.2.3.0/24" || got[1] != "10.0.0.1/32" {
		t.Fatalf("%v", got)
	}
	v6 := parseCIDRList("2400:cb00::/32\n1.2.3.0/24\n", false)
	if len(v6) != 1 || v6[0] != "2400:cb00::/32" {
		t.Fatalf("%v", v6)
	}
}

func TestApplyRejectCNNonLinuxNoop(t *testing.T) {
	old := rejectCNGOOS
	rejectCNGOOS = "darwin"
	defer func() { rejectCNGOOS = old }()
	called := false
	nftRun = func(args ...string) error {
		called = true
		return nil
	}
	defer func() { nftRun = nftRunCmd }()
	if err := applyRejectCN(t.TempDir(), wsproto.RejectCN{Ports: []int{443}}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("nft on darwin")
	}
}

func TestApplyRejectCNClearsWhenOff(t *testing.T) {
	oldOS := rejectCNGOOS
	rejectCNGOOS = "linux"
	defer func() { rejectCNGOOS = oldOS }()
	var got [][]string
	nftRun = func(args ...string) error {
		got = append(got, append([]string{}, args...))
		return nil
	}
	defer func() { nftRun = nftRunCmd }()
	if err := applyRejectCN(t.TempDir(), wsproto.RejectCN{}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || strings.Join(got[0], " ") != "delete table inet liking_cn" {
		t.Fatalf("%v", got)
	}
}

func TestLoadCNListCacheAndFetch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cn-ip4.txt")
	if err := os.WriteFile(path, []byte("1.0.1.0/24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cnHTTPGet = func(u string) ([]byte, error) {
		t.Fatal("should use cache")
		return nil, nil
	}
	defer func() { cnHTTPGet = httpGetBody }()
	got, err := loadCNList(path, true)
	if err != nil || len(got) != 1 || got[0] != "1.0.1.0/24" {
		t.Fatalf("%v %v", got, err)
	}

	oldNow := cnListNow
	cnListNow = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	defer func() { cnListNow = oldNow }()
	cnHTTPGet = func(u string) ([]byte, error) {
		return []byte("2.2.2.0/24\n"), nil
	}
	got, err = loadCNList(path, true)
	if err != nil || len(got) != 1 || got[0] != "2.2.2.0/24" {
		t.Fatalf("refresh %v %v", got, err)
	}
}

func TestApplyRejectCNWritesScript(t *testing.T) {
	oldOS := rejectCNGOOS
	rejectCNGOOS = "linux"
	oldHave := haveNFT
	haveNFT = func() bool { return true }
	defer func() {
		rejectCNGOOS = oldOS
		haveNFT = oldHave
		nftRun = nftRunCmd
	}()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cn-ip4.txt"), []byte("1.0.1.0/24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cn-ip6.txt"), []byte("2400:cb00::/32\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var script string
	nftRun = func(args ...string) error {
		if len(args) == 2 && args[0] == "-f" {
			b, err := os.ReadFile(args[1])
			if err != nil {
				t.Fatal(err)
			}
			script = string(b)
		}
		return nil
	}
	if err := applyRejectCN(dir, wsproto.RejectCN{Ports: []int{8443}, Allow: []string{"203.0.113.10"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "8443") || !strings.Contains(script, "203.0.113.10/32") || !strings.Contains(script, "1.0.1.0/24") {
		t.Fatalf("script %s", script)
	}
	allowAt := strings.Index(script, "ip saddr @allow4 accept")
	dropAt := strings.Index(script, "ip saddr @cn4 drop")
	if allowAt < 0 || dropAt < 0 || allowAt > dropAt {
		t.Fatalf("order %d %d", allowAt, dropAt)
	}
}

func TestApplyConfigRejectCNIgnoredByOldAgent(t *testing.T) {
	raw, err := json.Marshal(wsproto.ApplyConfig{
		Rev:      "abc",
		RejectCN: wsproto.RejectCN{Ports: []int{443}, Allow: []string{"1.1.1.1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"reject_cn"`) || !strings.Contains(string(raw), "443") {
		t.Fatalf("marshal %s", raw)
	}
	var old struct {
		Rev  string          `json:"rev"`
		Xray json.RawMessage `json:"xray,omitempty"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if old.Rev != "abc" {
		t.Fatal(old.Rev)
	}
}
