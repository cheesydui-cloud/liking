package server

import (
	"log"
	"strconv"
	"strings"

	"liking/internal/corecfg"
	"liking/internal/db"
	"liking/internal/wsproto"
)

// wgCursor is the last kernel reading billed for one client on one agent.
// A drop is ignored once. Two low readings confirm a wg-quick reset and move
// the cursor without billing the absolute value.
type wgCursor struct {
	rx, tx       int64
	have         bool
	pendingReset bool
	pendingRx    int64
	pendingTx    int64
}

func wgCursorKey(serverID, clientID int64) string {
	return strconv.FormatInt(serverID, 10) + ":" + strconv.FormatInt(clientID, 10)
}

// wgGap is the part of a kernel counter that has not been written to
// traffic_daily for this user and inbound. A counter below the billed raw
// total is a new session, not a second copy of the old one.
func wgGap(kernel, billed int64) int64 {
	if kernel <= 0 || billed < 0 || kernel <= billed {
		return 0
	}
	return kernel - billed
}

// wgStep turns one peer reading into bytes to bill. live is false for the
// first sighting: that amount may be a catch-up of a whole session and must
// not show up as a realtime rate.
func wgStep(cur wgCursor, rx, tx, catchUp, catchDown int64) (wgCursor, int64, int64, bool) {
	if rx < 0 || tx < 0 {
		return cur, 0, 0, false
	}
	if !cur.have {
		if catchUp < 0 {
			catchUp = 0
		}
		if catchDown < 0 {
			catchDown = 0
		}
		return wgCursor{rx: rx, tx: tx, have: true}, catchUp, catchDown, false
	}
	rxDrop := rx < cur.rx
	txDrop := tx < cur.tx
	if rxDrop || txDrop {
		if cur.pendingReset && rx <= cur.pendingRx && tx <= cur.pendingTx {
			return wgCursor{rx: rx, tx: tx, have: true}, 0, 0, false
		}
		next := cur
		next.pendingReset = true
		next.pendingRx = rx
		next.pendingTx = tx
		var up, down int64
		if !rxDrop && rx > cur.rx {
			up = rx - cur.rx
			next.rx = rx
		}
		if !txDrop && tx > cur.tx {
			down = tx - cur.tx
			next.tx = tx
		}
		return next, up, down, up > 0 || down > 0
	}
	up := rx - cur.rx
	down := tx - cur.tx
	return wgCursor{rx: rx, tx: tx, have: true}, up, down, up > 0 || down > 0
}

func splitWGAllowed(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "(none)" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// wgPeerUnbilled is the exit default route and the handshake probe. Those
// counters are not one customer's traffic.
func wgPeerUnbilled(allowed string) bool {
	ips := splitWGAllowed(allowed)
	if len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		switch ip {
		case "0.0.0.0/0", "::/0", corecfg.WGProbeAddress, "(none)":
			return true
		}
	}
	return false
}

