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
	port := wgConfListenPort(conf)
	c.wgDelayMu.Lock()
	c.wgDelayGen++
	gen := c.wgDelayGen
	c.wgDelayMu.Unlock()
	go func() {
		if !c.ensureWGDelay(port, gen) || port < 1 {
			return
		}
		if _, err := openAllow(c.dir, port, "both"); err != nil {
			log.Printf("wireguard: 延迟测试端口 %d 未写入防火墙: %v", port, err)
		}
	}()
}

func (c *Cores) ensureWGDelay(port int, gen uint64) bool {
	c.wgDelayMu.Lock()
	defer c.wgDelayMu.Unlock()
	if gen != c.wgDelayGen {
		return false
	}
	if port < 1 || port > 65535 {
		c.stopWGDelayLocked()
		return false
	}
	if c.wgDelayLn != nil && c.wgDelayPort == port {
		return true
	}
	c.stopWGDelayLocked()
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		ln, err = net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	}
	if err != nil {
		log.Printf("wireguard: 延迟测试监听 %d: %v", port, err)
		return false
	}
	c.wgDelayLn = ln
	c.wgDelayPort = port
	go serveWGDelay(ln)
	return true
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
	if c.wgDelayLn != nil {
		_ = c.wgDelayLn.Close()
		c.wgDelayLn = nil
	}
	c.wgDelayPort = 0
}
