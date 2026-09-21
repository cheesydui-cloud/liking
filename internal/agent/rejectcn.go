package agent

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"liking/internal/wsproto"
)

const (
	nftCNTable    = "liking_cn"
	cnCacheMaxAge = 7 * 24 * time.Hour
	cnListFetchTO = 25 * time.Second
)

var cnListURLs = []struct {
	v4, v6 string
}{
	{
		v4: "https://github.com/mayaxcn/china-ip-list/raw/master/chnroute.txt",
		v6: "https://github.com/mayaxcn/china-ip-list/raw/master/chnroute6.txt",
	},
}

var (
	nftRun         = nftRunCmd
	nftBin         = lookNft
	installNFT     = defaultInstallNFT
	nftPkgRun      = nftPkgRunCmd
	nftPkgManagers = defaultNFTPkgManagers
	cnHTTPGet      = httpGetBody
	cnListNow      = time.Now
	rejectCNGOOS   = runtime.GOOS
)

func haveNFT() bool { return nftBin() != "" }

func applyRejectCN(dir string, spec wsproto.RejectCN) error {
	if rejectCNGOOS != "linux" {
		return nil
	}
	ports := uniqPorts(spec.Ports)
	if len(ports) == 0 {
		_ = nftRun("delete", "table", "inet", nftCNTable)
		return nil
	}
	if !haveNFT() {
		return fmt.Errorf("需要 nftables 才能拒绝中国 IP")
	}
	cn4, cn6, err := loadCNLists(dir)
	if err != nil {
		return err
	}
	if len(cn4) == 0 && len(cn6) == 0 {
		return fmt.Errorf("没有中国 IP 段")
	}
	allow4, allow6 := splitIPVersions(spec.Allow)
	script := nftRejectCNScript(ports, allow4, allow6, cn4, cn6)
	path := filepath.Join(dir, "reject-cn.nft")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		return err
	}
	_ = nftRun("delete", "table", "inet", nftCNTable)
	if err := nftRun("-f", path); err != nil {
		return fmt.Errorf("nftables 拒绝中国 IP: %w", err)
	}
	return nil
}

func nftRunCmd(args ...string) error {
	bin := nftBin()
	if bin == "" {
		return fmt.Errorf("需要 nftables 才能拒绝中国 IP")
	}
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

type nftPkgMgr struct {
	Bin  string
	Pre  []string
	Args []string
}

func defaultNFTPkgManagers() []nftPkgMgr {
	var out []nftPkgMgr
	if p := lookBin("apt-get", "apt"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Pre: []string{"update", "-qq"}, Args: []string{"install", "-y", "nftables"}})
	}
	if p := lookBin("dnf"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: []string{"install", "-y", "nftables"}})
	}
	if p := lookBin("yum"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: []string{"install", "-y", "nftables"}})
	}
	if p := lookBin("apk"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: []string{"add", "nftables"}})
	}
	if p := lookBin("pacman"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: []string{"-Sy", "--noconfirm", "nftables"}})
	}
	if p := lookBin("zypper"); p != "" {
		out = append(out, nftPkgMgr{Bin: p, Args: []string{"install", "-y", "nftables"}})
	}
	return out
}

func nftPkgRunCmd(bin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	base := strings.ToLower(filepath.Base(bin))
	if base == "apt-get" || base == "apt" {
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	}
	return cmd.CombinedOutput()
}

func defaultInstallNFT() error {
	if haveNFT() {
		return nil
	}
	if rejectCNGOOS != "linux" {
		return fmt.Errorf("需要 Linux 才能安装 nftables")
	}
	mgrs := nftPkgManagers()
	if len(mgrs) == 0 {
		return fmt.Errorf("需要 nftables 才能拒绝中国 IP")
	}
	var last error
	for _, m := range mgrs {
		if len(m.Pre) > 0 {
			_, _ = nftPkgRun(m.Bin, m.Pre...)
		}
		out, err := nftPkgRun(m.Bin, m.Args...)
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			last = fmt.Errorf("安装 nftables: %s", msg)
			continue
		}
		if haveNFT() {
			return nil
		}
		last = fmt.Errorf("安装后仍找不到 nft")
	}
	if last != nil {
		return last
	}
	return fmt.Errorf("需要 nftables 才能拒绝中国 IP")
}

