package agent

import (
	"fmt"
	"io"
	"log"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"liking/internal/wsproto"
)

const nftForwardTable = "liking_forward"

// Forwarder relays TCP in userspace and DNATs UDP with nftables.
// A nil rule list means the panel did not send forwards; Apply is not called.
// An empty list clears relays left by an older push.
type Forwarder struct {
	mu         sync.Mutex
	listenHost string
	tcp        map[int]*tcpRelay
	udpHash    string
	udpOn      bool
}

func NewForwarder() *Forwarder {
	return &Forwarder{tcp: map[int]*tcpRelay{}}
}

type tcpRelay struct {
	dest  string
	ln    net.Listener
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func (r *tcpRelay) track(c net.Conn) {
	r.mu.Lock()
	if r.conns == nil {
		r.conns = map[net.Conn]struct{}{}
	}
	r.conns[c] = struct{}{}
	r.mu.Unlock()
}

func (r *tcpRelay) untrack(c net.Conn) {
	r.mu.Lock()
	delete(r.conns, c)
	r.mu.Unlock()
}

func (r *tcpRelay) stop() {
	if r == nil {
		return
	}
	if r.ln != nil {
		_ = r.ln.Close()
	}
	r.mu.Lock()
	for c := range r.conns {
		_ = c.Close()
	}
	r.mu.Unlock()
}

type udpForward struct {
	Listen int
	IP     string
	Port   int
}

func (f *Forwarder) Apply(rules []wsproto.ForwardRule) error {
	if f == nil {
		return nil
	}
	tcp, udp, err := splitForwardRules(rules)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.applyTCP(tcp); err != nil {
		return err
	}
	return f.applyUDP(udp)
}

func (f *Forwarder) Close() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for port, r := range f.tcp {
		r.stop()
		delete(f.tcp, port)
	}
	if f.udpOn {
		_ = applyForwardNft("")
		f.udpOn = false
		f.udpHash = ""
	}
}

func splitForwardRules(rules []wsproto.ForwardRule) (map[int]string, []udpForward, error) {
	tcp := map[int]string{}
	var udp []udpForward
	seenUDP := map[int]string{}
	for _, r := range rules {
		if r.Port < 1 || r.Port > 65535 || r.DestPort < 1 || r.DestPort > 65535 {
			return nil, nil, fmt.Errorf("转发端口无效")
		}
		host := strings.TrimSpace(r.DestHost)
		if host == "" {
			return nil, nil, fmt.Errorf("转发目标地址为空")
		}
		dest := net.JoinHostPort(host, strconv.Itoa(r.DestPort))
		if wantsForward(r.Proto, "tcp") {
			if prev, ok := tcp[r.Port]; ok && prev != dest {
				return nil, nil, fmt.Errorf("TCP 端口 %d 有两个目标", r.Port)
			}
			tcp[r.Port] = dest
		}
		if wantsForward(r.Proto, "udp") {
			ip, err := forwardIPv4(host)
			if err != nil {
				return nil, nil, err
			}
			target := net.JoinHostPort(ip, strconv.Itoa(r.DestPort))
			if prev, ok := seenUDP[r.Port]; ok && prev != target {
				return nil, nil, fmt.Errorf("UDP 端口 %d 有两个目标", r.Port)
			}
			if _, ok := seenUDP[r.Port]; ok {
				continue
			}
			seenUDP[r.Port] = target
			udp = append(udp, udpForward{Listen: r.Port, IP: ip, Port: r.DestPort})
		}
	}
	return tcp, udp, nil
}

func wantsForward(proto, part string) bool {
	return proto == part || proto == "tcp+udp" || proto == ""
}

func forwardIPv4(host string) (string, error) {
	if ip := net.ParseIP(host); ip != nil {
		v4 := ip.To4()
		if v4 == nil {
			return "", fmt.Errorf("UDP 转发暂只支持 IPv4：%s", host)
		}
		return v4.String(), nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return "", fmt.Errorf("UDP 转发解析 %s 失败", host)
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), nil
		}
	}
	return "", fmt.Errorf("UDP 转发暂只支持 IPv4：%s", host)
}

