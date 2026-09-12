package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"liking/internal/db"
	"liking/internal/wsproto"
)

const (
	hubWriteTimeout    = 10 * time.Second
	hubReadTimeout     = 30 * time.Second
	applyAckTimeout    = 60 * time.Second
	hubMaxReadBytes    = 4 << 20
	hubWriteTimeoutMax = 5 * time.Minute
)

func writeTimeoutFor(size int) time.Duration {
	const perMB = 3 * time.Second
	d := hubWriteTimeout + time.Duration(size/(1<<20))*perMB
	if d > hubWriteTimeoutMax {
		return hubWriteTimeoutMax
	}
	return d
}

type Hub struct {
	DB              *sql.DB
	OnTrafficUpdate func(userID int64)
	Redispatch      func(serverIDs []int64)

	mu    sync.RWMutex
	conns map[int64]*agentConn
}

func NewHub(d *sql.DB) *Hub {
	return &Hub{DB: d, conns: map[int64]*agentConn{}}
}

type agentConn struct {
	serverID  int64
	arch      string
	ws        *websocket.Conn
	writeCh   chan []byte
	closed    chan struct{}
	closeOnce sync.Once
	pendMu    sync.Mutex
	pending   map[string]chan json.RawMessage
	idSeq     atomic.Uint64
}

func (a *agentConn) nextID() string {
	return strconv.FormatUint(a.idSeq.Add(1), 36)
}

func (a *agentConn) signalClose() {
	a.closeOnce.Do(func() { close(a.closed) })
}

func (h *Hub) IsOnline(id int64) bool {
	h.mu.RLock()
	_, ok := h.conns[id]
	h.mu.RUnlock()
	return ok
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("hub: accept: %v", err)
		return
	}
	ws.SetReadLimit(hubMaxReadBytes)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	helloEnv, err := readEnvelope(ctx, ws, hubReadTimeout)
	if err != nil || helloEnv.Type != wsproto.TypeHello {
		writeError(ctx, ws, "protocol", "expected hello as first frame")
		ws.Close(websocket.StatusPolicyViolation, "no hello")
		return
	}
	var hello wsproto.Hello
	if err := json.Unmarshal(helloEnv.Payload, &hello); err != nil {
		writeError(ctx, ws, "protocol", "malformed hello")
		ws.Close(websocket.StatusPolicyViolation, "bad hello")
		return
	}
	srv, err := db.GetServerByToken(h.DB, hello.Token)
	if err != nil || srv == nil {
		ack, _ := json.Marshal(wsproto.HelloAck{Error: "unknown or revoked token"})
		writeEnvelope(ctx, ws, wsproto.Envelope{Type: wsproto.TypeHelloAck, ID: helloEnv.ID, Payload: ack})
		ws.Close(websocket.StatusPolicyViolation, "bad token")
		return
	}

	ac := &agentConn{
		serverID: srv.ID,
		arch:     hello.Arch,
		ws:       ws,
		writeCh:  make(chan []byte, 16),
		closed:   make(chan struct{}),
		pending:  map[string]chan json.RawMessage{},
	}
	h.registerConn(ac)
	defer h.unregisterConn(ac)

	ackPayload, _ := json.Marshal(wsproto.HelloAck{ServerID: srv.ID, Name: srv.Name})
	if err := writeEnvelope(ctx, ws, wsproto.Envelope{Type: wsproto.TypeHelloAck, ID: helloEnv.ID, Payload: ackPayload}); err != nil {
		ws.Close(websocket.StatusInternalError, "ack write failed")
		return
	}
	if err := db.MarkServerOnline(h.DB, srv.ID, hello.AgentVersion, hello.OS, hello.Arch, extractIP(r), hello.Cores); err != nil {
		log.Printf("hub: MarkServerOnline: %v", err)
	}
	h.reconcileOnConnect(srv.ID, hello.LastRev)

	go h.writerLoop(ac)
	h.readerLoop(ctx, ac)
}

func (h *Hub) reconcileOnConnect(serverID int64, lastRev string) {
	if h.Redispatch == nil {
		return
	}
	s, err := db.GetServer(h.DB, serverID)
	if err != nil {
		return
	}
	if lastRev != "" && s.ConfigRev != "" && lastRev == s.ConfigRev {
		return
	}
	go h.Redispatch([]int64{serverID})
}

func (h *Hub) registerConn(ac *agentConn) {
	h.mu.Lock()
	if old, ok := h.conns[ac.serverID]; ok {
		old.signalClose()
		old.ws.Close(websocket.StatusGoingAway, "replaced")
	}
	h.conns[ac.serverID] = ac
	h.mu.Unlock()
}

func (h *Hub) unregisterConn(ac *agentConn) {
	h.mu.Lock()
	if cur, ok := h.conns[ac.serverID]; ok && cur == ac {
		delete(h.conns, ac.serverID)
	}
	h.mu.Unlock()
	ac.signalClose()
	_ = db.MarkServerOffline(h.DB, ac.serverID)
}

func (h *Hub) Drop(id int64) {
	h.mu.Lock()
	ac, ok := h.conns[id]
	if ok {
		delete(h.conns, id)
	}
	h.mu.Unlock()
	if !ok {
		return
	}
	ac.signalClose()
	_ = ac.ws.Close(websocket.StatusGoingAway, "server deleted")
	_ = db.MarkServerOffline(h.DB, id)
}

func (h *Hub) Close() {
	h.mu.Lock()
	conns := make([]*agentConn, 0, len(h.conns))
	for _, ac := range h.conns {
		conns = append(conns, ac)
	}
	h.conns = map[int64]*agentConn{}
	h.mu.Unlock()
	for _, ac := range conns {
		ac.signalClose()
		_ = ac.ws.Close(websocket.StatusGoingAway, "panel shutting down")
	}
}

