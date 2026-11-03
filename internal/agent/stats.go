package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"liking/internal/wsproto"
)

var clashHTTP = &http.Client{Timeout: 2 * time.Second}

type bytePair struct{ up, down int64 }

type statRow struct {
	Name  string
	Value int64
}

type clashSnap struct {
	up, down int64
	user     string
}

type clashConn struct {
	ID     string
	User   string
	Up     int64
	Down   int64
	Start  time.Time
	Chains []string
}

func (c *Cores) Collect() []wsproto.Sample {
	samples, finish := c.BeginCollect()
	finish(true)
	return samples
}

// BeginCollect snapshots counters without moving the cursor. Commit only after
// the stats frame is written, so a failed send does not drop the interval.
// A skipped lock returns a no-op finish and must not be treated as zero traffic.
func (c *Cores) BeginCollect() ([]wsproto.Sample, func(commit bool)) {
	if !c.collectMu.TryLock() {
		return nil, func(bool) {}
	}
	var all []wsproto.Sample
	var xrayNext map[string]bytePair
	var clashNext map[string]clashSnap
	var singNext map[string]bytePair
	var mitaNext map[string]bytePair
	var wgNext map[string]bytePair
	xrayOK, clashOK, singOK, mitaOK, wgOK := false, false, false, false, false
	if samples, next, _, ok := c.peekXray(); ok {
		all = append(all, samples...)
		xrayNext, xrayOK = next, true
	}
	// sing-box 1.14 clash /connections has no username, so those samples cannot
	// be attributed. v2ray_api counters include the name. Use clash only when
	// that query is unavailable, and never add both in one tick.
	if samples, next, _, ok := c.peekSingboxStats(); ok {
		all = append(all, samples...)
		singNext, singOK = next, true
	} else if samples, next, _, ok := c.peekClash(); ok {
		all = append(all, samples...)
		clashNext, clashOK = next, true
	}
	if samples, next, _, ok := c.peekMita(); ok {
		all = append(all, samples...)
		mitaNext, mitaOK = next, true
	}
	if peers, ok := c.peekWireGuardPeers(); ok && len(peers) > 0 {
		peers = c.annotateWGPeers(peers)
		c.pendingWG = peers
		c.pendingHasWG = true
		// Email deltas stay for an older panel, and for a peer the panel
		// cannot match. A matched peer is billed from the kernel counters,
		// so the panel skips that email and the same bytes are not added twice.
		if samples, next, ok := c.wgEmailDeltas(peers); ok {
			all = append(all, samples...)
			wgNext, wgOK = next, true
		}
	} else {
		c.pendingWG = nil
		c.pendingHasWG = false
	}
	// Core counters and socket marks measure the same interval. Vision splice
	// freezes user>>> counters after the handshake while the marked socket
	// keeps growing. A zero core delta must not hide that socket. Take the
	// larger side of each direction so a live core counter is not added again.
	flowSamples, flowNext, portNext, flowOK := c.peekFlows()
	merged := maxMergeSamples(all, flowSamples)
	if len(merged) == 0 && portRatesMoving(c.pendingPorts) {
		noteStatsFail("用户流量", fmt.Errorf("监听端口有流量，但没有归到用户"))
	}
	finished := false
	finish := func(commit bool) {
		if finished {
			return
		}
		finished = true
		if commit {
			if xrayOK {
				c.xrayLast = xrayNext
			}
			if singOK {
				c.singboxStatsLast = singNext
				// A later clash fallback must not bill the interval already counted here.
				c.clashLast = map[string]clashSnap{}
			}
			if clashOK {
				c.clashLast = clashNext
			}
			if mitaOK {
				c.mitaLast = mitaNext
			}
			if wgOK {
				c.mu.Lock()
				c.wgLast = wgNext
				c.mu.Unlock()
			}
			if flowOK {
				c.flowLast = flowNext
				c.portLast = portNext
				c.flowAt = time.Now()
			}
			if c.pendingWGListenOK {
				c.wgListenCur = c.pendingWGListen
				if c.wgListenCurByPort == nil {
					c.wgListenCurByPort = map[int]wgListenCursor{}
				}
				for port, cur := range c.pendingWGListenByPort {
					c.wgListenCurByPort[port] = cur
				}
			}
		}
		c.pendingPorts = nil
		c.pendingHasPorts = false
		c.pendingWGListenOK = false
		c.pendingWGListenByPort = nil
		c.pendingWGByPort = nil
		if commit && c.pendingClearHandoff {
			c.clearWGHandoff()
		}
		c.pendingClearHandoff = false
		c.pendingWG = nil
		c.pendingHasWG = false
		c.collectMu.Unlock()
	}
	return merged, finish
}

