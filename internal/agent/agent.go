package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"liking/internal/version"
	"liking/internal/wsproto"
)

var (
	errSelfRestart = errors.New("self-restart")
	errUninstalled = errors.New("uninstalled")
)

type Config struct {
	ConnectURL string
	Token      string
	TokenFile  string
	Dir        string
	Insecure   bool
}

type Agent struct {
	cfg     Config
	cores   *Cores
	lastRev string
	revMu   sync.Mutex
	writeMu sync.Mutex
	exit    func(int)
}

func Run(ctx context.Context, cfg Config) error {
	if err := os.MkdirAll(cfg.Dir, 0o750); err != nil {
		return err
	}
	a := &Agent{cfg: cfg, cores: NewCores(cfg.Dir), exit: os.Exit}
	defer a.cores.Close()
	backoff := time.Second
	for {
		err := a.session(ctx)
		if errors.Is(err, errSelfRestart) {
			a.exit(0)
			return nil
		}
		if errors.Is(err, errUninstalled) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			log.Printf("agent: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (a *Agent) session(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	u, err := url.Parse(a.cfg.ConnectURL)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "wss", "https":
		u.Scheme = "wss"
	case "ws", "http":
		u.Scheme = "ws"
		if !a.cfg.Insecure {
			return fmt.Errorf("明文 ws:// 需要 --insecure-connect")
		}
	default:
		return fmt.Errorf("不支持的 connect URL: %s", a.cfg.ConnectURL)
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, 15*time.Second)
	defer dialCancel()
	opts := &websocket.DialOptions{}
	if a.cfg.Insecure {
		opts.HTTPClient = &http.Client{
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec
			Timeout:   15 * time.Second,
		}
	}
	ws, _, err := websocket.Dial(dialCtx, u.String(), opts)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "bye")

	a.revMu.Lock()
	lastRev := a.lastRev
	a.revMu.Unlock()
	hello, _ := json.Marshal(wsproto.Hello{
		Token:        a.cfg.Token,
		AgentVersion: version.Version,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		LastRev:      lastRev,
		Cores:        detectedCores(),
		Caps:         []string{wsproto.CapCores, wsproto.CapProbe, wsproto.CapKick, wsproto.CapNFT},
	})
	if err := a.writeEnv(ctx, ws, wsproto.Envelope{Type: wsproto.TypeHello, ID: "hello", Payload: hello}); err != nil {
		return err
	}
	env, err := readEnv(ctx, ws, 15*time.Second)
	if err != nil {
		return err
	}
	if env.Type != wsproto.TypeHelloAck {
		return fmt.Errorf("expected hello_ack, got %s", env.Type)
	}
	var ack wsproto.HelloAck
	_ = json.Unmarshal(env.Payload, &ack)
	if ack.Error != "" {
		return fmt.Errorf("hello rejected: %s", ack.Error)
	}
	log.Printf("agent: connected as server %d %s", ack.ServerID, ack.Name)

	ping := time.NewTicker(10 * time.Second)
	defer ping.Stop()
	stats := time.NewTicker(5 * time.Second)
	defer stats.Stop()

	envCh := make(chan wsproto.Envelope, 8)
	errCh := make(chan error, 1)
	go func() {
		for {
			env, err := readEnv(ctx, ws, 60*time.Second)
			if err != nil {
				select {
				case errCh <- err:
				case <-ctx.Done():
				}
				return
			}
			select {
			case envCh <- env:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case env := <-envCh:
			if env.Type == wsproto.TypeApply || env.Type == wsproto.TypeEnsureCore || env.Type == wsproto.TypeRemoveCore || env.Type == wsproto.TypeProbe || env.Type == wsproto.TypeKick || env.Type == wsproto.TypeEnsureNFT {
				go func(env wsproto.Envelope) {
					var err error
					switch env.Type {
					case wsproto.TypeApply:
						err = a.handleApply(ctx, ws, env)
					case wsproto.TypeEnsureCore:
						err = a.handleEnsureCore(ctx, ws, env)
					case wsproto.TypeRemoveCore:
						err = a.handleRemoveCore(ctx, ws, env)
					case wsproto.TypeProbe:
						err = a.handleProbe(ctx, ws, env)
					case wsproto.TypeKick:
						err = a.handleKick(ctx, ws, env)
					case wsproto.TypeEnsureNFT:
						err = a.handleEnsureNFT(ctx, ws, env)
					}
					if err != nil {
						log.Printf("agent %s: %v", env.Type, err)
					}
				}(env)
				continue
			}
			if env.Type == wsproto.TypeUpgrade {
				go func(env wsproto.Envelope) {
					if err := a.handleUpgrade(ctx, ws, env); err != nil {
						if errors.Is(err, errSelfRestart) {
							a.exit(0)
							return
						}
						log.Printf("agent upgrade: %v", err)
					}
				}(env)
				continue
			}
			if env.Type == wsproto.TypeUninstall {
				if err := a.handle(ctx, ws, env); err != nil {
					if errors.Is(err, errUninstalled) {
						return err
					}
					log.Printf("agent handle: %v", err)
				}
				continue
			}
			if err := a.handle(ctx, ws, env); err != nil {
				log.Printf("agent handle: %v", err)
			}
		case <-ping.C:
			p, _ := json.Marshal(wsproto.Ping{TS: time.Now().UnixMilli()})
			if err := a.writeEnv(ctx, ws, wsproto.Envelope{Type: wsproto.TypePing, Payload: p}); err != nil {
				return err
			}
		case <-stats.C:
			samples, finishStats := a.cores.BeginCollect()
			up, down, hasNet := snapshotNet()
			diskFree, diskTotal := snapshotDisk()
			memAvail, memTotal := snapshotMem()
			st, _ := json.Marshal(wsproto.Stats{
				Samples:      samples,
				NetUp:        up,
				NetDown:      down,
				HasNet:       hasNet,
				Cores:        detectedCores(),
				CoresRunning: a.cores.Running(),
				DiskFree:     diskFree,
				DiskTotal:    diskTotal,
				MemAvail:     memAvail,
				MemTotal:     memTotal,
				LoadMilli:    snapshotLoadMilli(),
				Conns:        countEstablished(a.cores.ListenPorts()),
			})
			if err := a.writeEnv(ctx, ws, wsproto.Envelope{Type: wsproto.TypeStats, Payload: st}); err != nil {
				finishStats(false)
				return err
			}
			finishStats(true)
		}
	}
}

func (a *Agent) handle(ctx context.Context, ws *websocket.Conn, env wsproto.Envelope) error {
	switch env.Type {
	case wsproto.TypePong, wsproto.TypePing:
		return nil
	case wsproto.TypeUpgrade:
		return a.handleUpgrade(ctx, ws, env)
	case wsproto.TypeUninstall:
		return a.handleUninstall(ctx, ws, env)
	default:
		return nil
	}
}

func (a *Agent) handleUpgrade(ctx context.Context, ws *websocket.Conn, env wsproto.Envelope) error {
	var req wsproto.Upgrade
	if err := json.Unmarshal(env.Payload, &req); err != nil {
		return a.ackUpgrade(ctx, ws, env.ID, false, "malformed upgrade")
	}
	if strings.TrimSpace(req.URL) == "" {
		return a.ackUpgrade(ctx, ws, env.ID, false, "缺少下载地址")
	}
	upCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := a.replaceSelf(upCtx, req); err != nil {
		return a.ackUpgrade(ctx, ws, env.ID, false, err.Error())
	}
	if err := a.ackUpgrade(ctx, ws, env.ID, true, ""); err != nil {
		log.Printf("agent upgrade ack: %v", err)
	}
	return errSelfRestart
}

func (a *Agent) handleUninstall(ctx context.Context, ws *websocket.Conn, env wsproto.Envelope) error {
	if err := a.ackUninstall(ctx, ws, env.ID, true, ""); err != nil {
		return err
	}
	if err := a.uninstall(); err != nil {
		log.Printf("agent uninstall: %v", err)
	}
	return errUninstalled
}

func (a *Agent) ackUpgrade(ctx context.Context, ws *websocket.Conn, id string, ok bool, errMsg string) error {
	p, _ := json.Marshal(wsproto.UpgradeAck{OK: ok, Error: errMsg, Version: version.Version})
	return a.writeEnv(ctx, ws, wsproto.Envelope{Type: wsproto.TypeUpgradeAck, ID: id, Payload: p})
}

func (a *Agent) ackUninstall(ctx context.Context, ws *websocket.Conn, id string, ok bool, errMsg string) error {
	p, _ := json.Marshal(wsproto.UninstallAck{OK: ok, Error: errMsg})
	return a.writeEnv(ctx, ws, wsproto.Envelope{Type: wsproto.TypeUninstallAck, ID: id, Payload: p})
}

func (a *Agent) handleEnsureCore(ctx context.Context, ws *websocket.Conn, env wsproto.Envelope) error {
	var req wsproto.CoreOp
	if err := json.Unmarshal(env.Payload, &req); err != nil {
		return a.ackCoreOp(ctx, ws, wsproto.TypeEnsureCoreAck, env.ID, "", false, "malformed ensure_core")
	}
	name := normalizeAgentCore(req.Core)
	if !knownAgentCore(name) {
		return a.ackCoreOp(ctx, ws, wsproto.TypeEnsureCoreAck, env.ID, name, false, "未知内核")
	}
	_, err := ensureCore(name)
	cores := detectedCores()
	if err != nil {
		return a.ackCoreOp(ctx, ws, wsproto.TypeEnsureCoreAck, env.ID, name, false, err.Error(), cores)
	}
	return a.ackCoreOp(ctx, ws, wsproto.TypeEnsureCoreAck, env.ID, name, true, "", cores)
}

func (a *Agent) handleRemoveCore(ctx context.Context, ws *websocket.Conn, env wsproto.Envelope) error {
	var req wsproto.CoreOp
	if err := json.Unmarshal(env.Payload, &req); err != nil {
		return a.ackCoreOp(ctx, ws, wsproto.TypeRemoveCoreAck, env.ID, "", false, "malformed remove_core")
	}
	name := normalizeAgentCore(req.Core)
	if !knownAgentCore(name) {
		return a.ackCoreOp(ctx, ws, wsproto.TypeRemoveCoreAck, env.ID, name, false, "未知内核")
	}
	a.cores.Remove(name)
	err := removeCoreBin(name)
	cores := detectedCores()
	if err != nil {
		return a.ackCoreOp(ctx, ws, wsproto.TypeRemoveCoreAck, env.ID, name, false, err.Error(), cores)
	}
	return a.ackCoreOp(ctx, ws, wsproto.TypeRemoveCoreAck, env.ID, name, true, "", cores)
}

func (a *Agent) ackCoreOp(ctx context.Context, ws *websocket.Conn, typ, id, core string, ok bool, errMsg string, cores ...[]string) error {
	ack := wsproto.CoreOpAck{OK: ok, Error: errMsg, Core: core}
	if len(cores) > 0 {
		ack.Cores = cores[0]
	}
	p, _ := json.Marshal(ack)
	return a.writeEnv(ctx, ws, wsproto.Envelope{Type: typ, ID: id, Payload: p})
}

func (a *Agent) handleProbe(ctx context.Context, ws *websocket.Conn, env wsproto.Envelope) error {
	var req wsproto.Probe
	if err := json.Unmarshal(env.Payload, &req); err != nil {
		return a.ackProbe(ctx, ws, env.ID, false, "malformed probe", 0)
	}
	host := strings.TrimSpace(req.Host)
	if host == "" || req.Port < 1 || req.Port > 65535 {
		return a.ackProbe(ctx, ws, env.ID, false, "主机或端口无效", 0)
	}
	addr := net.JoinHostPort(host, strconv.Itoa(req.Port))
	dialCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	start := time.Now()
	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		msg := "超时"
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			msg = "超时"
		} else if errors.Is(err, context.DeadlineExceeded) {
			msg = "超时"
		} else {
			msg = err.Error()
		}
		return a.ackProbe(ctx, ws, env.ID, false, msg, 0)
	}
	_ = conn.Close()
	ms := time.Since(start).Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return a.ackProbe(ctx, ws, env.ID, true, "", ms)
}

