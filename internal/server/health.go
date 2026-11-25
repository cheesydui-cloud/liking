package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// CheckHealthURL GETs a loopback /healthz and returns when the panel answers.
// The flag is the container healthcheck. It refuses any other host so the
// binary cannot be pointed at an internal address.
func CheckHealthURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("健康检查地址无效")
	}
	if u.Path != "/healthz" {
		return fmt.Errorf("健康检查地址无效")
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
	default:
		return fmt.Errorf("健康检查只能访问本机")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("健康检查地址无效")
	}
	client := &http.Client{
		Timeout: 3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("健康检查不能跳转")
		},
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("健康检查失败")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("健康检查失败")
	}
	return nil
}