func (h *Hub) applyWGPeers(serverID int64, peers []wsproto.WGPeerStat) map[string]struct{} {
	type resolved struct {
		peer   wsproto.WGPeerStat
		client *db.Client
		in     *db.Inbound
	}
	var found []resolved
	var pending []wsproto.WGPeerStat
	taken := map[int64]struct{}{}
	for _, p := range peers {
		if p.Rx < 0 || p.Tx < 0 || wgPeerUnbilled(p.AllowedIPs) {
			continue
		}
		c, in := h.clientForWGPeer(serverID, p)
		if c == nil || in == nil {
			// A /32 with traffic is one customer. A wider net is the home
			// tunnel, and a transfer line with no address can be the exit.
			if wgPeerFallbackOK(p) && (p.Rx > 0 || p.Tx > 0) {
				pending = append(pending, p)
			}
			continue
		}
		found = append(found, resolved{peer: p, client: c, in: in})
		taken[c.ID] = struct{}{}
	}
	if len(pending) == 1 {
		if c, in := h.soleFreeWGClient(serverID, taken); c != nil && in != nil {
			found = append(found, resolved{peer: pending[0], client: c, in: in})
		}
	}
	matched := map[string]struct{}{}
	for _, r := range found {
		if r.client != nil && r.client.Email != "" {
			matched[r.client.Email] = struct{}{}
		}
	}
	if len(found) == 0 {
		return nil
	}

	type bill struct {
		client   *db.Client
		in       *db.Inbound
		up, down int64
		live     bool
		key      string
		next     wgCursor
	}
	h.wgMu.Lock()
	snap := map[string]wgCursor{}
	for _, r := range found {
		key := wgCursorKey(serverID, r.client.ID)
		if h.wgCursor != nil {
			snap[key] = h.wgCursor[key]
		}
	}
	h.wgMu.Unlock()

	var bills []bill
	for _, r := range found {
		key := wgCursorKey(serverID, r.client.ID)
		cur := snap[key]
		catchUp, catchDown := int64(0), int64(0)
		if !cur.have {
			catchUp, catchDown = h.wgCatchUp(r.client, r.peer.Rx, r.peer.Tx)
		}
		next, up, down, live := wgStep(cur, r.peer.Rx, r.peer.Tx, catchUp, catchDown)
		if up == 0 && down == 0 && next == cur {
			continue
		}
		bills = append(bills, bill{
			client: r.client, in: r.in, up: up, down: down, live: live, key: key, next: next,
		})
	}
	if len(bills) == 0 {
		return matched
	}

	day := db.ClockDay(h.DB)
	live := map[int64]map[int64]liveBytes{}
	var items []db.TrafficWrite
	touched := map[int64]struct{}{}
	pkgCache := map[int64]*db.Package{}
	pkgSeen := map[int64]struct{}{}
	var save []bill
	for _, b := range bills {
		if b.up == 0 && b.down == 0 {
			save = append(save, b)
			continue
		}
		u, err := db.GetUser(h.DB, b.client.UserID)
		if err != nil || u == nil {
			continue
		}
		mult := 1.0
		if u.PackageID != nil {
			pid := *u.PackageID
			if _, ok := pkgSeen[pid]; !ok {
				pkgSeen[pid] = struct{}{}
				if p, err := db.GetPackage(h.DB, pid); err == nil {
					pkgCache[pid] = p
				}
			}
			mult = db.PackageMultiplier(pkgCache[pid], b.in.ID)
		}
		upB, okUp := billBytes(b.up, mult)
		downB, okDown := billBytes(b.down, mult)
		if !okUp || !okDown {
			save = append(save, b)
			continue
		}
		if b.live {
			byIn := live[u.ID]
			if byIn == nil {
				byIn = map[int64]liveBytes{}
				live[u.ID] = byIn
			}
			a := byIn[b.in.ID]
			a.up += b.up
			a.down += b.down
			byIn[b.in.ID] = a
		}
		if upB != 0 || downB != 0 {
			items = append(items, db.TrafficWrite{
				UserID:     u.ID,
				InboundID:  b.in.ID,
				BilledUp:   upB,
				BilledDown: downB,
				RawUp:      b.up,
				RawDown:    b.down,
			})
			touched[u.ID] = struct{}{}
		}
		save = append(save, b)
	}
	if len(items) > 0 {
		if err := db.AddTrafficBatch(h.DB, day, items); err != nil {
			log.Printf("hub: wireguard traffic: %v", err)
			return matched
		}
	}
	h.wgMu.Lock()
	if h.wgCursor == nil {
		h.wgCursor = map[string]wgCursor{}
	}
	for _, b := range save {
		h.wgCursor[b.key] = b.next
	}
	h.wgMu.Unlock()
	h.noteUserLive(serverID, live)
	if h.OnTrafficUpdate != nil {
		for uid := range touched {
			h.OnTrafficUpdate(uid)
		}
	}
	return matched
}

func (h *Hub) wgCatchUp(c *db.Client, rx, tx int64) (int64, int64) {
	totals, err := db.UserInboundTrafficTotals(h.DB, c.UserID)
	if err != nil {
		log.Printf("hub: wireguard catch-up: %v", err)
		return 0, 0
	}
	raw := totals[c.InboundID]
	return wgGap(rx, raw.Up), wgGap(tx, raw.Down)
}