func (a *Agent) ackProbe(ctx context.Context, ws *websocket.Conn, id string, ok bool, errMsg string, ms int64) error {
	p, _ := json.Marshal(wsproto.ProbeAck{OK: ok, Error: errMsg, LatencyMS: ms})
	return a.writeEnv(ctx, ws, wsproto.Envelope{Type: wsproto.TypeProbeAck, ID: id, Payload: p})
}

func (a *Agent) handleKick(ctx context.Context, ws *websocket.Conn, env wsproto.Envelope) error {
	var req wsproto.Kick
	if err := json.Unmarshal(env.Payload, &req); err != nil {
		return a.ackKick(ctx, ws, env.ID, false, "malformed kick", 0)
	}
	closed, err := a.cores.Kick(req.Emails)
	if err != nil {
		return a.ackKick(ctx, ws, env.ID, false, err.Error(), closed)
	}
	return a.ackKick(ctx, ws, env.ID, true, "", closed)
}

func (a *Agent) ackKick(ctx context.Context, ws *websocket.Conn, id string, ok bool, errMsg string, closed int) error {
	p, _ := json.Marshal(wsproto.KickAck{OK: ok, Error: errMsg, Closed: closed})
	return a.writeEnv(ctx, ws, wsproto.Envelope{Type: wsproto.TypeKickAck, ID: id, Payload: p})
}

