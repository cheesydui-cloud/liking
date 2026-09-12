package certs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const defaultCFAPI = "https://api.cloudflare.com/client/v4"

type Cloudflare struct {
	Token string
	HTTP  *http.Client
	API   string
	zones map[string]string
}

func (c *Cloudflare) api() string {
	if c.API != "" {
		return strings.TrimRight(c.API, "/")
	}
	return defaultCFAPI
}

func (c *Cloudflare) httpc() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

type cfResp struct {
	Success    bool            `json:"success"`
	Errors     []cfErr         `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo cfResultInfo    `json:"result_info"`
}

type cfResultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	TotalPages int `json:"total_pages"`
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
}

type cfErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfZone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type cfRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

func (c *Cloudflare) do(ctx context.Context, method, path string, body any) (*cfResp, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.api()+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.httpc().Do(req)
	if err != nil {
		return nil, fmt.Errorf("Cloudflare 请求失败：%w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var wrap cfResp
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("Cloudflare 返回无法解析（HTTP %d）", res.StatusCode)
	}
	if res.StatusCode == 403 || hasCFCode(wrap.Errors, 9103, 10000) {
		return nil, fmt.Errorf("Cloudflare Token 无效或权限不足（需要 Zone 读取和 DNS 编辑）")
	}
	if !wrap.Success {
		msg := cfErrText(wrap.Errors)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", res.StatusCode)
		}
		return nil, fmt.Errorf("Cloudflare：%s", msg)
	}
	return &wrap, nil
}

func hasCFCode(errs []cfErr, codes ...int) bool {
	for _, e := range errs {
		for _, c := range codes {
			if e.Code == c {
				return true
			}
		}
	}
	return false
}

func cfErrText(errs []cfErr) string {
	if len(errs) == 0 {
		return ""
	}
	return errs[0].Message
}

func (c *Cloudflare) zoneID(ctx context.Context, fqdn string) (string, error) {
	if c.zones == nil {
		c.zones = map[string]string{}
	}
	for _, cand := range ZoneCandidates(fqdn) {
		if id, ok := c.zones[cand]; ok {
			return id, nil
		}
		wrap, err := c.do(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(cand)+"&status=active", nil)
		if err != nil {
			return "", err
		}
		var zones []cfZone
		if err := json.Unmarshal(wrap.Result, &zones); err != nil {
			return "", fmt.Errorf("Cloudflare zone 无法解析")
		}
		for _, z := range zones {
			if strings.EqualFold(z.Name, cand) && z.ID != "" {
				c.zones[cand] = z.ID
				return z.ID, nil
			}
		}
	}
	return "", fmt.Errorf("Cloudflare 找不到域名 %s 所在的 Zone（Token 是否绑对了这个域？）", fqdn)
}

func (c *Cloudflare) Present(ctx context.Context, name, content string) error {
	zid, err := c.zoneID(ctx, name)
	if err != nil {
		return err
	}
	recs, err := c.listTXT(ctx, zid, name)
	if err != nil {
		return err
	}
	for _, r := range recs {
		if r.Content == content {
			return nil
		}
	}
	_, err = c.do(ctx, http.MethodPost, "/zones/"+zid+"/dns_records", map[string]any{
		"type":    "TXT",
		"name":    name,
		"content": content,
		"ttl":     60,
	})
	return err
}

func (c *Cloudflare) DeleteTXT(ctx context.Context, name, content string) error {
	zid, err := c.zoneID(ctx, name)
	if err != nil {
		return err
	}
	recs, err := c.listTXT(ctx, zid, name)
	if err != nil {
		return err
	}
	var last error
	for _, r := range recs {
		if r.Content != content {
			continue
		}
		if _, err := c.do(ctx, http.MethodDelete, "/zones/"+zid+"/dns_records/"+r.ID, nil); err != nil {
			last = err
		}
	}
	return last
}

func (c *Cloudflare) listTXT(ctx context.Context, zoneID, name string) ([]cfRecord, error) {
	wrap, err := c.do(ctx, http.MethodGet, "/zones/"+zoneID+"/dns_records?type=TXT&name="+url.QueryEscape(name), nil)
	if err != nil {
		return nil, err
	}
	var recs []cfRecord
	if err := json.Unmarshal(wrap.Result, &recs); err != nil {
		return nil, fmt.Errorf("Cloudflare DNS 记录无法解析")
	}
	want := strings.TrimSuffix(strings.ToLower(name), ".")
	var out []cfRecord
	for _, r := range recs {
		got := strings.TrimSuffix(strings.ToLower(r.Name), ".")
		if got == want || strings.HasPrefix(got, want+".") {
			out = append(out, r)
		}
	}
	return out, nil
}

const cfPerPage = 50
const cfMaxPages = 40

// HostName is a Cloudflare-hosted name that can be used as a server public_host.
type HostName struct {
	Name    string `json:"name"`
	Zone    string `json:"zone"`
	Type    string `json:"type,omitempty"`
	Content string `json:"content,omitempty"`
	Proxied bool   `json:"proxied"`
	Matched bool   `json:"matched"`
}

func (c *Cloudflare) forEachPage(ctx context.Context, path string, fn func(json.RawMessage) (int, error)) error {
	page := 1
	for {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		wrap, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s%sper_page=%d&page=%d", path, sep, cfPerPage, page), nil)
		if err != nil {
			return err
		}
		n, err := fn(wrap.Result)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		if wrap.ResultInfo.TotalPages > 0 && page >= wrap.ResultInfo.TotalPages {
			return nil
		}
		if wrap.ResultInfo.TotalPages == 0 && n < cfPerPage {
			return nil
		}
		page++
		if page > cfMaxPages {
			return nil
		}
	}
}

func (c *Cloudflare) listZones(ctx context.Context) ([]cfZone, error) {
	var out []cfZone
	err := c.forEachPage(ctx, "/zones?status=active", func(raw json.RawMessage) (int, error) {
		var zones []cfZone
		if len(raw) == 0 || string(raw) == "null" {
			return 0, nil
		}
		if err := json.Unmarshal(raw, &zones); err != nil {
			return 0, fmt.Errorf("Cloudflare zone 无法解析")
		}
		out = append(out, zones...)
		return len(zones), nil
	})
	return out, err
}

func (c *Cloudflare) listRecords(ctx context.Context, zoneID, recType string) ([]cfRecord, error) {
	var out []cfRecord
	path := "/zones/" + zoneID + "/dns_records?type=" + url.QueryEscape(recType)
	err := c.forEachPage(ctx, path, func(raw json.RawMessage) (int, error) {
		var recs []cfRecord
		if len(raw) == 0 || string(raw) == "null" {
			return 0, nil
		}
		if err := json.Unmarshal(raw, &recs); err != nil {
			return 0, fmt.Errorf("Cloudflare DNS 记录无法解析")
		}
		out = append(out, recs...)
		return len(recs), nil
	})
	return out, err
}

func skipHostName(name string) bool {
	n := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if n == "" {
		return true
	}
	for _, label := range strings.Split(n, ".") {
		if label == "" || label == "*" || strings.HasPrefix(label, "_") {
			return true
		}
	}
	return false
}

func collectMatchIPs(vals ...string) []net.IP {
	var out []net.IP
	seen := map[string]bool{}
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		host := v
		if h, _, err := net.SplitHostPort(v); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		ip := net.ParseIP(host)
		if ip == nil {
			continue
		}
		key := ip.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ip)
	}
	return out
}

func ipMatches(content string, ips []net.IP) bool {
	got := net.ParseIP(strings.TrimSpace(strings.Trim(content, "[]")))
	if got == nil {
		return false
	}
	for _, ip := range ips {
		if ip.Equal(got) {
			return true
		}
	}
	return false
}

type hostAcc struct {
	HostName
	types map[string]bool
	ips   []string
}

// ListHostNames returns A/AAAA names (and zone apex) from the token's zones.
// Names whose content equals connectIP or an IP publicHost are marked Matched.
func (c *Cloudflare) ListHostNames(ctx context.Context, connectIP, publicHost string) ([]HostName, error) {
	zones, err := c.listZones(ctx)
	if err != nil {
		return nil, err
	}
	ips := collectMatchIPs(connectIP, publicHost)
	byName := map[string]*hostAcc{}
	add := func(name, zone, recType, content string, proxied bool) {
		if skipHostName(name) {
			return
		}
		name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
		zone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zone), "."))
		a := byName[name]
		if a == nil {
			a = &hostAcc{HostName: HostName{Name: name, Zone: zone}, types: map[string]bool{}}
			byName[name] = a
		}
		if recType != "" {
			a.types[recType] = true
		}
		content = strings.TrimSpace(content)
		if content != "" {
			dup := false
			for _, x := range a.ips {
				if strings.EqualFold(x, content) {
					dup = true
					break
				}
			}
			if !dup {
				a.ips = append(a.ips, content)
			}
			if ipMatches(content, ips) {
				a.Matched = true
			}
		}
		if proxied {
			a.Proxied = true
		}
		if a.Zone == "" {
			a.Zone = zone
		}
	}
	for _, z := range zones {
		if z.Name == "" || z.ID == "" {
			continue
		}
		add(z.Name, z.Name, "", "", false)
		for _, typ := range []string{"A", "AAAA"} {
			recs, err := c.listRecords(ctx, z.ID, typ)
			if err != nil {
				return nil, err
			}
			for _, rec := range recs {
				t := rec.Type
				if t == "" {
					t = typ
				}
				add(rec.Name, z.Name, t, rec.Content, rec.Proxied)
			}
		}
	}
	out := make([]HostName, 0, len(byName))
	for _, a := range byName {
		var types []string
		if a.types["A"] {
			types = append(types, "A")
		}
		if a.types["AAAA"] {
			types = append(types, "AAAA")
		}
		a.Type = strings.Join(types, ",")
		a.Content = strings.Join(a.ips, ", ")
		out = append(out, a.HostName)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Matched != out[j].Matched {
			return out[i].Matched
		}
		if out[i].Proxied != out[j].Proxied {
			return !out[i].Proxied
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
