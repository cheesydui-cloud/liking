package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"liking/internal/wsproto"
)

// readFlowSources is replaced in tests. Production reads conntrack, then ss.
var readFlowSources = defaultReadFlowSources

type flowSources struct {
	conntrack string
	ss        string
	local     map[string]struct{}
}

func defaultReadFlowSources() (flowSources, error) {
	if runtime.GOOS != "linux" {
		return flowSources{}, os.ErrNotExist
	}
	enableConntrackAcct()
	var src flowSources
	src.local = localAddrs()
	if b, err := os.ReadFile("/proc/net/nf_conntrack"); err == nil {
		src.conntrack = string(b)
	}
	if !strings.Contains(src.conntrack, "bytes=") {
		if out, err := runFlowTool("conntrack", "-L"); err == nil && strings.Contains(string(out), "bytes=") {
			src.conntrack = string(out)
		}
	}
	// -p scans /proc for every socket and is not parsed. On a busy node it
	// blows the timeout, and a non-zero exit used to discard the fwmark lines
	// already printed. Keep any stdout that contains a socket line.
	out, err := runFlowTool("ss", "-tuine")
	if text := strings.TrimSpace(string(out)); text != "" {
		src.ss = string(out)
	}
	if err != nil {
		if lookBin("ss") == "" {
			noteStatsFail("ss", fmt.Errorf("未找到 ss，套接字标记无法读取"))
		} else if src.ss == "" {
			noteStatsFail("ss", err)
		} else {
			noteStatsFail("ss", fmt.Errorf("%v（已保留套接字输出）", err))
		}
	}
	if src.conntrack == "" && src.ss == "" {
		if err != nil {
			return src, err
		}
		return src, os.ErrNotExist
	}
	return src, nil
}

func runFlowTool(name string, args ...string) ([]byte, error) {
	bin := lookBin(name)
	if bin == "" {
		return nil, os.ErrNotExist
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

func enableConntrackAcct() {
	if runtime.GOOS != "linux" {
		return
	}
	_ = os.WriteFile("/proc/sys/net/netfilter/nf_conntrack_acct", []byte("1\n"), 0o644)
}

func localAddrs() map[string]struct{} {
	out := map[string]struct{}{"127.0.0.1": {}, "::1": {}}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ip, _, err := net.ParseCIDR(a.String())
		if err != nil || ip == nil {
			continue
		}
		out[ip.String()] = struct{}{}
	}
	return out
}

func parseMark(s string) (uint32, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	base := 10
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		base = 16
		s = s[2:]
	}
	n, err := strconv.ParseUint(s, base, 32)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint32(n), true
}

// parseConntrackLine reads one nf_conntrack row.
// Inbound upload is original bytes when original dport is a listen port and
// original dst is local. Reply bytes are the download.
// Marked rows are outbound sockets; original bytes are the user upload.
func parseConntrackLine(line string) (dport int, dst string, origBytes, replyBytes int64, mark uint32, hasMark, hasBytes bool) {
	srcN := 0
	for _, f := range strings.Fields(line) {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			continue
		}
		switch k {
		case "src":
			srcN++
		case "dst":
			if srcN == 1 && dst == "" {
				dst = v
			}
		case "dport":
			if srcN == 1 && dport == 0 {
				dport, _ = strconv.Atoi(v)
			}
		case "bytes":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				continue
			}
			hasBytes = true
			if srcN <= 1 {
				origBytes = n
			} else if replyBytes == 0 {
				replyBytes = n
			}
		case "mark":
			if m, ok := parseMark(v); ok {
				mark = m
				hasMark = true
			}
		}
	}
	return
}

func parseConntrack(text string, listen map[int]struct{}, local map[string]struct{}) (ports map[int]bytePair, marks map[uint32]bytePair, hasBytes bool) {
	ports = map[int]bytePair{}
	marks = map[uint32]bytePair{}
	for _, line := range strings.Split(text, "\n") {
		dport, dst, up, down, mark, hasMark, bytesOK := parseConntrackLine(line)
		if !bytesOK {
			continue
		}
		hasBytes = true
		_, hit := listen[dport]
		_, here := local[dst]
		if dport > 0 && hit && here {
			a := ports[dport]
			a.up += up
			a.down += down
			ports[dport] = a
		}
		if hasMark {
			a := marks[mark]
			a.up += up
			a.down += down
			marks[mark] = a
		}
	}
	return ports, marks, hasBytes
}