func uniqPorts(ports []int) []int {
	seen := map[int]struct{}{}
	var out []int
	for _, p := range ports {
		if p < 1 || p > 65535 {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

func splitIPVersions(addrs []string) (v4, v6 []string) {
	seen4, seen6 := map[string]struct{}{}, map[string]struct{}{}
	for _, a := range addrs {
		cidr, ok := normalizeCIDR(a)
		if !ok {
			continue
		}
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ip.To4() != nil {
			if _, ok := seen4[cidr]; ok {
				continue
			}
			seen4[cidr] = struct{}{}
			v4 = append(v4, cidr)
			continue
		}
		if _, ok := seen6[cidr]; ok {
			continue
		}
		seen6[cidr] = struct{}{}
		v6 = append(v6, cidr)
	}
	sort.Strings(v4)
	sort.Strings(v6)
	return v4, v6
}

func normalizeCIDR(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	if ip := net.ParseIP(s); ip != nil {
		if ip.To4() != nil {
			return ip.String() + "/32", true
		}
		return ip.String() + "/128", true
	}
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		return "", false
	}
	ones, _ := n.Mask.Size()
	ip := n.IP
	if v4 := ip.To4(); v4 != nil {
		return v4.String() + "/" + strconv.Itoa(ones), true
	}
	return ip.String() + "/" + strconv.Itoa(ones), true
}

func nftRejectCNScript(ports []int, allow4, allow6, cn4, cn6 []string) string {
	var b strings.Builder
	b.WriteString("table inet ")
	b.WriteString(nftCNTable)
	b.WriteString(" {\n")
	writeNFTSet(&b, "allow4", "ipv4_addr", allow4)
	writeNFTSet(&b, "allow6", "ipv6_addr", allow6)
	writeNFTSet(&b, "cn4", "ipv4_addr", cn4)
	writeNFTSet(&b, "cn6", "ipv6_addr", cn6)
	b.WriteString("  set ports {\n    type inet_service\n")
	if len(ports) > 0 {
		b.WriteString("    elements = { ")
		for i, p := range ports {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Itoa(p))
		}
		b.WriteString(" }\n")
	}
	b.WriteString("  }\n")
	b.WriteString("  chain input {\n")
	b.WriteString("    type filter hook input priority -10; policy accept;\n")
	b.WriteString("    tcp dport @ports ip saddr @allow4 accept\n")
	b.WriteString("    udp dport @ports ip saddr @allow4 accept\n")
	b.WriteString("    tcp dport @ports ip6 saddr @allow6 accept\n")
	b.WriteString("    udp dport @ports ip6 saddr @allow6 accept\n")
	b.WriteString("    tcp dport @ports ip saddr @cn4 drop\n")
	b.WriteString("    udp dport @ports ip saddr @cn4 drop\n")
	b.WriteString("    tcp dport @ports ip6 saddr @cn6 drop\n")
	b.WriteString("    udp dport @ports ip6 saddr @cn6 drop\n")
	b.WriteString("  }\n}\n")
	return b.String()
}

func writeNFTSet(b *strings.Builder, name, typ string, elems []string) {
	fmt.Fprintf(b, "  set %s {\n    type %s\n    flags interval\n    auto-merge\n", name, typ)
	if len(elems) > 0 {
		b.WriteString("    elements = {\n")
		for i, e := range elems {
			b.WriteString("      ")
			b.WriteString(e)
			if i+1 < len(elems) {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("    }\n")
	}
	b.WriteString("  }\n")
}

func loadCNLists(dir string) (v4, v6 []string, err error) {
	v4, err4 := loadCNList(filepath.Join(dir, "cn-ip4.txt"), true)
	v6, err6 := loadCNList(filepath.Join(dir, "cn-ip6.txt"), false)
	if len(v4) == 0 && len(v6) == 0 {
		if err4 != nil {
			return nil, nil, err4
		}
		if err6 != nil {
			return nil, nil, err6
		}
		return nil, nil, fmt.Errorf("没有中国 IP 段")
	}
	if err4 != nil {
		log.Printf("agent: cn ipv4 list: %v", err4)
	}
	if err6 != nil {
		log.Printf("agent: cn ipv6 list: %v", err6)
	}
	return v4, v6, nil
}

func loadCNList(path string, v4 bool) ([]string, error) {
	if cidrs, ok := readCNCache(path); ok {
		return cidrs, nil
	}
	var last error
	for _, u := range cnListURLs {
		src := u.v6
		if v4 {
			src = u.v4
		}
		body, err := cnHTTPGet(src)
		if err != nil {
			last = err
			continue
		}
		cidrs := parseCIDRList(string(body), v4)
		if len(cidrs) == 0 {
			last = fmt.Errorf("列表为空")
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err == nil {
			_ = os.WriteFile(path, body, 0o600)
		}
		return cidrs, nil
	}
	if cidrs, ok := readCNCacheAny(path); ok {
		log.Printf("agent: using stale cn list %s", path)
		return cidrs, nil
	}
	if last == nil {
		last = fmt.Errorf("下载中国 IP 段失败")
	}
	return nil, last
}

func readCNCache(path string) ([]string, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if cnListNow().Sub(st.ModTime()) > cnCacheMaxAge {
		return nil, false
	}
	return readCNCacheAny(path)
}

func readCNCacheAny(path string) ([]string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	v4 := !strings.Contains(path, "ip6")
	cidrs := parseCIDRList(string(b), v4)
	if len(cidrs) == 0 {
		return nil, false
	}
	return cidrs, true
}

func parseCIDRList(body string, want4 bool) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexAny(line, " \t"); i > 0 {
			line = line[:i]
		}
		cidr, ok := normalizeCIDR(line)
		if !ok {
			continue
		}
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		is4 := ip.To4() != nil
		if is4 != want4 {
			continue
		}
		if _, ok := seen[cidr]; ok {
			continue
		}
		seen[cidr] = struct{}{}
		out = append(out, cidr)
	}
	sort.Strings(out)
	return out
}

func httpGetBody(u string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, withGHProxy(u), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "liking-agent")
	client := &http.Client{Timeout: cnListFetchTO}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("下载中国 IP 段: HTTP %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, 4<<20))
}