// TakePorts returns listen-port rates from the open collect. Call it before finish.
func (c *Cores) TakePorts() ([]wsproto.PortRate, bool) {
	p, ok := c.pendingPorts, c.pendingHasPorts
	return p, ok
}

// TakeWGPeers returns kernel peer counters from the open collect. Call it before finish.
func (c *Cores) TakeWGPeers() ([]wsproto.WGPeerStat, bool) {
	return c.pendingWG, c.pendingHasWG
}

func emailsOf(samples []wsproto.Sample) map[string]struct{} {
	out := map[string]struct{}{}
	for _, s := range samples {
		if s.Email != "" && (s.Up != 0 || s.Down != 0) {
			out[s.Email] = struct{}{}
		}
	}
	return out
}

func hasUserCounter(rows []statRow) bool {
	for _, row := range rows {
		parts := strings.Split(row.Name, ">>>")
		if len(parts) == 4 && parts[0] == "user" && parts[2] == "traffic" && row.Value > 0 {
			return true
		}
	}
	return false
}

func portRatesMoving(ports []wsproto.PortRate) bool {
	for _, p := range ports {
		if p.UpBps > 0 || p.DownBps > 0 {
			return true
		}
	}
	return false
}

func (c *Cores) peekXray() ([]wsproto.Sample, map[string]bytePair, map[string]struct{}, bool) {
	c.mu.Lock()
	bin, api := c.xrayBin, c.xrayAPI
	c.mu.Unlock()
	var grpcRows []statRow
	grpcOK := false
	if api != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
		rows, err := queryXrayGRPCFn(ctx, api)
		cancel()
		if err == nil {
			grpcRows = rows
			grpcOK = true
		} else {
			noteStatsFail("xray", err)
		}
	}
	// Empty gRPC success is not proof that user counters are absent. The CLI
	// still sees them when the HTTP/2 query decoded nothing.
	if bin != "" && api != "" && !hasUserCounter(grpcRows) {
		samples, err := queryXrayUserStats(bin, api)
		if err != nil {
			noteStatsFail("xray", err)
		} else if len(samples) > 0 || !grpcOK {
			deltas, next := mitaDeltas(c.xrayLast, samples)
			return deltas, next, emailsOf(samples), true
		}
	}
	if !grpcOK {
		return nil, nil, nil, false
	}
	cum := samplesFromStatRows(grpcRows)
	deltas, next := mitaDeltas(c.xrayLast, cum)
	return deltas, next, emailsOf(cum), true
}

var runXrayStatsQueryFn = runXrayStatsQuery

// queryXrayUserStats asks for user counters only. A timeout is not retried:
// the dokodemo API path used to stall every tick, and a second attempt would
// skip the next stats frame. A fast failure retries once without the pattern.
func queryXrayUserStats(bin, api string) ([]wsproto.Sample, error) {
	out, err := runXrayStatsQueryFn(bin, api, true)
	if samples, ok := parseXrayStatsOK(out); ok && err == nil {
		return samples, nil
	}
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	out, err = runXrayStatsQueryFn(bin, api, false)
	if err != nil {
		return nil, err
	}
	samples, ok := parseXrayStatsOK(out)
	if !ok {
		return nil, fmt.Errorf("statsquery 输出不是 JSON")
	}
	return samples, nil
}

