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

const nftSpeedTableName = "liking_speed"

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
		if l.Mark == 0 || l.Mbps < 1 {
			continue
		}
		if _, ok := seen[l.Mark]; ok {
			continue
		}
		seen[l.Mark] = struct{}{}
		kBps := nftMbpsToKBps(l.Mbps)
		if kBps < 1 {
			continue
		}
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
