package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const nftAllowTable = "liking_allow"

type allowRecord struct {
	Port int  `json:"port"`
	TCP  bool `json:"tcp"`
	UDP  bool `json:"udp"`
}

var (
	allowMu sync.Mutex
	fwLook  = lookBin
	fwRun   = fwRunCmd
	fwGOOS  = runtime.GOOS
)

func fwRunCmd(bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		if msg == "" {
			return "", err
		}
		return msg, fmt.Errorf("%s", msg)
	}
	return msg, nil
}

func openAllow(dir string, port int, proto string) (string, error) {
	tcp, udp, err := allowProto(proto)
	if err != nil {
		return "", err
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("端口无效")
	}
	allowMu.Lock()
	defer allowMu.Unlock()
	list, err := loadAllows(dir)
	if err != nil {
		return "", err
	}
	list = mergeAllow(list, allowRecord{Port: port, TCP: tcp, UDP: udp})
	how, err := applyAllows(dir, list)
	if err != nil {
		return "", err
	}
	if err := saveAllows(dir, list); err != nil {
		return how, err
	}
	return how, nil
}

func reapplySavedAllows(dir string) (int, error) {
	allowMu.Lock()
	defer allowMu.Unlock()
	list, err := loadAllows(dir)
	if err != nil || len(list) == 0 {
		return 0, err
	}
	if _, err := applyAllows(dir, list); err != nil {
		return 0, err
	}
	return len(list), nil
}

func clearAllowNFT() {
	if fwGOOS != "linux" {
		return
	}
	_ = nftRun("delete", "table", "inet", nftAllowTable)
}

func allowProto(proto string) (tcp, udp bool, err error) {
	switch strings.ToLower(strings.TrimSpace(proto)) {
	case "", "both":
		return true, true, nil
	case "tcp":
		return true, false, nil
	case "udp":
		return false, true, nil
	default:
		return false, false, fmt.Errorf("协议只能是 tcp、udp 或 both")
	}
}

func mergeAllow(list []allowRecord, add allowRecord) []allowRecord {
	for i := range list {
		if list[i].Port != add.Port {
			continue
		}
		list[i].TCP = list[i].TCP || add.TCP
		list[i].UDP = list[i].UDP || add.UDP
		return list
	}
	list = append(list, add)
	sort.Slice(list, func(i, j int) bool { return list[i].Port < list[j].Port })
	return list
}

func allowsPath(dir string) string {
	return filepath.Join(dir, "allow-ports.json")
}

func loadAllows(dir string) ([]allowRecord, error) {
	b, err := os.ReadFile(allowsPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []allowRecord
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	var out []allowRecord
	for _, r := range list {
		if r.Port < 1 || r.Port > 65535 || (!r.TCP && !r.UDP) {
			continue
		}
		out = mergeAllow(out, r)
	}
	return out, nil
}

func saveAllows(dir string, list []allowRecord) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return os.WriteFile(allowsPath(dir), b, 0o600)
}

func applyAllows(dir string, list []allowRecord) (string, error) {
	if fwGOOS != "linux" {
		return "", fmt.Errorf("需要 Linux 才能放行端口")
	}
	kind, bin := detectFirewall(list)
	switch kind {
	case "ufw":
		return "ufw", allowUFW(bin, list)
	case "firewalld":
		return "firewalld", allowFirewalld(bin, list)
	case "iptables":
		return "iptables", allowIptables(bin, list)
	case "nftables":
		return "nftables", allowNFT(dir, list)
	default:
		return "", fmt.Errorf("这台机器没有在拦这个端口（没有启用 ufw、firewalld，iptables 也不是 DROP）。外面不通的话，要到服务商安全组放行，面板改不到那里")
	}
}

func detectFirewall(list []allowRecord) (kind, bin string) {
	if p := fwLook("ufw"); p != "" {
		out, err := fwRun(p, "status")
		if err == nil && ufwActive(out) {
			return "ufw", p
		}
	}
	if p := fwLook("firewall-cmd"); p != "" {
		out, err := fwRun(p, "--state")
		if err == nil && strings.TrimSpace(out) == "running" {
			return "firewalld", p
		}
	}
	if p := fwLook("iptables"); p != "" && iptablesFiltering(p) {
		return "iptables", p
	}
	if p := fwLook("nft"); p != "" {
		out, err := fwRun(p, "list", "ruleset")
		if err == nil && nftForeignDrop(out) {
			return "nftables", p
		}
	}
	_ = list
	return "", ""
}