func runXrayStatsQuery(bin, api string, pattern bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	args := []string{"api", "statsquery", "--server=" + api, "-timeout=2"}
	if pattern {
		args = append(args, "-pattern=user>>>")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

func (c *Cores) peekSingboxStats() ([]wsproto.Sample, map[string]bytePair, map[string]struct{}, bool) {
	c.mu.Lock()
	raw := append(json.RawMessage(nil), c.last["singbox"]...)
	c.mu.Unlock()
	addr, ok := singboxStatsListen(raw)
	if !ok {
		return nil, nil, nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := queryV2RayStatsFn(ctx, addr)
	if err != nil {
		noteStatsFail("sing-box", err)
		return nil, nil, nil, false
	}
	cum := samplesFromStatRows(rows)
	deltas, next := mitaDeltas(c.singboxStatsLast, cum)
	return deltas, next, emailsOf(cum), true
}

// singboxStatsListen reads experimental.v2ray_api.listen. Only a loopback
// address is queried. Clash is the fallback when this block is absent.
func singboxStatsListen(raw []byte) (string, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", false
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return "", false
	}
	exp, _ := top["experimental"].(map[string]any)
	v2, _ := exp["v2ray_api"].(map[string]any)
	if v2 == nil {
		return "", false
	}
	stats, _ := v2["stats"].(map[string]any)
	enabled, _ := stats["enabled"].(bool)
	if !enabled {
		return "", false
	}
	listen := strings.TrimSpace(strAny(v2["listen"]))
	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" || !isLoopback(host) {
		return "", false
	}
	return listen, true
}

var (
	statsFailMu sync.Mutex
	statsFailAt = map[string]time.Time{}
)

func noteStatsFail(core string, err error) {
	if err == nil {
		return
	}
	statsFailMu.Lock()
	defer statsFailMu.Unlock()
	now := time.Now()
	if now.Sub(statsFailAt[core]) < 30*time.Second {
		return
	}
	statsFailAt[core] = now
	log.Printf("agent: %s 用户统计采集失败: %v", core, err)
}

func (c *Cores) peekClash() ([]wsproto.Sample, map[string]clashSnap, map[string]struct{}, bool) {
	c.mu.Lock()
	api := c.singboxAPI
	emails := acctEmailMap(c.acctMarks)
	c.mu.Unlock()
	if api == "" {
		return nil, nil, nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+api+"/connections", nil)
	if err != nil {
		return nil, nil, nil, false
	}
	res, err := clashHTTP.Do(req)
	if err != nil {
		return nil, nil, nil, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, nil, nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, nil, nil, false
	}
	conns := fillClashUsers(parseClashConnections(raw), emails)
	samples, next := clashDeltas(c.clashLast, conns, time.Now())
	tracked := map[string]struct{}{}
	for _, conn := range conns {
		if conn.User != "" {
			tracked[conn.User] = struct{}{}
		}
	}
	return samples, next, tracked, true
}

func (c *Cores) peekMita() ([]wsproto.Sample, map[string]bytePair, map[string]struct{}, bool) {
	bin := lookBin("mita")
	if bin == "" || !mitaIsRunning(bin) {
		return nil, nil, nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "get", "metrics").Output()
	if err != nil {
		return nil, nil, nil, false
	}
	cum := parseMitaMetrics(out)
	samples, next := mitaDeltas(c.mitaLast, cum)
	return samples, next, emailsOf(cum), true
}

func parseXrayStats(raw []byte) []wsproto.Sample {
	samples, ok := parseXrayStatsOK(raw)
	if !ok {
		return nil
	}
	return samples
}

// parseXrayStatsOK accepts a log prefix and Stat/stats keys. Non-JSON is a
// failure so a garbage success cannot be committed as an empty snapshot.
func parseXrayStatsOK(raw []byte) ([]wsproto.Sample, bool) {
	body := extractJSONObject(raw)
	if body == nil {
		return nil, false
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, false
	}
	var statRaw json.RawMessage
	for _, key := range []string{"stat", "Stat", "stats"} {
		if v, ok := top[key]; ok && string(v) != "null" {
			statRaw = v
			break
		}
	}
	if statRaw == nil {
		return nil, true
	}
	var rows []struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
	}
	if err := json.Unmarshal(statRaw, &rows); err != nil {
		return nil, false
	}
	out := make([]statRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, statRow{Name: row.Name, Value: anyInt(row.Value)})
	}
	return samplesFromStatRows(out), true
}

func extractJSONObject(raw []byte) []byte {
	raw = bytes.TrimSpace(raw)
	start := bytes.IndexByte(raw, '{')
	end := bytes.LastIndexByte(raw, '}')
	if start < 0 || end < start {
		return nil
	}
	return raw[start : end+1]
}

func samplesFromStatRows(rows []statRow) []wsproto.Sample {
	m := map[string]*bytePair{}
	for _, row := range rows {
		parts := strings.Split(row.Name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
			continue
		}
		email := parts[1]
		if email == "" {
			continue
		}
		a := m[email]
		if a == nil {
			a = &bytePair{}
			m[email] = a
		}
		if parts[3] == "uplink" {
			a.up = row.Value
		} else if parts[3] == "downlink" {
			a.down = row.Value
		}
	}
	var out []wsproto.Sample
	for email, a := range m {
		if a.up == 0 && a.down == 0 {
			continue
		}
		out = append(out, wsproto.Sample{Email: email, Up: a.up, Down: a.down})
	}
	return out
}