func ssLocalPort(line string, listen map[int]struct{}) (int, bool) {
	local := ssLocalAddr(line)
	if local == "" {
		return 0, false
	}
	_, port, err := net.SplitHostPort(local)
	if err != nil {
		i := strings.LastIndex(local, ":")
		if i < 0 {
			return 0, false
		}
		port = local[i+1:]
	}
	n, err := strconv.Atoi(port)
	if err != nil || n <= 0 {
		return 0, false
	}
	_, ok := listen[n]
	return n, ok
}

func ssLocalAddr(line string) string {
	var addrs []string
	for _, f := range strings.Fields(line) {
		if !strings.Contains(f, ":") || strings.Contains(f, "=") {
			continue
		}
		addrs = append(addrs, f)
		if len(addrs) == 1 {
			return f
		}
	}
	return ""
}

func parseSSDetail(line string) (sent, recv int64, mark uint32, hasMark bool) {
	for _, f := range strings.Fields(line) {
		k, v, ok := strings.Cut(f, ":")
		if !ok {
			continue
		}
		switch k {
		case "bytes_sent":
			sent, _ = strconv.ParseInt(v, 10, 64)
		case "bytes_received":
			recv, _ = strconv.ParseInt(v, 10, 64)
		case "fwmark", "mark":
			if m, ok := parseMark(v); ok {
				mark = m
				hasMark = true
			}
		}
	}
	return
}

// parseSS reads `ss -tuine`. iproute2 prints fwmark on the socket line and
// bytes_sent/bytes_received on the next indented line. A mark is kept for that
// socket so the following line's bytes are attributed to it.
func parseSS(text string, listen map[int]struct{}) (ports map[int]bytePair, marks map[uint32]bytePair) {
	ports = map[int]bytePair{}
	marks = map[uint32]bytePair{}
	var port int
	var isListen bool
	var headMark uint32
	var headHasMark bool
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			port, isListen = ssLocalPort(line, listen)
			sent, recv, mark, hasMark := parseSSDetail(line)
			headMark, headHasMark = 0, false
			if hasMark {
				headMark, headHasMark = mark, true
				addSSMark(marks, mark, sent, recv)
			}
			continue
		}
		sent, recv, mark, hasMark := parseSSDetail(line)
		if !hasMark && headHasMark {
			mark, hasMark = headMark, true
		}
		if isListen && port > 0 {
			a := ports[port]
			a.up += recv
			a.down += sent
			ports[port] = a
		}
		if hasMark {
			addSSMark(marks, mark, sent, recv)
		}
	}
	return ports, marks
}

func addSSMark(marks map[uint32]bytePair, mark uint32, sent, recv int64) {
	if mark == 0 || (sent == 0 && recv == 0) {
		return
	}
	a := marks[mark]
	a.up += sent
	a.down += recv
	marks[mark] = a
}

// mergeMarkBytes takes the larger of each direction. Conntrack mark is the
// connection mark, which stays 0 unless nft copied SO_MARK. ss fwmark is the
// socket mark the core actually sets. Summing the two would bill a live flow
// twice once the copy succeeds; skipping ss whenever conntrack has bytes left
// every user at 0.
func mergeMarkBytes(conntrack, ss map[uint32]bytePair) map[uint32]bytePair {
	out := map[uint32]bytePair{}
	seen := map[uint32]struct{}{}
	for mark := range conntrack {
		seen[mark] = struct{}{}
	}
	for mark := range ss {
		seen[mark] = struct{}{}
	}
	for mark := range seen {
		a, b := conntrack[mark], ss[mark]
		up, down := a.up, a.down
		if b.up > up {
			up = b.up
		}
		if b.down > down {
			down = b.down
		}
		if up == 0 && down == 0 {
			continue
		}
		out[mark] = bytePair{up: up, down: down}
	}
	return out
}