func (h *Hub) writerLoop(ac *agentConn) {
	for {
		select {
		case <-ac.closed:
			return
		case b := <-ac.writeCh:
			ctx, cancel := context.WithTimeout(context.Background(), writeTimeoutFor(len(b)))
			err := ac.ws.Write(ctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				ac.ws.Close(websocket.StatusInternalError, "write error")
				return
			}
		}
	}
}

func (h *Hub) readerLoop(parent context.Context, ac *agentConn) {
	for {
		ctx, cancel := context.WithTimeout(parent, hubReadTimeout)
		_, b, err := ac.ws.Read(ctx)
		cancel()
		if err != nil {
			return
		}
		var env wsproto.Envelope
		if err := json.Unmarshal(b, &env); err != nil {
			log.Printf("hub: malformed envelope from server %d: %v", ac.serverID, err)
			continue
		}
		switch env.Type {
		case wsproto.TypePing:
			pong, _ := json.Marshal(wsproto.Ping{TS: time.Now().UnixMilli()})
			ac.enqueueWrite(wsproto.Envelope{Type: wsproto.TypePong, ID: env.ID, Payload: pong})
			_ = db.TouchServerLastSeen(h.DB, ac.serverID)
		case wsproto.TypeStats:
			var st wsproto.Stats
			if err := json.Unmarshal(env.Payload, &st); err != nil {
				continue
			}
			h.applyStats(st.Samples)
		case wsproto.TypeApplyAck, wsproto.TypeHelloAck:
			ac.dispatchAck(env)
		default:
			log.Printf("hub: server %d unknown frame %q", ac.serverID, env.Type)
		}
	}
}

func (ac *agentConn) enqueueWrite(env wsproto.Envelope) {
	b, err := json.Marshal(env)
	if err != nil {
		return
	}
	select {
	case ac.writeCh <- b:
	case <-ac.closed:
	}
}

func (ac *agentConn) dispatchAck(env wsproto.Envelope) {
	ac.pendMu.Lock()
	ch, ok := ac.pending[env.ID]
	if ok {
		delete(ac.pending, env.ID)
	}
	ac.pendMu.Unlock()
	if ok {
		ch <- env.Payload
	}
}

func (h *Hub) SendApply(serverID int64, cfg wsproto.ApplyConfig) error {
	h.mu.RLock()
	ac, ok := h.conns[serverID]
	h.mu.RUnlock()
	if !ok {
		return fmt.Errorf("server %d not connected", serverID)
	}
	id := ac.nextID()
	ch := make(chan json.RawMessage, 1)
	ac.pendMu.Lock()
	ac.pending[id] = ch
	ac.pendMu.Unlock()
	defer func() {
		ac.pendMu.Lock()
		delete(ac.pending, id)
		ac.pendMu.Unlock()
	}()

	payload, _ := json.Marshal(cfg)
	ac.enqueueWrite(wsproto.Envelope{Type: wsproto.TypeApply, ID: id, Payload: payload})

	select {
	case raw := <-ch:
		var ack wsproto.ApplyAck
		if err := json.Unmarshal(raw, &ack); err != nil {
			return fmt.Errorf("malformed apply_ack: %w", err)
		}
		if !ack.OK {
			msg := strings.TrimSpace(ack.Error)
			if msg == "" {
				return fmt.Errorf("配置下发被拒绝")
			}
			return fmt.Errorf("%s", msg)
		}
		return nil
	case <-time.After(applyAckTimeout):
		return errors.New("apply_ack timeout")
	case <-ac.closed:
		return errors.New("connection closed before ack")
	}
}

func (h *Hub) applyStats(samples []wsproto.Sample) {
	day := time.Now().Format("2006-01-02")
	touched := map[int64]struct{}{}
	for _, s := range samples {
		if s.Email == "" || strings.HasPrefix(s.Email, "relay.") {
			continue
		}
		c, err := db.ClientByEmail(h.DB, s.Email)
		if err != nil {
			continue
		}
		up, down := s.Up, s.Down
		if up == 0 && down == 0 {
			continue
		}
		in, err := db.GetInbound(h.DB, c.InboundID)
		if err != nil {
			continue
		}
		u, err := db.GetUser(h.DB, c.UserID)
		if err != nil {
			continue
		}
		mult := 1.0
		if u.PackageID != nil {
			if p, err := db.GetPackage(h.DB, *u.PackageID); err == nil {
				mult = db.PackageMultiplier(p, in.ID)
			}
		}
		upB := int64(float64(up) * mult)
		downB := int64(float64(down) * mult)
		_ = db.AddUserTraffic(h.DB, u.ID, upB, downB)
		_ = db.AddDailyTraffic(h.DB, day, u.ID, in.ID, up, down)
		touched[u.ID] = struct{}{}
	}
	if h.OnTrafficUpdate != nil {
		for uid := range touched {
			h.OnTrafficUpdate(uid)
		}
	}
}

func readEnvelope(ctx context.Context, ws *websocket.Conn, timeout time.Duration) (wsproto.Envelope, error) {
	c, cancel := context.WithTimeout(ctx, timeout)
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

func writeEnvelope(ctx context.Context, ws *websocket.Conn, env wsproto.Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, writeTimeoutFor(len(b)))
	defer cancel()
	return ws.Write(c, websocket.MessageText, b)
}

func writeError(ctx context.Context, ws *websocket.Conn, code, msg string) {
	p, _ := json.Marshal(wsproto.Error{Code: code, Message: msg})
	_ = writeEnvelope(ctx, ws, wsproto.Envelope{Type: wsproto.TypeError, Payload: p})
}

func extractIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
