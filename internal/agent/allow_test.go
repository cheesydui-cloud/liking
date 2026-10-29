package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAllowUFW(t *testing.T) {
	oldOS, oldLook, oldRun := fwGOOS, fwLook, fwRun
	fwGOOS = "linux"
	var calls []string
	fwLook = func(names ...string) string {
		if names[0] == "ufw" {
			return "ufw"
		}
		return ""
	}
	fwRun = func(bin string, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "status" {
			return "Status: active", nil
		}
		return "", nil
	}
	t.Cleanup(func() {
		fwGOOS, fwLook, fwRun = oldOS, oldLook, oldRun
	})
	dir := t.TempDir()
	how, err := openAllow(dir, 40611, "tcp")
	if err != nil {
		t.Fatal(err)
	}
	if how != "ufw" {
		t.Fatalf("how %s", how)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "allow 40611/tcp") || strings.Contains(joined, "40611/udp") {
		t.Fatalf("calls\n%s", joined)
	}
	b, err := os.ReadFile(filepath.Join(dir, "allow-ports.json"))
	if err != nil || !strings.Contains(string(b), "40611") {
		t.Fatalf("saved %s %v", b, err)
	}
}

func TestOpenAllowNoLocalFirewall(t *testing.T) {
	oldOS, oldLook, oldRun := fwGOOS, fwLook, fwRun
	fwGOOS = "linux"
	fwLook = func(names ...string) string { return "" }
	fwRun = func(string, ...string) (string, error) { return "", nil }
	t.Cleanup(func() {
		fwGOOS, fwLook, fwRun = oldOS, oldLook, oldRun
	})
	_, err := openAllow(t.TempDir(), 40611, "both")
	if err == nil || !strings.Contains(err.Error(), "服务商安全组") {
		t.Fatalf("%v", err)
	}
}

func TestOpenAllowNFTWhenForeignDrop(t *testing.T) {
	oldOS, oldLook, oldRun, oldNFT := fwGOOS, fwLook, fwRun, nftRun
	fwGOOS = "linux"
	fwLook = func(names ...string) string {
		if names[0] == "nft" {
			return "nft"
		}
		return ""
	}
	fwRun = func(bin string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "list" {
			return "table inet filter {\n chain input { type filter hook input priority 0; policy drop; }\n}\ntable inet liking_cn {\n chain x { drop }\n}\n", nil
		}
		return "", nil
	}
	var nftArgs []string
	nftRun = func(args ...string) error {
		nftArgs = append(nftArgs, strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() {
		fwGOOS, fwLook, fwRun, nftRun = oldOS, oldLook, oldRun, oldNFT
	})
	dir := t.TempDir()
	how, err := openAllow(dir, 40611, "both")
	if err != nil {
		t.Fatal(err)
	}
	if how != "nftables" {
		t.Fatalf("how %s", how)
	}
	script, err := os.ReadFile(filepath.Join(dir, "allow.nft"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), "40611") || !strings.Contains(string(script), "tcp dport") || !strings.Contains(string(script), "udp dport") {
		t.Fatalf("script\n%s", script)
	}
	if !strings.Contains(strings.Join(nftArgs, "\n"), "-f") {
		t.Fatalf("nft %v", nftArgs)
	}
}

func TestUFWInactiveIsNotActive(t *testing.T) {
	if ufwActive("Status: inactive") || ufwActive("状态：不活动") {
		t.Fatal("inactive")
	}
	if !ufwActive("Status: active") || !ufwActive("状态：激活") {
		t.Fatal("active")
	}
}

func TestNftForeignDropIgnoresRejectCN(t *testing.T) {
	if nftForeignDrop("table inet liking_cn {\n chain c { tcp dport 443 drop }\n}\n") {
		t.Fatal("reject cn is not a general firewall")
	}
	if !nftForeignDrop("table inet filter {\n chain input { policy drop; }\n}\n") {
		t.Fatal("foreign drop")
	}
}
