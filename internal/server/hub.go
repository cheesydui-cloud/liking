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
	applyAckTimeout    = 2 * time.Minute
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

type userLivePart struct {
	up, down int64
	at       time.Time
}

type Hub struct {
	DB              *sql.DB
	OnTrafficUpdate func(userID int64)
	Redispatch      func(serverIDs []int64)

	mu    sync.RWMutex
	conns map[int64]*agentConn

	userLiveMu sync.Mutex
	userParts  map[int64]map[int64]userLivePart // userID -> serverID -> bps
	userPartAt map[int64]time.Time              // serverID -> last stats time
}

const userLiveTTL = 15 * time.Second

func NewHub(d *sql.DB) *Hub {
	return &Hub{DB: d, conns: map[int64]*agentConn{}}
}

type agentConn struct {
	serverID  int64
	osName    string
	arch      string
	ws        *websocket.Conn
	writeCh   chan []byte
	closed    chan struct{}
	closeOnce sync.Once
	pendMu    sync.Mutex
	pending   map[string]chan json.RawMessage
	idSeq     atomic.Uint64

	liveMu       sync.Mutex
	upBps        int64
	downBps      int64
	lastStatsAt  time.Time
	diskFree     int64
	diskTotal    int64
	memAvail     int64
	memTotal     int64
	loadMilli    int64
	conns        int
	coresRunning string
	caps         []string
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
		osName:   hello.OS,
		ws:       ws,
		writeCh:  make(chan []byte, 16),
		closed:   make(chan struct{}),
		pending:  map[string]chan json.RawMessage{},
		caps:     append([]string{}, hello.Caps...),
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
		h.mu.Unlock()
		h.clearUserLive(ac.serverID)
	} else {
		h.mu.Unlock()
	}
	ac.signalClose()
	_ = db.MarkServerOffline(h.DB, ac.serverID)
}

