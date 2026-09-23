package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"liking/internal/wsproto"
)

var clashHTTP = &http.Client{Timeout: 2 * time.Second}

type bytePair struct{ up, down int64 }

type clashSnap struct {
	up, down int64
	user     string
}

type clashConn struct {
	ID    string
	User  string
	Up    int64
	Down  int64
	Start time.Time
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
	var mitaNext map[string]bytePair
	xrayOK, clashOK, mitaOK := false, false, false
	if samples, next, ok := c.peekXray(); ok {
		all = append(all, samples...)
		xrayNext, xrayOK = next, true
	}
	if samples, next, ok := c.peekClash(); ok {
		all = append(all, samples...)
		clashNext, clashOK = next, true
	}
	if samples, next, ok := c.peekMita(); ok {
		all = append(all, samples...)
		mitaNext, mitaOK = next, true
	}
	merged := mergeSamples(all)
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
			if clashOK {
				c.clashLast = clashNext
			}
			if mitaOK {
				c.mitaLast = mitaNext
			}
		}
		c.collectMu.Unlock()
	}
	return merged, finish
}

func (c *Cores) peekXray() ([]wsproto.Sample, map[string]bytePair, bool) {
	c.mu.Lock()
	bin, api := c.xrayBin, c.xrayAPI
	c.mu.Unlock()
	if bin == "" || api == "" {
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "api", "statsquery", "--server="+api)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, false
	}
	samples, next := mitaDeltas(c.xrayLast, parseXrayStats(out))
	return samples, next, true
}

func (c *Cores) peekClash() ([]wsproto.Sample, map[string]clashSnap, bool) {
	c.mu.Lock()
	api := c.singboxAPI
	c.mu.Unlock()
	if api == "" {
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+api+"/connections", nil)
	if err != nil {
		return nil, nil, false
	}
	res, err := clashHTTP.Do(req)
	if err != nil {
		return nil, nil, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, nil, false
	}
	samples, next := clashDeltas(c.clashLast, parseClashConnections(raw), time.Now())
	return samples, next, true
}

func (c *Cores) peekMita() ([]wsproto.Sample, map[string]bytePair, bool) {
	bin := lookBin("mita")
	if bin == "" || !mitaIsRunning(bin) {
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "get", "metrics").Output()
	if err != nil {
		return nil, nil, false
	}
	samples, next := mitaDeltas(c.mitaLast, parseMitaMetrics(out))
	return samples, next, true
}

func parseXrayStats(raw []byte) []wsproto.Sample {
	var st xrayStats
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil
	}
	m := map[string]*bytePair{}
	for _, row := range st.Stat {
		parts := strings.Split(row.Name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
			continue
		}
		email := parts[1]
		a := m[email]
		if a == nil {
			a = &bytePair{}
			m[email] = a
		}
		v := anyInt(row.Value)
		if parts[3] == "uplink" {
			a.up = v
		} else if parts[3] == "downlink" {
			a.down = v
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

func parseClashConnections(raw []byte) []clashConn {
	var payload struct {
		Connections []struct {
			ID       string `json:"id"`
			Upload   any    `json:"upload"`
			Download any    `json:"download"`
			Start    string `json:"start"`
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
		if row.ID == "" || user == "" {
			continue
		}
		out = append(out, clashConn{
			ID:    row.ID,
			User:  user,
			Up:    anyInt(row.Upload),
			Down:  anyInt(row.Download),
			Start: parseClashStart(row.Start),
		})
	}
	return out
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
	m := map[string]*bytePair{}
	var order []string
	for _, s := range in {
		if s.Email == "" || (s.Up == 0 && s.Down == 0) {
			continue
		}
		a := m[s.Email]
		if a == nil {
			a = &bytePair{}
			m[s.Email] = a
			order = append(order, s.Email)
		}
		a.up += s.Up
		a.down += s.Down
	}
	out := make([]wsproto.Sample, 0, len(order))
	for _, email := range order {
		a := m[email]
		out = append(out, wsproto.Sample{Email: email, Up: a.up, Down: a.down})
	}
	return out
}