func ufwActive(out string) bool {
	if strings.Contains(strings.ToLower(out), "inactive") || strings.Contains(out, "不活动") {
		return false
	}
	low := strings.ToLower(out)
	return strings.Contains(low, "status: active") || strings.Contains(out, "状态：激活") || strings.Contains(out, "状态: 激活")
}

func iptablesFiltering(bin string) bool {
	out, err := fwRun(bin, "-S", "INPUT")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-P INPUT DROP") || strings.HasPrefix(line, "-P INPUT REJECT") {
			return true
		}
	}
	return false
}

func nftForeignDrop(ruleset string) bool {
	parts := strings.Split(ruleset, "table ")
	for _, p := range parts[1:] {
		head, _, _ := strings.Cut(p, "\n")
		if strings.Contains(head, "liking_cn") || strings.Contains(head, nftAllowTable) {
			continue
		}
		low := strings.ToLower(p)
		if strings.Contains(low, "drop") || strings.Contains(low, "reject") {
			return true
		}
	}
	return false
}

func allowUFW(bin string, list []allowRecord) error {
	for _, r := range list {
		if r.TCP {
			if _, err := fwRun(bin, "allow", strconv.Itoa(r.Port)+"/tcp"); err != nil {
				return fmt.Errorf("ufw: %w", err)
			}
		}
		if r.UDP {
			if _, err := fwRun(bin, "allow", strconv.Itoa(r.Port)+"/udp"); err != nil {
				return fmt.Errorf("ufw: %w", err)
			}
		}
	}
	return nil
}

func allowFirewalld(bin string, list []allowRecord) error {
	for _, r := range list {
		if r.TCP {
			if _, err := fwRun(bin, "--permanent", "--add-port="+strconv.Itoa(r.Port)+"/tcp"); err != nil {
				return fmt.Errorf("firewalld: %w", err)
			}
		}
		if r.UDP {
			if _, err := fwRun(bin, "--permanent", "--add-port="+strconv.Itoa(r.Port)+"/udp"); err != nil {
				return fmt.Errorf("firewalld: %w", err)
			}
		}
	}
	if _, err := fwRun(bin, "--reload"); err != nil {
		return fmt.Errorf("firewalld: %w", err)
	}
	return nil
}

func allowIptables(bin string, list []allowRecord) error {
	for _, r := range list {
		if r.TCP {
			if err := iptablesAllow(bin, "tcp", r.Port); err != nil {
				return err
			}
		}
		if r.UDP {
			if err := iptablesAllow(bin, "udp", r.Port); err != nil {
				return err
			}
		}
	}
	saveIptables()
	return nil
}

func iptablesAllow(bin, proto string, port int) error {
	p := strconv.Itoa(port)
	if _, err := fwRun(bin, "-C", "INPUT", "-p", proto, "--dport", p, "-j", "ACCEPT"); err == nil {
		return nil
	}
	if _, err := fwRun(bin, "-I", "INPUT", "-p", proto, "--dport", p, "-j", "ACCEPT"); err != nil {
		return fmt.Errorf("iptables: %w", err)
	}
	return nil
}

func saveIptables() {
	bin := fwLook("iptables-save")
	if bin == "" {
		return
	}
	out, err := fwRun(bin)
	if err != nil || out == "" {
		return
	}
	for _, p := range []string{"/etc/iptables/rules.v4", "/etc/sysconfig/iptables"} {
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			continue
		}
		_ = os.WriteFile(p, []byte(out+"\n"), 0o600)
	}
}

func allowNFT(dir string, list []allowRecord) error {
	script := nftAllowScript(list)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	path := filepath.Join(dir, "allow.nft")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		return err
	}
	_ = nftRun("delete", "table", "inet", nftAllowTable)
	if err := nftRun("-f", path); err != nil {
		return fmt.Errorf("nftables: %w", err)
	}
	return nil
}

func nftAllowScript(list []allowRecord) string {
	var tcp, udp []string
	for _, r := range list {
		p := strconv.Itoa(r.Port)
		if r.TCP {
			tcp = append(tcp, p)
		}
		if r.UDP {
			udp = append(udp, p)
		}
	}
	var b strings.Builder
	b.WriteString("table inet " + nftAllowTable + " {\n")
	b.WriteString("  chain input {\n")
	b.WriteString("    type filter hook input priority -10; policy accept;\n")
	if len(tcp) > 0 {
		fmt.Fprintf(&b, "    tcp dport { %s } accept\n", strings.Join(tcp, ", "))
	}
	if len(udp) > 0 {
		fmt.Fprintf(&b, "    udp dport { %s } accept\n", strings.Join(udp, ", "))
	}
	b.WriteString("  }\n}\n")
	return b.String()
}
