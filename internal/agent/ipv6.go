package agent

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const ipv6SysctlName = "99-liking-disable-ipv6.conf"

var (
	ipv6ProcGlob   = "/proc/sys/net/ipv6/conf/*/disable_ipv6"
	ipv6SysctlPath = "/etc/sysctl.d/" + ipv6SysctlName
	ipv6NetDir     = "/sys/class/net"
)

func applyDisableIPv6(disable bool) {
	if runtime.GOOS != "linux" {
		return
	}
	applyDisableIPv6To(disable, ipv6ProcGlob, ipv6SysctlPath)
	if disable {
		flushIPv6Addrs()
	} else {
		kickIPv6Addrs()
	}
}

func applyDisableIPv6To(disable bool, procGlob, sysctlPath string) {
	val := "0"
	if disable {
		val = "1"
	}
	if procGlob != "" {
		matches, err := filepath.Glob(procGlob)
		if err != nil {
			log.Printf("agent: disable_ipv6 glob: %v", err)
		}
		for _, p := range matches {
			if err := os.WriteFile(p, []byte(val+"\n"), 0644); err != nil {
				log.Printf("agent: disable_ipv6 %s: %v", p, err)
			}
		}
	}
	if sysctlPath == "" {
		return
	}
	if dir := filepath.Dir(sysctlPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("agent: persist disable_ipv6: %v", err)
			return
		}
	}
	body := fmt.Sprintf("net.ipv6.conf.all.disable_ipv6 = %s\nnet.ipv6.conf.default.disable_ipv6 = %s\n", val, val)
	if err := os.WriteFile(sysctlPath, []byte(body), 0644); err != nil {
		log.Printf("agent: persist disable_ipv6: %v", err)
	}
}

func listNetIfaces() []string {
	entries, err := os.ReadDir(ipv6NetDir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if name == "" || name == "." || name == ".." {
			continue
		}
		out = append(out, name)
	}
	return out
}

func flushIPv6Addrs() {
	bin := lookBin("ip")
	if bin == "" {
		return
	}
	for _, name := range listNetIfaces() {
		out, err := exec.Command(bin, "-6", "addr", "flush", "dev", name).CombinedOutput()
		if err != nil {
			log.Printf("agent: flush ipv6 %s: %v (%s)", name, err, trimOut(out))
		}
	}
}

func kickIPv6Addrs() {
	ifaces := listNetIfaces()
	ncOK := false
	if nc := lookBin("networkctl"); nc != "" {
		ncOK = true
		for _, name := range ifaces {
			out, err := exec.Command(nc, "reconfigure", name).CombinedOutput()
			if err != nil {
				ncOK = false
				log.Printf("agent: networkctl reconfigure %s: %v (%s)", name, err, trimOut(out))
			}
		}
	}
	if ncOK {
		return
	}
	if nm := lookBin("nmcli"); nm != "" {
		for _, name := range ifaces {
			if name == "lo" {
				continue
			}
			out, err := exec.Command(nm, "device", "reapply", name).CombinedOutput()
			if err != nil {
				log.Printf("agent: nmcli reapply %s: %v (%s)", name, err, trimOut(out))
			}
		}
	}
}

func trimOut(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
