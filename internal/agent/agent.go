package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/coder/websocket"

	"liking/internal/version"
	"liking/internal/wsproto"
)

type Config struct {
	ConnectURL string
	Token      string
	Dir        string
	Insecure   bool
}

type Agent struct {
	cfg     Config
	cores   *Cores
	lastRev string
	revMu   sync.Mutex
	writeMu sync.Mutex
}

func Run(ctx context.Context, cfg Config) error {
	if err := os.MkdirAll(cfg.Dir, 0o750); err != nil {
		return err
	}
	a := &Agent{cfg: cfg, cores: NewCores(cfg.Dir)}
	backoff := time.Second
	for {
		err := a.session(ctx)
		if ctx.Err() != nil {
			a.cores.StopAll()
			return ctx.Err()
		}
		if err != nil {
			log.Printf("agent: %v", err)
		}
		select {
		case <-ctx.Done():
			a.cores.StopAll()
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (a *Agent) session(ctx context.Context) error {
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

	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
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
				errCh <- err
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
			if env.Type == wsproto.TypeApply {
				go func(env wsproto.Envelope) {
					if err := a.handleApply(ctx, ws, env); err != nil {
						log.Printf("agent apply: %v", err)
					}
				}(env)
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
			samples := a.cores.Collect()
			up, down, hasNet := snapshotNet()
			st, _ := json.Marshal(wsproto.Stats{
				Samples: samples,
				NetUp:   up,
				NetDown: down,
				HasNet:  hasNet,
				Cores:   detectedCores(),
			})
			if err := a.writeEnv(ctx, ws, wsproto.Envelope{Type: wsproto.TypeStats, Payload: st}); err != nil {
				return err
			}
		}
	}
}

func (a *Agent) handle(ctx context.Context, ws *websocket.Conn, env wsproto.Envelope) error {
	switch env.Type {
	case wsproto.TypePong, wsproto.TypePing:
		return nil
	default:
		return nil
	}
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
