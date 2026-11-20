package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

var (
	installWG        = defaultInstallWG
	uninstallWG      = defaultUninstallWG
	haveWG           = defaultHaveWG
	wgPkgManagers    = defaultWGPkgManagers
	wgRemoveManagers = defaultWGRemoveManagers
	wgPkgRun         = nftPkgRunCmd
	wgGOOS           = runtime.GOOS
	modprobeWG       = defaultModprobeWG
	wgForeignNames   = defaultWGForeignNames
)

func defaultHaveWG() bool {
	return lookBin("wg") != "" && lookBin("wg-quick") != ""
}

func defaultWGPkgManagers() []nftPkgMgr {
	var out []nftPkgMgr
	if p := lookBin("apt-get", "apt"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Pre: []string{"update", "-qq"}, Args: []string{"install", "-y", "wireguard", "iptables"}})
	}
	if p := lookBin("dnf"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: []string{"install", "-y", "wireguard-tools", "iptables"}})
	}
	if p := lookBin("yum"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: []string{"install", "-y", "wireguard-tools", "iptables"}})
	}
	if p := lookBin("apk"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: []string{"add", "wireguard-tools", "iptables"}})
	}
	if p := lookBin("pacman"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: []string{"-Sy", "--noconfirm", "wireguard-tools", "iptables"}})
	}
	if p := lookBin("zypper"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: []string{"install", "-y", "wireguard-tools", "iptables"}})
	}
	return out
}

func defaultModprobeWG() {
	bin := lookBin("modprobe")
	if bin == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, bin, "wireguard").Run()
}

func defaultInstallWG() error {
	if haveWG() {
		modprobeWG()
		return nil
	}
	if wgGOOS != "linux" {
		return fmt.Errorf("需要 Linux 才能安装 WireGuard")
	}
	mgrs := wgPkgManagers()
	if len(mgrs) == 0 {
		return fmt.Errorf("找不到包管理器，无法安装 WireGuard")
	}
	var last error
	for _, m := range mgrs {
		if err := runWGPkg(m); err != nil {
			last = err
			if wgFallback(m) {
				alt := m
				alt.Args = []string{"install", "-y", "wireguard-tools", "iptables"}
				if err2 := runWGPkg(alt); err2 == nil {
					modprobeWG()
					return nil
				} else {
					last = err2
				}
			}
			continue
		}
		modprobeWG()
		return nil
	}
	if last != nil {
		return last
	}
	return fmt.Errorf("安装后仍找不到 wg")
}

func wgFallback(m nftPkgMgr) bool {
	base := strings.ToLower(m.Bin)
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if base != "apt-get" && base != "apt" {
		return false
	}
	for _, a := range m.Args {
		if a == "wireguard" {
			return true
		}
	}
	return false
}

func defaultWGRemoveManagers() []nftPkgMgr {
	var out []nftPkgMgr
	if p := lookBin("apt-get", "apt"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: wgRemoveArgs(p)})
	}
	if p := lookBin("dnf"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: wgRemoveArgs(p)})
	}
	if p := lookBin("yum"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: wgRemoveArgs(p)})
	}
	if p := lookBin("apk"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: wgRemoveArgs(p)})
	}
	if p := lookBin("pacman"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: wgRemoveArgs(p)})
	}
	if p := lookBin("zypper"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: wgRemoveArgs(p)})
	}
	return out
}

// wgRemoveArgs removes the tools package only. iptables stays: other features use it.
func wgRemoveArgs(bin string) []string {
	base := strings.ToLower(bin)
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	switch base {
	case "apk":
		return []string{"del", "wireguard-tools"}
	case "pacman":
		return []string{"-R", "--noconfirm", "wireguard-tools"}
	case "apt-get", "apt":
		return []string{"remove", "-y", "wireguard", "wireguard-tools"}
	default:
		return []string{"remove", "-y", "wireguard-tools"}
	}
}

func defaultUninstallWG() error {
	if wgGOOS != "linux" {
		return fmt.Errorf("需要 Linux 才能卸载 WireGuard")
	}
	if !haveWG() {
		return nil
	}
	mgrs := wgRemoveManagers()
	if len(mgrs) == 0 {
		return fmt.Errorf("找不到包管理器，无法卸载 WireGuard")
	}
	var last error
	for _, m := range mgrs {
		if err := runWGRemove(m); err != nil {
			last = err
		}
		if !haveWG() {
			return nil
		}
	}
	if last != nil {
		return last
	}
	return fmt.Errorf("卸载后仍能找到 wg")
}

func runWGRemove(m nftPkgMgr) error {
	out, err := wgPkgRun(m.Bin, m.Args...)
	if err != nil {
		return fmt.Errorf("卸载 WireGuard: %s", clipCmd(out, err))
	}
	return nil
}

func defaultWGForeignNames() []string {
	return foreignWGNames(append(listWGIfaces(), etcWGConfNames()...))
}

func foreignWGNames(names []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || likingWGIface(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func etcWGConfNames() []string {
	entries, err := os.ReadDir("/etc/wireguard")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".conf") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".conf"))
	}
	return out
}

func runWGPkg(m nftPkgMgr) error {
	if len(m.Pre) > 0 {
		_, _ = wgPkgRun(m.Bin, m.Pre...)
	}
	out, err := wgPkgRun(m.Bin, m.Args...)
	if err != nil {
		return fmt.Errorf("安装 WireGuard: %s", clipCmd(out, err))
	}
	if !haveWG() {
		return fmt.Errorf("安装后仍找不到 wg")
	}
	return nil
}

func clipCmd(out []byte, err error) string {
	msg := strings.Join(strings.Fields(string(out)), " ")
	if msg == "" && err != nil {
		msg = err.Error()
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}