func (f *Forwarder) applyTCP(want map[int]string) error {
	if f.tcp == nil {
		f.tcp = map[int]*tcpRelay{}
	}
	for port, r := range f.tcp {
		dest, ok := want[port]
		if ok && r.dest == dest {
			continue
		}
		r.stop()
		delete(f.tcp, port)
	}
	for port, dest := range want {
		if _, ok := f.tcp[port]; ok {
			continue
		}
		addr := fmt.Sprintf(":%d", port)
		if f.listenHost != "" {
			addr = net.JoinHostPort(f.listenHost, strconv.Itoa(port))
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("入口端口 %d 监听失败: %w", port, err)
		}
		r := &tcpRelay{dest: dest, ln: ln, conns: map[net.Conn]struct{}{}}
		f.tcp[port] = r
		go serveTCP(r, dest)
	}
	return nil
}

func serveTCP(r *tcpRelay, dest string) {
	for {
		c, err := r.ln.Accept()
		if err != nil {
			return
		}
		go relayTCP(r, c, dest)
	}
}

func relayTCP(r *tcpRelay, client net.Conn, dest string) {
	r.track(client)
	defer r.untrack(client)
	defer client.Close()
	setKeepAlive(client)
	dialer := net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	up, err := dialer.Dial("tcp", dest)
	if err != nil {
		log.Printf("agent: 转发 %s 连接失败: %v", dest, err)
		return
	}
	r.track(up)
	defer r.untrack(up)
	defer up.Close()
	setKeepAlive(up)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(up, client)
		closeWrite(up)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, up)
		closeWrite(client)
	}()
	wg.Wait()
}

func setKeepAlive(c net.Conn) {
	tc, ok := c.(*net.TCPConn)
	if !ok {
		return
	}
	_ = tc.SetKeepAlive(true)
	_ = tc.SetKeepAlivePeriod(30 * time.Second)
}

func closeWrite(c net.Conn) {
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
		return
	}
	_ = c.Close()
}

func (f *Forwarder) applyUDP(rules []udpForward) error {
	body := nftForwardScript(rules)
	if body == f.udpHash && (body == "" || f.udpOn) {
		return nil
	}
	if forwardGOOS != "linux" {
		if len(rules) == 0 {
			f.udpHash = ""
			f.udpOn = false
			return nil
		}
		return fmt.Errorf("需要 Linux 才能转发 UDP")
	}
	if err := applyForwardNft(body); err != nil {
		return err
	}
	f.udpHash = body
	f.udpOn = body != ""
	return nil
}

var (
	forwardGOOS    = runtime.GOOS
	forwardNftBin  = lookNft
	applyForwardNft = applyForwardNftCmd
)

func applyForwardNftCmd(body string) error {
	bin := forwardNftBin()
	if bin == "" {
		if body == "" {
			return nil
		}
		return fmt.Errorf("需要 nftables 才能转发 UDP")
	}
	_ = exec.Command(bin, "delete", "table", "inet", nftForwardTable).Run()
	if body == "" {
		return nil
	}
	cmd := exec.Command(bin, "-f", "-")
	cmd.Stdin = strings.NewReader(body)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nftables 转发失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// nftForwardScript renders table inet liking_forward. An accept in our own
// filter chain runs before a later FORWARD drop, so Docker or ufw does not
// need a separate shim. Destinations must already be IPv4.
func nftForwardScript(rules []udpForward) string {
	if len(rules) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("table inet " + nftForwardTable + " {\n")
	b.WriteString("\tchain prerouting {\n")
	b.WriteString("\t\ttype nat hook prerouting priority dstnat; policy accept;\n")
	for _, r := range rules {
		fmt.Fprintf(&b, "\t\tudp dport %d dnat ip to %s:%d\n", r.Listen, r.IP, r.Port)
	}
	b.WriteString("\t}\n")
	b.WriteString("\tchain postrouting {\n")
	b.WriteString("\t\ttype nat hook postrouting priority srcnat; policy accept;\n")
	for _, r := range rules {
		fmt.Fprintf(&b, "\t\tip daddr %s udp dport %d ct status dnat masquerade\n", r.IP, r.Port)
	}
	b.WriteString("\t}\n")
	b.WriteString("\tchain forward_accept {\n")
	b.WriteString("\t\ttype filter hook forward priority -10; policy accept;\n")
	for _, r := range rules {
		fmt.Fprintf(&b, "\t\tip daddr %s udp dport %d ct status dnat accept\n", r.IP, r.Port)
	}
	b.WriteString("\t\tct state established,related ct status dnat accept\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")
	return b.String()
}
