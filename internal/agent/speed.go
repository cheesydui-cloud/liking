package agent

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"liking/internal/wsproto"
)

const (
	nftSpeedTableName = "liking_speed"
	nftAcctTableName  = "liking_acct"
)

func nftMbpsToKBps(mbps int64) int64 {
	if mbps < 1 {
		return 0
	}
	n := mbps * 1000 / 8
	if n < 1 {
		return 1
	}
	return n
}

func limitKBps(l wsproto.SpeedLimit) int64 {
	if l.KBps > 0 {
		return l.KBps
	}
	return nftMbpsToKBps(l.Mbps)
}

func nftSpeedTable(limits []wsproto.SpeedLimit) string {
	seen := map[uint32]struct{}{}
	var b strings.Builder
	b.WriteString("table inet ")
	b.WriteString(nftSpeedTableName)
	b.WriteString(" {\n")
	b.WriteString("  chain output {\n")
	b.WriteString("    type filter hook output priority 0; policy accept;\n")
	var input strings.Builder
	n := 0
	for _, l := range limits {
		if l.Mark == 0 {
			continue
		}
		kBps := limitKBps(l)
		if kBps < 1 {
			continue
		}
		if _, ok := seen[l.Mark]; ok {
			continue
		}
		seen[l.Mark] = struct{}{}
		n++
		fmt.Fprintf(&b, "    meta mark 0x%08x ct mark set meta mark\n", l.Mark)
		fmt.Fprintf(&b, "    ct mark 0x%08x limit rate over %d kbytes/second drop\n", l.Mark, kBps)
		fmt.Fprintf(&input, "    ct mark 0x%08x limit rate over %d kbytes/second drop\n", l.Mark, kBps)
	}
	if n == 0 {
		return ""
	}
	b.WriteString("  }\n")
	b.WriteString("  chain input {\n")
	b.WriteString("    type filter hook input priority 0; policy accept;\n")
	b.WriteString(input.String())
	b.WriteString("  }\n}\n")
	return b.String()
}

func lookNft() string {
	if p := lookBin("nft"); p != "" {
		return p
	}
	for _, p := range []string{"/usr/sbin/nft", "/sbin/nft"} {
		st, err := os.Stat(p)
		if err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func nftAcctTable(marks []wsproto.AcctMark) string {
	seen := map[uint32]struct{}{}
	var rules strings.Builder
	n := 0
	for _, m := range marks {
		if m.Mark == 0 {
			continue
		}
		if _, ok := seen[m.Mark]; ok {
			continue
		}
		seen[m.Mark] = struct{}{}
		n++
		fmt.Fprintf(&rules, "    meta mark 0x%08x ct mark set meta mark\n", m.Mark)
	}
	if n == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("table inet ")
	b.WriteString(nftAcctTableName)
	b.WriteString(" {\n  chain output {\n")
	b.WriteString("    type filter hook output priority -10; policy accept;\n")
	b.WriteString(rules.String())
	b.WriteString("  }\n}\n")
	return b.String()
}

func applyAcctMarks(marks []wsproto.AcctMark) {
	if runtime.GOOS != "linux" {
		return
	}
	nft := lookNft()
	if nft == "" {
		return
	}
	_ = exec.Command(nft, "delete", "table", "inet", nftAcctTableName).Run()
	body := nftAcctTable(marks)
	if body == "" {
		return
	}
	cmd := exec.Command(nft, "-f", "-")
	cmd.Stdin = strings.NewReader(body)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("agent: nft 计数标记失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}
}

func applySpeedLimits(limits []wsproto.SpeedLimit) {
	if runtime.GOOS != "linux" {
		return
	}
	nft := lookNft()
	if nft == "" {
		if len(limits) > 0 {
			log.Printf("agent: nft 未安装，跳过限速")
		}
		return
	}
	_ = exec.Command(nft, "delete", "table", "inet", nftSpeedTableName).Run()
	body := nftSpeedTable(limits)
	if body == "" {
		return
	}
	cmd := exec.Command(nft, "-f", "-")
	cmd.Stdin = strings.NewReader(body)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("agent: nft 限速失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}
}
