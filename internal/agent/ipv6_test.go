package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"liking/internal/wsproto"
)

func TestApplyDisableIPv6To(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "conf")
	for _, name := range []string{"all", "default", "eth0"} {
		d := filepath.Join(conf, name)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "disable_ipv6"), []byte("0\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	sysctl := filepath.Join(dir, "sysctl.d", ipv6SysctlName)
	glob := filepath.Join(conf, "*", "disable_ipv6")

	applyDisableIPv6To(true, glob, sysctl)
	for _, name := range []string{"all", "default", "eth0"} {
		b, err := os.ReadFile(filepath.Join(conf, name, "disable_ipv6"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(b)) != "1" {
			t.Fatalf("%s %q", name, b)
		}
	}
	body, err := os.ReadFile(sysctl)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "net.ipv6.conf.all.disable_ipv6 = 1") || !strings.Contains(string(body), "net.ipv6.conf.default.disable_ipv6 = 1") {
		t.Fatalf("sysctl disable %s", body)
	}

	applyDisableIPv6To(false, glob, sysctl)
	for _, name := range []string{"all", "default", "eth0"} {
		b, err := os.ReadFile(filepath.Join(conf, name, "disable_ipv6"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(b)) != "0" {
			t.Fatalf("re-enable %s %q", name, b)
		}
	}
	body, err = os.ReadFile(sysctl)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "net.ipv6.conf.all.disable_ipv6 = 0") {
		t.Fatalf("sysctl enable %s", body)
	}
}

func TestApplyDisableIPv6MissingProc(t *testing.T) {
	dir := t.TempDir()
	sysctl := filepath.Join(dir, "99.conf")
	applyDisableIPv6To(true, filepath.Join(dir, "nope", "*", "disable_ipv6"), sysctl)
	if _, err := os.Stat(sysctl); err != nil {
		t.Fatal(err)
	}
}

func TestListNetIfaces(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"lo", "eth0"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	old := ipv6NetDir
	ipv6NetDir = dir
	defer func() { ipv6NetDir = old }()
	got := listNetIfaces()
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
}

func TestApplyConfigDisableIPv6IgnoredByOldAgent(t *testing.T) {
	raw, err := json.Marshal(wsproto.ApplyConfig{
		Rev:         "abc",
		DisableIPv6: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"disable_ipv6":true`) {
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
