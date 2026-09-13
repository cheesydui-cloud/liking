package corecfg

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"liking/internal/db"
)

type SocksTarget struct {
	Host string
	Port int
	User string
	Pass string
}

func ParseSocksURI(raw string) (*SocksTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("SK5 地址为空")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return nil, fmt.Errorf("SK5 需要 socks5:// 地址")
	}
	switch strings.ToLower(u.Scheme) {
	case "socks", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("SK5 需要 socks5:// 地址")
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("SK5 缺少主机")
	}
	port := 1080
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("SK5 端口无效")
		}
		port = n
	}
	t := &SocksTarget{Host: host, Port: port}
	if u.User != nil {
		t.User = u.User.Username()
		t.Pass, _ = u.User.Password()
	}
	return t, nil
}

func FormatSocksURI(t *SocksTarget) string {
	if t == nil || strings.TrimSpace(t.Host) == "" {
		return ""
	}
	port := t.Port
	if port < 1 || port > 65535 {
		port = 1080
	}
	u := &url.URL{
		Scheme: "socks5",
		Host:   net.JoinHostPort(t.Host, strconv.Itoa(port)),
	}
	if t.User != "" || t.Pass != "" {
		u.User = url.UserPassword(t.User, t.Pass)
	}
	return u.String()
}

func socksExit(entry *db.Inbound) (*SocksTarget, error) {
	if entry == nil {
		return nil, nil
	}
	uri := strings.TrimSpace(entry.ExitURI)
	if uri == "" {
		return nil, nil
	}
	return ParseSocksURI(uri)
}
