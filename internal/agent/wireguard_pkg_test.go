package agent

import (
	"strings"
	"testing"
)

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