func (a *Agent) handleEnsureNFT(ctx context.Context, ws *websocket.Conn, env wsproto.Envelope) error {
	err := installNFT()
	have := haveNFT()
	ok := err == nil && have
	msg := ""
	if !ok {
		if err != nil {
			msg = err.Error()
		} else {
			msg = "需要 nftables 才能拒绝中国 IP"
		}
	}
	p, _ := json.Marshal(wsproto.EnsureNFTAck{OK: ok, Error: msg, Have: have})
	return a.writeEnv(ctx, ws, wsproto.Envelope{Type: wsproto.TypeEnsureNFTAck, ID: env.ID, Payload: p})
}

func (a *Agent) handleApply(ctx context.Context, ws *websocket.Conn, env wsproto.Envelope) error {
	var cfg wsproto.ApplyConfig
	if err := json.Unmarshal(env.Payload, &cfg); err != nil {
		return a.ackApply(ctx, ws, env.ID, "", false, "malformed apply")
	}
	err := a.cores.Apply(cfg)
	cores := detectedCores()
	if err != nil {
		return a.ackApply(ctx, ws, env.ID, cfg.Rev, false, err.Error(), cores)
	}
	a.revMu.Lock()
	a.lastRev = cfg.Rev
	a.revMu.Unlock()
	return a.ackApply(ctx, ws, env.ID, cfg.Rev, true, "", cores)
}

func (a *Agent) ackApply(ctx context.Context, ws *websocket.Conn, id, rev string, ok bool, errMsg string, cores ...[]string) error {
	ack := wsproto.ApplyAck{Rev: rev, OK: ok, Error: errMsg}
	if len(cores) > 0 {
		ack.Cores = cores[0]
	}
	p, _ := json.Marshal(ack)
	return a.writeEnv(ctx, ws, wsproto.Envelope{Type: wsproto.TypeApplyAck, ID: id, Payload: p})
}

func (a *Agent) writeEnv(ctx context.Context, ws *websocket.Conn, env wsproto.Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return ws.Write(c, websocket.MessageText, b)
}

func readEnv(ctx context.Context, ws *websocket.Conn, d time.Duration) (wsproto.Envelope, error) {
	c, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	_, b, err := ws.Read(c)
	if err != nil {
		return wsproto.Envelope{}, err
	}
	var env wsproto.Envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return wsproto.Envelope{}, err
	}
	return env, nil
}
