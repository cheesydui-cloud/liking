package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectedCoresIncludesWireGuard(t *testing.T) {
	old := haveWG
	defer func() { haveWG = old }()
	haveWG = func() bool { return true }
	found := false
	for _, c := range detectedCores() {
		if c == "wireguard" {
			found = true
		}
	}
	if !found {
		t.Fatal("wireguard missing from detected cores")
	}
	haveWG = func() bool { return false }
	for _, c := range detectedCores() {
		if c == "wireguard" {
			t.Fatal("wireguard reported without tools")
		}
	}
}

func TestInstallWGUsesApt(t *testing.T) {
	oldOS, oldHave, oldMgr, oldRun, oldProbe := wgGOOS, haveWG, wgPkgManagers, wgPkgRun, modprobeWG
	wgGOOS = "linux"
	have := false
	haveWG = func() bool { return have }
	wgPkgManagers = func() []nftPkgMgr {
		return []nftPkgMgr{{Bin: "/usr/bin/apt-get", Pre: []string{"update", "-qq"}, Args: []string{"install", "-y", "wireguard", "iptables"}}}
	}
	var cmds []string
	wgPkgRun = func(bin string, args ...string) ([]byte, error) {
		cmds = append(cmds, bin+" "+strings.Join(args, " "))
		if len(args) > 0 && args[0] == "install" {
			have = true
		}
		return nil, nil
	}
	modprobeWG = func() {}
	defer func() {
		wgGOOS, haveWG, wgPkgManagers, wgPkgRun, modprobeWG = oldOS, oldHave, oldMgr, oldRun, oldProbe
	}()
	if err := defaultInstallWG(); err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 || !strings.Contains(cmds[0], "update") || !strings.Contains(cmds[1], "install -y wireguard iptables") {
		t.Fatalf("%v", cmds)
	}
}

func TestInstallWGFallsBackToTools(t *testing.T) {
	oldOS, oldHave, oldMgr, oldRun, oldProbe := wgGOOS, haveWG, wgPkgManagers, wgPkgRun, modprobeWG
	wgGOOS = "linux"
	have := false
	haveWG = func() bool { return have }
	wgPkgManagers = func() []nftPkgMgr {
		return []nftPkgMgr{{Bin: "/usr/bin/apt-get", Pre: []string{"update", "-qq"}, Args: []string{"install", "-y", "wireguard", "iptables"}}}
	}
	wgPkgRun = func(bin string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "install" && !containsArg(args, "wireguard-tools") {
			return []byte("no package wireguard"), errString("exit")
		}
		if len(args) > 0 && args[0] == "install" {
			have = true
		}
		return nil, nil
	}
	modprobeWG = func() {}
	defer func() {
		wgGOOS, haveWG, wgPkgManagers, wgPkgRun, modprobeWG = oldOS, oldHave, oldMgr, oldRun, oldProbe
	}()
	if err := defaultInstallWG(); err != nil {
		t.Fatal(err)
	}
	if !have {
		t.Fatal("tools package did not count as installed")
	}
}

func TestInstallWGSkipsWhenPresent(t *testing.T) {
	oldHave, oldRun, oldProbe := haveWG, wgPkgRun, modprobeWG
	haveWG = func() bool { return true }
	called := false
	wgPkgRun = func(string, ...string) ([]byte, error) {
		called = true
		return nil, nil
	}
	modprobeWG = func() {}
	defer func() {
		haveWG, wgPkgRun, modprobeWG = oldHave, oldRun, oldProbe
	}()
	if err := defaultInstallWG(); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("already installed")
	}
}

func TestUninstallWGUsesApt(t *testing.T) {
	oldOS, oldHave, oldMgr, oldRun := wgGOOS, haveWG, wgRemoveManagers, wgPkgRun
	wgGOOS = "linux"
	have := true
	haveWG = func() bool { return have }
	wgRemoveManagers = func() []nftPkgMgr {
		return []nftPkgMgr{{Bin: "/usr/bin/apt-get", Args: wgRemoveArgs("/usr/bin/apt-get")}}
	}
	var cmds []string
	wgPkgRun = func(bin string, args ...string) ([]byte, error) {
		cmds = append(cmds, bin+" "+strings.Join(args, " "))
		have = false
		return nil, nil
	}
	defer func() {
		wgGOOS, haveWG, wgRemoveManagers, wgPkgRun = oldOS, oldHave, oldMgr, oldRun
	}()
	if err := defaultUninstallWG(); err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || !strings.Contains(cmds[0], "remove -y wireguard wireguard-tools") || strings.Contains(cmds[0], "iptables") {
		t.Fatalf("%v", cmds)
	}
}

func TestUninstallWGSkipsWhenAbsent(t *testing.T) {
	oldOS, oldHave, oldRun := wgGOOS, haveWG, wgPkgRun
	wgGOOS = "linux"
	haveWG = func() bool { return false }
	called := false
	wgPkgRun = func(string, ...string) ([]byte, error) {
		called = true
		return nil, nil
	}
	defer func() {
		wgGOOS, haveWG, wgPkgRun = oldOS, oldHave, oldRun
	}()
	if err := defaultUninstallWG(); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("package remove ran without tools")
	}
}

func TestUninstallWGRejectsNonLinux(t *testing.T) {
	oldOS, oldHave := wgGOOS, haveWG
	wgGOOS = "windows"
	haveWG = func() bool { return true }
	defer func() {
		wgGOOS, haveWG = oldOS, oldHave
	}()
	if err := defaultUninstallWG(); err == nil || !strings.Contains(err.Error(), "Linux") {
		t.Fatalf("%v", err)
	}
}

func TestForeignWGNamesSkipsLiking(t *testing.T) {
	got := foreignWGNames([]string{"likingwg0", "wg0", " likingwg1 ", "wg0"})
	if len(got) != 1 || got[0] != "wg0" {
		t.Fatalf("%v", got)
	}
}

func TestUninstallWireGuardClearsFiles(t *testing.T) {
	dir := t.TempDir()
	c := NewCores(dir)
	for _, name := range []string{"likingwg0.conf", "likingwg0.sync", "wg-dial-likingwg0.json", "wg-peers.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldUninstall, oldForeign := uninstallWG, wgForeignNames
	uninstallWG = func() error { return nil }
	wgForeignNames = func() []string { return nil }
	defer func() {
		uninstallWG, wgForeignNames = oldUninstall, oldForeign
	}()
	if err := c.UninstallWireGuard(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "likingwg0.conf" || e.Name() == "wg-dial-likingwg0.json" || e.Name() == "wg-peers.json" {
			t.Fatalf("left %s", e.Name())
		}
	}
}

func TestUninstallWireGuardRefusesForeign(t *testing.T) {
	c := NewCores(t.TempDir())
	oldUninstall, oldForeign := uninstallWG, wgForeignNames
	called := false
	uninstallWG = func() error {
		called = true
		return nil
	}
	wgForeignNames = func() []string { return []string{"wg0"} }
	defer func() {
		uninstallWG, wgForeignNames = oldUninstall, oldForeign
	}()
	err := c.UninstallWireGuard()
	if err == nil || !strings.Contains(err.Error(), "wg0") || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestInstallWGRejectsNonLinux(t *testing.T) {
	oldOS, oldHave := wgGOOS, haveWG
	wgGOOS = "windows"
	haveWG = func() bool { return false }
	defer func() {
		wgGOOS, haveWG = oldOS, oldHave
	}()
	if err := defaultInstallWG(); err == nil || !strings.Contains(err.Error(), "Linux") {
		t.Fatalf("%v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