func acctEmailMap(marks []wsproto.AcctMark) map[uint32]string {
	out := map[uint32]string{}
	for _, m := range marks {
		if m.Mark == 0 || m.Email == "" {
			continue
		}
		out[m.Mark] = m.Email
	}
	return out
}

func parseClashConnections(raw []byte) []clashConn {
	var payload struct {
		Connections []struct {
			ID       string   `json:"id"`
			Upload   any      `json:"upload"`
			Download any      `json:"download"`
			Start    string   `json:"start"`
			Chains   []string `json:"chains"`
			Metadata struct {
				User string `json:"user"`
			} `json:"metadata"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	var out []clashConn
	for _, row := range payload.Connections {
		user := strings.TrimSpace(row.Metadata.User)
		if row.ID == "" || (user == "" && len(row.Chains) == 0) {
			continue
		}
		out = append(out, clashConn{
			ID:     row.ID,
			User:   user,
			Up:     anyInt(row.Upload),
			Down:   anyInt(row.Download),
			Start:  parseClashStart(row.Start),
			Chains: row.Chains,
		})
	}
	return out
}

// fillClashUsers names a connection from its outbound tag when the clash
// JSON has no username. Official sing-box omits metadata.user. The tag we
// write is limit-<mark>, and that mark is already mapped to the client email.
func fillClashUsers(conns []clashConn, emails map[uint32]string) []clashConn {
	if len(emails) == 0 {
		return conns
	}
	for i := range conns {
		if conns[i].User != "" {
			continue
		}
		conns[i].User = emailFromChains(conns[i].Chains, emails)
	}
	return conns
}

func emailFromChains(chains []string, emails map[uint32]string) string {
	for _, tag := range chains {
		mark, ok := markFromLimitTag(tag)
		if !ok {
			continue
		}
		if email := emails[mark]; email != "" {
			return email
		}
	}
	return ""
}

func markFromLimitTag(tag string) (uint32, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(tag), "limit-")
	if !ok || rest == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(rest, 10, 32)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint32(n), true
}

func parseClashStart(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func clashDeltas(prev map[string]clashSnap, conns []clashConn, now time.Time) ([]wsproto.Sample, map[string]clashSnap) {
	next := map[string]clashSnap{}
	acc := map[string]*bytePair{}
	var order []string
	add := func(user string, up, down int64) {
		if user == "" || (up == 0 && down == 0) {
			return
		}
		a := acc[user]
		if a == nil {
			a = &bytePair{}
			acc[user] = a
			order = append(order, user)
		}
		a.up += up
		a.down += down
	}
	for _, conn := range conns {
		snap := clashSnap{up: conn.Up, down: conn.Down, user: conn.User}
		next[conn.ID] = snap
		old, ok := prev[conn.ID]
		if !ok {
			continue
		}
		up, down := conn.Up, conn.Down
		if up >= old.up {
			up -= old.up
		}
		if down >= old.down {
			down -= old.down
		}
		add(conn.User, up, down)
	}
	out := make([]wsproto.Sample, 0, len(order))
	for _, user := range order {
		a := acc[user]
		out = append(out, wsproto.Sample{Email: user, Up: a.up, Down: a.down})
	}
	return out, next
}

func parseMitaMetrics(raw []byte) []wsproto.Sample {
	var top any
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil
	}
	m := map[string]*bytePair{}
	var walk func(path []string, v any)
	walk = func(path []string, v any) {
		switch t := v.(type) {
		case map[string]any:
			if n, ok := metricNumber(t); ok {
				applyMitaMetric(m, path, n)
				return
			}
			for k, child := range t {
				walk(append(path, splitMetricKey(k)...), child)
			}
		case float64, int64, json.Number, string:
			applyMitaMetric(m, path, anyInt(t))
		}
	}
	walk(nil, top)
	var out []wsproto.Sample
	for user, a := range m {
		if a.up == 0 && a.down == 0 {
			continue
		}
		out = append(out, wsproto.Sample{Email: user, Up: a.up, Down: a.down})
	}
	return out
}

func splitMetricKey(k string) []string {
	k = strings.TrimSpace(k)
	if k == "" {
		return nil
	}
	if strings.Contains(k, " - ") {
		parts := strings.Split(k, " - ")
		var out []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		if len(out) > 1 {
			return out
		}
	}
	return []string{k}
}

func metricNumber(m map[string]any) (int64, bool) {
	for _, key := range []string{"Total", "total", "value", "Value", "count"} {
		if v, ok := m[key]; ok {
			switch v.(type) {
			case map[string]any:
				continue
			default:
				return anyInt(v), true
			}
		}
	}
	return 0, false
}

func applyMitaMetric(m map[string]*bytePair, path []string, n int64) {
	if n == 0 {
		return
	}
	user, dir := classifyMitaPath(path)
	if user == "" || dir == "" {
		return
	}
	a := m[user]
	if a == nil {
		a = &bytePair{}
		m[user] = a
	}
	if dir == "up" {
		a.up += n
	} else {
		a.down += n
	}
}

func classifyMitaPath(path []string) (user, dir string) {
	low := strings.ToLower(strings.Join(path, " "))
	isDown := strings.Contains(low, "downloadfrominternet") || strings.Contains(low, "downlink") || strings.Contains(low, "download")
	isUp := strings.Contains(low, "uploadtointernet") || strings.Contains(low, "uplink") || strings.Contains(low, "upload")
	if isDown {
		dir = "down"
	} else if isUp {
		dir = "up"
	} else {
		return "", ""
	}
	for i, p := range path {
		pl := strings.ToLower(p)
		if pl == "user" || pl == "users" || pl == "usermetric" || pl == "user-metric" {
			if i+1 < len(path) && !isMitaMetricName(path[i+1]) {
				return path[i+1], dir
			}
		}
	}
	return "", ""
}

func isMitaMetricName(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "upload") || strings.Contains(l, "download") || strings.Contains(l, "uplink") || strings.Contains(l, "downlink")
}

// flowDeltas bills only an increase. Conntrack sums shrink when a flow
// expires; the remaining bytes were already counted and must not be billed again.
func flowDeltas(prev map[string]bytePair, cum []wsproto.Sample) ([]wsproto.Sample, map[string]bytePair) {
	next := map[string]bytePair{}
	for k, v := range prev {
		next[k] = v
	}
	var out []wsproto.Sample
	for _, s := range cum {
		if s.Email == "" {
			continue
		}
		next[s.Email] = bytePair{up: s.Up, down: s.Down}
		old, ok := prev[s.Email]
		if !ok {
			continue
		}
		var up, down int64
		if s.Up >= old.up {
			up = s.Up - old.up
		}
		if s.Down >= old.down {
			down = s.Down - old.down
		}
		if up == 0 && down == 0 {
			continue
		}
		out = append(out, wsproto.Sample{Email: s.Email, Up: up, Down: down})
	}
	return out, next
}

func mitaDeltas(prev map[string]bytePair, cum []wsproto.Sample) ([]wsproto.Sample, map[string]bytePair) {
	next := map[string]bytePair{}
	for k, v := range prev {
		next[k] = v
	}
	var out []wsproto.Sample
	for _, s := range cum {
		if s.Email == "" {
			continue
		}
		next[s.Email] = bytePair{up: s.Up, down: s.Down}
		old, ok := prev[s.Email]
		if !ok {
			continue
		}
		up, down := s.Up, s.Down
		if up >= old.up {
			up -= old.up
		}
		if down >= old.down {
			down -= old.down
		}
		if up == 0 && down == 0 {
			continue
		}
		out = append(out, wsproto.Sample{Email: s.Email, Up: up, Down: down})
	}
	return out, next
}

func mergeSamples(in []wsproto.Sample) []wsproto.Sample {
	return maxMergeSamples(in)
}

// maxMergeSamples sums samples inside one source, then keeps the larger value
// of each direction across sources. Socket bytes and a core counter describe
// the same transfer. Adding them bills it twice. Keeping only the core counter
// drops the bytes after Xray splices past the userspace stat writer.
func maxMergeSamples(groups ...[]wsproto.Sample) []wsproto.Sample {
	summed := make([]map[string]bytePair, 0, len(groups))
	var order []string
	seen := map[string]struct{}{}
	for _, group := range groups {
		m := map[string]bytePair{}
		for _, s := range group {
			if s.Email == "" || (s.Up == 0 && s.Down == 0) {
				continue
			}
			a := m[s.Email]
			a.up += s.Up
			a.down += s.Down
			m[s.Email] = a
			if _, ok := seen[s.Email]; !ok {
				seen[s.Email] = struct{}{}
				order = append(order, s.Email)
			}
		}
		summed = append(summed, m)
	}
	out := make([]wsproto.Sample, 0, len(order))
	for _, email := range order {
		var up, down int64
		for _, m := range summed {
			a := m[email]
			if a.up > up {
				up = a.up
			}
			if a.down > down {
				down = a.down
			}
		}
		if up == 0 && down == 0 {
			continue
		}
		out = append(out, wsproto.Sample{Email: email, Up: up, Down: down})
	}
	return out
}
