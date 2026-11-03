package agent

import (
	"fmt"
	"log"
	"net"
	"time"
)

// armWGDelay opens a TCP accept-and-close on the WireGuard listen port.
// Client delay rows often TCP-ping that port. WireGuard only answers UDP, so
// the row stays at 超时 while the tunnel itself works. Home has no listen port.
// A listen or firewall failure must not take the tunnel down.
func (c *Cores) armWGDelay(conf string) {
	c.armWGDelays([]string{conf})
}

func (c *Cores) armWGDelays(confs []string) {
	seen := map[int]struct{}{}
	var ports []int
	for _, conf := range confs {
		port := wgConfListenPort(conf)
		if port < 1 {
			continue
		}
		if _, ok := seen[port]; ok {
			continue
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	c.wgDelayMu.Lock()
	c.wgDelayGen++
	gen := c.wgDelayGen
	c.wgDelayMu.Unlock()
	go func() {
		if !c.ensureWGDelays(ports, gen) {
			return
		}
		for _, port := range ports {
			if _, err := openAllow(c.dir, port, "both"); err != nil {
				log.Printf("wireguard: 延迟测试端口 %d 未写入防火墙: %v", port, err)
			}
		}
	}()
}

func (c *Cores) ensureWGDelay(port int, gen uint64) bool {
	if port < 1 {
		return c.ensureWGDelays(nil, gen)
	}
	return c.ensureWGDelays([]int{port}, gen)
}

func (c *Cores) ensureWGDelays(ports []int, gen uint64) bool {
	c.wgDelayMu.Lock()
	defer c.wgDelayMu.Unlock()
	if gen != c.wgDelayGen {
		return false
	}
	want := map[int]struct{}{}
	for _, port := range ports {
		if port >= 1 && port <= 65535 {
			want[port] = struct{}{}
		}
	}
	if len(want) == 0 {
		c.stopWGDelayLocked()
		return false
	}
	if c.wgDelayLns == nil {
		c.wgDelayLns = map[int]net.Listener{}
	}
	for port, ln := range c.wgDelayLns {
		if _, ok := want[port]; ok {
			continue
		}
		_ = ln.Close()
		delete(c.wgDelayLns, port)
	}
	okAny := false
	for port := range want {
		if _, ok := c.wgDelayLns[port]; ok {
			okAny = true
			continue
		}
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			ln, err = net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
		}
		if err != nil {
			log.Printf("wireguard: 延迟测试监听 %d: %v", port, err)
			continue
		}
		c.wgDelayLns[port] = ln
		go serveWGDelay(ln)
		okAny = true
	}
	c.wgDelayLn = nil
	c.wgDelayPort = 0
	for port, ln := range c.wgDelayLns {
		c.wgDelayLn = ln
		c.wgDelayPort = port
		break
	}
	return okAny
}

func serveWGDelay(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(1500 * time.Millisecond))
			buf := make([]byte, 16)
			_, _ = conn.Read(buf)
		}()
	}
}

func (c *Cores) stopWGDelay() {
	c.wgDelayMu.Lock()
	defer c.wgDelayMu.Unlock()
	c.wgDelayGen++
	c.stopWGDelayLocked()
}

func (c *Cores) stopWGDelayLocked() {
	for port, ln := range c.wgDelayLns {
		if ln != nil {
			_ = ln.Close()
		}
		delete(c.wgDelayLns, port)
	}
	if c.wgDelayLn != nil {
		_ = c.wgDelayLn.Close()
		c.wgDelayLn = nil
	}
	c.wgDelayPort = 0
}
