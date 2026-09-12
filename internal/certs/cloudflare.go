package certs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	Success bool            `json:"success"`
	Errors  []cfErr         `json:"errors"`
	Result  json.RawMessage `json:"result"`
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