// wgPeerFallbackOK is a customer /32 the key lookup missed. Empty allowed
// IPs and anything wider than /32 stay unmatched so the exit is not billed
// to the only user on the machine.
func wgPeerFallbackOK(p wsproto.WGPeerStat) bool {
	ips := splitWGAllowed(p.AllowedIPs)
	if len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		switch ip {
		case "0.0.0.0/0", "::/0", corecfg.WGProbeAddress, "(none)":
			return false
		}
		if i := strings.IndexByte(ip, '/'); i > 0 {
			n, err := strconv.Atoi(strings.TrimSpace(ip[i+1:]))
			if err != nil || n < 32 {
				return false
			}
		}
	}
	return true
}

func (h *Hub) soleFreeWGClient(serverID int64, taken map[int64]struct{}) (*db.Client, *db.Inbound) {
	ins, err := db.ListInboundsByServer(h.DB, serverID)
	if err != nil {
		return nil, nil
	}
	var found *db.Client
	var foundIn *db.Inbound
	for _, in := range ins {
		if in == nil || in.Profile != corecfg.ProfileWireGuard {
			continue
		}
		list, err := db.ListClientsByInbound(h.DB, in.ID)
		if err != nil {
			continue
		}
		for _, c := range list {
			if c == nil || strings.HasPrefix(c.Email, "relay.") {
				continue
			}
			if _, ok := taken[c.ID]; ok {
				continue
			}
			if _, ok := h.wgInboundOnServer(serverID, c); !ok {
				continue
			}
			if found != nil && found.ID != c.ID {
				return nil, nil
			}
			found, foundIn = c, in
		}
	}
	return found, foundIn
}

func wgUUIDCandidates(ip string) []string {
	ip = strings.TrimSpace(ip)
	switch ip {
	case "", "(none)", "0.0.0.0/0", "::/0", corecfg.WGProbeAddress:
		return nil
	}
	if i := strings.IndexByte(ip, '/'); i > 0 {
		host := strings.TrimSpace(ip[:i])
		if host == "" || host == ip {
			return []string{ip}
		}
		return []string{ip, host}
	}
	return []string{ip, ip + "/32"}
}

func (h *Hub) clientForWGPeer(serverID int64, p wsproto.WGPeerStat) (*db.Client, *db.Inbound) {
	if c, in := h.wgClientByKey(serverID, p.PublicKey); c != nil {
		return c, in
	}
	if email := strings.TrimSpace(p.Email); email != "" && !strings.HasPrefix(email, "relay.") {
		c, err := db.ClientByIdentity(h.DB, email)
		if err == nil && c != nil {
			if in, ok := h.wgInboundOnServer(serverID, c); ok {
				return c, in
			}
		}
	}
	var found *db.Client
	var foundIn *db.Inbound
	for _, ip := range splitWGAllowed(p.AllowedIPs) {
		cands := wgUUIDCandidates(ip)
		if len(cands) == 0 {
			continue
		}
		list, err := db.ClientsByUUIDs(h.DB, cands)
		if err != nil || len(list) == 0 {
			continue
		}
		for _, c := range list {
			in, ok := h.wgInboundOnServer(serverID, c)
			if !ok {
				continue
			}
			if found != nil && found.ID != c.ID {
				return nil, nil
			}
			found, foundIn = c, in
		}
	}
	return found, foundIn
}

func (h *Hub) wgClientByKey(serverID int64, pub string) (*db.Client, *db.Inbound) {
	pub = strings.TrimSpace(pub)
	if pub == "" || pub == "(none)" {
		return nil, nil
	}
	names := []string{pub}
	if c := wsproto.CanonicalWGKey(pub); c != "" && c != pub {
		names = append(names, c)
	}
	list, err := db.ClientsByUsernames(h.DB, names)
	if err != nil || len(list) == 0 {
		return nil, nil
	}
	var found *db.Client
	var foundIn *db.Inbound
	for _, c := range list {
		in, ok := h.wgInboundOnServer(serverID, c)
		if !ok {
			continue
		}
		if found != nil && found.ID != c.ID {
			return nil, nil
		}
		found, foundIn = c, in
	}
	return found, foundIn
}

func (h *Hub) wgInboundOnServer(serverID int64, c *db.Client) (*db.Inbound, bool) {
	if c == nil || strings.HasPrefix(c.Email, "relay.") {
		return nil, false
	}
	in, err := db.GetInbound(h.DB, c.InboundID)
	if err != nil || in == nil || in.ServerID != serverID || in.Profile != corecfg.ProfileWireGuard {
		return nil, false
	}
	return in, true
}