func flowCumul(src flowSources, listen map[int]struct{}, emails map[uint32]string) (ports map[int]bytePair, byEmail map[string]bytePair, ok bool) {
	ports = map[int]bytePair{}
	byEmail = map[string]bytePair{}
	var ctMarks map[uint32]bytePair
	if strings.Contains(src.conntrack, "bytes=") {
		var hasBytes bool
		ports, ctMarks, hasBytes = parseConntrack(src.conntrack, listen, src.local)
		if hasBytes {
			ok = true
		}
	}
	var ssMarks map[uint32]bytePair
	if strings.TrimSpace(src.ss) != "" {
		ssPorts, marks := parseSS(src.ss, listen)
		ssMarks = marks
		if !ok {
			ports = ssPorts
			ok = true
		}
	}
	if !ok {
		return nil, nil, false
	}
	for mark, b := range mergeMarkBytes(ctMarks, ssMarks) {
		email := emails[mark]
		if email == "" || (b.up == 0 && b.down == 0) {
			continue
		}
		a := byEmail[email]
		a.up += b.up
		a.down += b.down
		byEmail[email] = a
	}
	return ports, byEmail, true
}

func (c *Cores) peekFlows() ([]wsproto.Sample, map[string]bytePair, map[int]bytePair, bool) {
	c.mu.Lock()
	listen := listenPortsLocked(c.last)
	marks := append([]wsproto.AcctMark(nil), c.acctMarks...)
	c.mu.Unlock()
	emails := map[uint32]string{}
	for _, m := range marks {
		if m.Mark == 0 || m.Email == "" {
			continue
		}
		emails[m.Mark] = m.Email
	}
	src, err := readFlowSources()
	if err != nil && src.conntrack == "" && src.ss == "" {
		c.pendingPorts = nil
		c.pendingHasPorts = false
		return nil, nil, nil, false
	}
	listenSet := map[int]struct{}{}
	for _, p := range listen {
		listenSet[p] = struct{}{}
	}
	portCumul, emailCumul, ok := flowCumul(src, listenSet, emails)
	if !ok {
		c.pendingPorts = nil
		c.pendingHasPorts = false
		return nil, nil, nil, false
	}
	dt := 5.0
	if !c.flowAt.IsZero() {
		d := time.Since(c.flowAt).Seconds()
		if d > 0.2 && d < 60 {
			dt = d
		}
	}
	ports, portNext := portRatesFrom(c.portLast, portCumul, listen, dt)
	c.pendingPorts = ports
	c.pendingHasPorts = true
	var cum []wsproto.Sample
	for email, b := range emailCumul {
		cum = append(cum, wsproto.Sample{Email: email, Up: b.up, Down: b.down})
	}
	deltas, next := flowDeltas(c.flowLast, cum)
	return deltas, next, portNext, true
}

func listenPortsLocked(last map[string]json.RawMessage) []int {
	seen := map[int]struct{}{}
	var out []int
	for _, raw := range last {
		tcp, udp := publicListenSpecs(raw)
		for _, a := range append(tcp, udp...) {
			_, p, err := net.SplitHostPort(a)
			if err != nil {
				continue
			}
			n, _ := strconv.Atoi(p)
			if n <= 0 {
				continue
			}
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}
	return out
}

func portRatesFrom(prev map[int]bytePair, cur map[int]bytePair, listen []int, dt float64) ([]wsproto.PortRate, map[int]bytePair) {
	if dt < 0.2 || dt > 60 {
		dt = 5
	}
	next := map[int]bytePair{}
	for k, v := range cur {
		next[k] = v
	}
	out := make([]wsproto.PortRate, 0, len(listen))
	seen := map[int]struct{}{}
	for _, port := range listen {
		if port <= 0 {
			continue
		}
		if _, ok := seen[port]; ok {
			continue
		}
		seen[port] = struct{}{}
		b := cur[port]
		old, had := prev[port]
		var up, down int64
		if had {
			if b.up >= old.up {
				up = b.up - old.up
			}
			if b.down >= old.down {
				down = b.down - old.down
			}
		}
		out = append(out, wsproto.PortRate{
			Port:    port,
			UpBps:   int64(float64(up) / dt),
			DownBps: int64(float64(down) / dt),
		})
	}
	return out, next
}