func (h *Hub) DropAll() {
	h.mu.Lock()
	conns := make([]*agentConn, 0, len(h.conns))
	for id, ac := range h.conns {
		conns = append(conns, ac)
		delete(h.conns, id)
	}
	h.mu.Unlock()
	h.clearAllUserLive()
	for _, ac := range conns {
		ac.signalClose()
		_ = ac.ws.Close(websocket.StatusGoingAway, "panel restore")
	}
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
	h.clearUserLive(id)
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
	h.clearAllUserLive()
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
			if len(st.Cores) > 0 {
				_ = db.SetServerCores(h.DB, ac.serverID, st.Cores)
			}
			h.noteLive(ac, st)
			h.applyStats(ac.serverID, st.Samples)
		case wsproto.TypeApplyAck, wsproto.TypeHelloAck, wsproto.TypeUpgradeAck, wsproto.TypeUninstallAck, wsproto.TypeEnsureCoreAck, wsproto.TypeRemoveCoreAck, wsproto.TypeProbeAck:
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

func (h *Hub) SendRPC(serverID int64, typ string, payload any, timeout time.Duration) (json.RawMessage, error) {
	h.mu.RLock()
	ac, ok := h.conns[serverID]
	h.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("Agent 不在线")
	}
	if timeout <= 0 {
		timeout = applyAckTimeout
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

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ac.enqueueWrite(wsproto.Envelope{Type: typ, ID: id, Payload: raw})

	select {
	case ack := <-ch:
		return ack, nil
	case <-time.After(timeout):
		return nil, errors.New("等待 Agent 回复超时")
	case <-ac.closed:
		return nil, errors.New("连接在回复前断开")
	}
}

func (h *Hub) SendApply(serverID int64, cfg wsproto.ApplyConfig) error {
	raw, err := h.SendRPC(serverID, wsproto.TypeApply, cfg, applyAckTimeout)
	if err != nil {
		if err.Error() == "Agent 不在线" {
			return fmt.Errorf("server %d not connected", serverID)
		}
		return err
	}
	var ack wsproto.ApplyAck
	if err := json.Unmarshal(raw, &ack); err != nil {
		return fmt.Errorf("malformed apply_ack: %w", err)
	}
	if len(ack.Cores) > 0 {
		_ = db.SetServerCores(h.DB, serverID, ack.Cores)
	}
	if !ack.OK {
		msg := strings.TrimSpace(ack.Error)
		if msg == "" {
			return fmt.Errorf("配置下发被拒绝")
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (h *Hub) HasCap(id int64, cap string) bool {
	if cap == "" {
		return false
	}
	h.mu.RLock()
	ac, ok := h.conns[id]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	for _, c := range ac.caps {
		if c == cap {
			return true
		}
	}
	return false
}

func (h *Hub) ConnMeta(id int64) (osName, arch string, ok bool) {
	h.mu.RLock()
	ac, exists := h.conns[id]
	h.mu.RUnlock()
	if !exists {
		return "", "", false
	}
	return ac.osName, ac.arch, true
}

func (h *Hub) Health(id int64) (wsproto.Stats, bool) {
	h.mu.RLock()
	ac, exists := h.conns[id]
	h.mu.RUnlock()
	if !exists {
		return wsproto.Stats{}, false
	}
	ac.liveMu.Lock()
	defer ac.liveMu.Unlock()
	if ac.lastStatsAt.IsZero() || time.Since(ac.lastStatsAt) > 90*time.Second {
		return wsproto.Stats{}, false
	}
	var running []string
	if ac.coresRunning != "" {
		running = strings.Split(ac.coresRunning, ",")
	}
	return wsproto.Stats{
		DiskFree:     ac.diskFree,
		DiskTotal:    ac.diskTotal,
		MemAvail:     ac.memAvail,
		MemTotal:     ac.memTotal,
		LoadMilli:    ac.loadMilli,
		Conns:        ac.conns,
		CoresRunning: running,
	}, true
}

func (h *Hub) Live(id int64) (up, down int64, ok bool) {
	h.mu.RLock()
	ac, exists := h.conns[id]
	h.mu.RUnlock()
	if !exists {
		return 0, 0, false
	}
	ac.liveMu.Lock()
	defer ac.liveMu.Unlock()
	if ac.lastStatsAt.IsZero() || time.Since(ac.lastStatsAt) > 90*time.Second {
		return 0, 0, false
	}
	return ac.upBps, ac.downBps, true
}

func (h *Hub) noteLive(ac *agentConn, st wsproto.Stats) {
	now := time.Now()
	ac.liveMu.Lock()
	defer ac.liveMu.Unlock()
	ac.diskFree = st.DiskFree
	ac.diskTotal = st.DiskTotal
	ac.memAvail = st.MemAvail
	ac.memTotal = st.MemTotal
	ac.loadMilli = st.LoadMilli
	ac.conns = st.Conns
	if len(st.CoresRunning) > 0 {
		ac.coresRunning = strings.Join(st.CoresRunning, ",")
	}
	if st.HasNet {
		ac.upBps = st.NetUp
		ac.downBps = st.NetDown
		ac.lastStatsAt = now
		return
	}
	var up, down int64
	for _, s := range st.Samples {
		up += s.Up
		down += s.Down
	}
	if !ac.lastStatsAt.IsZero() {
		dt := now.Sub(ac.lastStatsAt).Seconds()
		if dt > 0.2 {
			ac.upBps = int64(float64(up) / dt)
			ac.downBps = int64(float64(down) / dt)
		}
	}
	ac.lastStatsAt = now
}

type liveBytes struct{ up, down int64 }

func (h *Hub) applyStats(serverID int64, samples []wsproto.Sample) {
	day := db.ClockDay(h.DB)
	touched := map[int64]struct{}{}
	live := map[int64]liveBytes{}
	var items []db.TrafficWrite
	for _, s := range samples {
		if s.Email == "" || strings.HasPrefix(s.Email, "relay.") {
			continue
		}
		c, err := db.ClientByIdentity(h.DB, s.Email)
		if err != nil {
			continue
		}
		up, down := s.Up, s.Down
		if up == 0 && down == 0 {
			continue
		}
		a := live[c.UserID]
		a.up += up
		a.down += down
		live[c.UserID] = a
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
		items = append(items, db.TrafficWrite{
			UserID:     u.ID,
			InboundID:  in.ID,
			BilledUp:   upB,
			BilledDown: downB,
			RawUp:      up,
			RawDown:    down,
		})
		touched[u.ID] = struct{}{}
	}
	h.noteUserLive(serverID, live)
	if err := db.AddTrafficBatch(h.DB, day, items); err != nil {
		log.Printf("hub: traffic batch: %v", err)
		return
	}
	if h.OnTrafficUpdate != nil {
		for uid := range touched {
			h.OnTrafficUpdate(uid)
		}
	}
}

func (h *Hub) noteUserLive(serverID int64, live map[int64]liveBytes) {
	now := time.Now()
	h.userLiveMu.Lock()
	defer h.userLiveMu.Unlock()
	if h.userParts == nil {
		h.userParts = map[int64]map[int64]userLivePart{}
	}
	if h.userPartAt == nil {
		h.userPartAt = map[int64]time.Time{}
	}
	dt := 5.0
	if prev, ok := h.userPartAt[serverID]; ok {
		d := now.Sub(prev).Seconds()
		if d > 0.2 && d < 60 {
			dt = d
		}
	}
	h.userPartAt[serverID] = now
	for uid, parts := range h.userParts {
		if _, ok := live[uid]; ok {
			continue
		}
		delete(parts, serverID)
		if len(parts) == 0 {
			delete(h.userParts, uid)
		}
	}
	for uid, b := range live {
		parts := h.userParts[uid]
		if parts == nil {
			parts = map[int64]userLivePart{}
			h.userParts[uid] = parts
		}
		parts[serverID] = userLivePart{
			up:   int64(float64(b.up) / dt),
			down: int64(float64(b.down) / dt),
			at:   now,
		}
	}
}

func (h *Hub) clearUserLive(serverID int64) {
	h.userLiveMu.Lock()
	defer h.userLiveMu.Unlock()
	delete(h.userPartAt, serverID)
	for uid, parts := range h.userParts {
		delete(parts, serverID)
		if len(parts) == 0 {
			delete(h.userParts, uid)
		}
	}
}

func (h *Hub) clearAllUserLive() {
	h.userLiveMu.Lock()
	h.userParts = nil
	h.userPartAt = nil
	h.userLiveMu.Unlock()
}

func (h *Hub) UserLive(id int64) (up, down int64, ok bool) {
	h.userLiveMu.Lock()
	defer h.userLiveMu.Unlock()
	now := time.Now()
	for _, p := range h.userParts[id] {
		if now.Sub(p.at) > userLiveTTL {
			continue
		}
		up += p.up
		down += p.down
		ok = true
	}
	return
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
