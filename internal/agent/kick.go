package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type xrayKickTarget struct {
	Tag  string
	User json.RawMessage
}

func extractXrayKickTargets(raw json.RawMessage, emails map[string]struct{}) []xrayKickTarget {
	if len(emails) == 0 || !hasCfg(raw) {
		return nil
	}
	var cfg struct {
		Inbounds []struct {
			Tag      string `json:"tag"`
			Settings struct {
				Clients []json.RawMessage `json:"clients"`
			} `json:"settings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	var out []xrayKickTarget
	for _, in := range cfg.Inbounds {
		tag := strings.TrimSpace(in.Tag)
		if tag == "" || tag == "api" {
			continue
		}
		for _, rawUser := range in.Settings.Clients {
			var u struct {
				Email string `json:"email"`
			}
			if err := json.Unmarshal(rawUser, &u); err != nil {
				continue
			}
			email := strings.TrimSpace(u.Email)
			if email == "" {
				continue
			}
			if _, ok := emails[email]; !ok {
				continue
			}
			out = append(out, xrayKickTarget{Tag: tag, User: append(json.RawMessage(nil), rawUser...)})
		}
	}
	return out
}

func clashIDsForEmails(conns []clashConn, emails map[string]struct{}) []string {
	if len(emails) == 0 {
		return nil
	}
	var ids []string
	for _, c := range conns {
		if _, ok := emails[strings.TrimSpace(c.User)]; !ok {
			continue
		}
		if c.ID == "" {
			continue
		}
		ids = append(ids, c.ID)
	}
	return ids
}

var runXrayAPI = func(bin, api string, args []string, stdin []byte) ([]byte, error) {
	if bin == "" || api == "" {
		return nil, fmt.Errorf("xray api 不可用")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmdArgs := append([]string{"api", args[0], "--server=" + api}, args[1:]...)
	cmd := exec.CommandContext(ctx, bin, cmdArgs...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

func (c *Cores) Kick(emails []string) (int, error) {
	want := map[string]struct{}{}
	for _, e := range emails {
		e = strings.TrimSpace(e)
		if e == "" || strings.HasPrefix(e, "relay.") {
			continue
		}
		want[e] = struct{}{}
	}
	if len(want) == 0 {
		return 0, nil
	}

	c.mu.Lock()
	xrayBin, xrayAPI := c.xrayBin, c.xrayAPI
	xrayLast := append(json.RawMessage(nil), c.last["xray"]...)
	singAPI := c.singboxAPI
	c.mu.Unlock()

	closed := 0
	var errs []string
	targets := extractXrayKickTargets(xrayLast, want)
	aduFailed := false
	for _, t := range targets {
		email := xrayUserEmail(t.User)
		out, err := runXrayAPI(xrayBin, xrayAPI, []string{"rmu", "-tag=" + t.Tag, "-email=" + email}, nil)
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			if !xrayUserMissing(msg) {
				errs = append(errs, "xray rmu: "+msg)
			}
			continue
		}
		out, err = runXrayAPI(xrayBin, xrayAPI, []string{"adu", "-tag=" + t.Tag}, t.User)
		if err != nil {
			aduFailed = true
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			errs = append(errs, "xray adu: "+msg)
			continue
		}
		closed++
	}
	if aduFailed {
		if err := c.restartXrayFromDisk(); err != nil {
			errs = append(errs, "xray 恢复: "+err.Error())
		}
	}

	n, err := c.kickSingbox(singAPI, want)
	closed += n
	if err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 && closed == 0 {
		return 0, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return closed, nil
}

func xrayUserEmail(raw json.RawMessage) string {
	var u struct {
		Email string `json:"email"`
	}
	_ = json.Unmarshal(raw, &u)
	return strings.TrimSpace(u.Email)
}

func xrayUserMissing(msg string) bool {
	l := strings.ToLower(msg)
	return strings.Contains(l, "not found") || strings.Contains(l, "doesn't exist") || strings.Contains(l, "does not exist") || strings.Contains(l, "no user")
}

func (c *Cores) restartXrayFromDisk() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	bin := c.xrayBin
	if bin == "" {
		bin = lookBin("xray")
	}
	if bin == "" {
		return fmt.Errorf("xray 未安装")
	}
	path := filepath.Join(c.dir, "xray.json")
	c.stopLocked("xray")
	return c.startLocked("xray", bin, []string{"run", "-c", path})
}

func (c *Cores) kickSingbox(api string, emails map[string]struct{}) (int, error) {
	api = strings.TrimSpace(api)
	if api == "" || len(emails) == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+api+"/connections", nil)
	if err != nil {
		return 0, err
	}
	res, err := clashHTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("sing-box 连接列表: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("sing-box 连接列表 HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return 0, err
	}
	ids := clashIDsForEmails(parseClashConnections(raw), emails)
	closed := 0
	for _, id := range ids {
		dctx, dcancel := context.WithTimeout(context.Background(), 2*time.Second)
		dreq, err := http.NewRequestWithContext(dctx, http.MethodDelete, "http://"+api+"/connections/"+id, nil)
		if err != nil {
			dcancel()
			continue
		}
		dres, err := clashHTTP.Do(dreq)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(dres.Body, 1<<20))
			_ = dres.Body.Close()
			if dres.StatusCode < 300 {
				closed++
			}
		}
		dcancel()
	}
	return closed, nil
}
