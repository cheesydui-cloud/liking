package agent

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

var (
	installWG     = defaultInstallWG
	haveWG        = defaultHaveWG
	wgPkgManagers = defaultWGPkgManagers
	wgPkgRun      = nftPkgRunCmd
	wgGOOS        = runtime.GOOS
	modprobeWG    = defaultModprobeWG
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
