package corecfg

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"liking/internal/db"
)

// ShareTarget is a vless / ss / trojan / socks5 share link used as a chain exit.
type ShareTarget struct {
	Scheme        string
	Host          string
	Port          int
	Name          string
	UUID          string
	User          string
	Password      string
	Method        string
	Flow          string
	Encryption    string
	Security      string
	Network       string
	SNI           string
	Fingerprint   string
	ALPN          []string
	PublicKey     string
	ShortID       string
	SpiderX       string
	Path          string
	HostHeader    string
	ServiceName   string
	Mode          string
	AllowInsecure bool
	Raw           string
}

func firstURILine(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "\"'")
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return line
	}
	return raw
}

// ParseShareURI accepts vless://, ss://, trojan://, and socks5://.
func ParseShareURI(raw string) (*ShareTarget, error) {
	raw = firstURILine(raw)
	if raw == "" {
		return nil, fmt.Errorf("链接为空")
	}
	u, err := url.Parse(raw)
	scheme := ""
	if err == nil && u != nil {
		scheme = strings.ToLower(u.Scheme)
	} else {
		if i := strings.Index(raw, "://"); i > 0 {
			scheme = strings.ToLower(raw[:i])
		}
	}
	switch scheme {
	case "socks", "socks5", "socks5h":
		t, err := ParseSocksURI(raw)
		if err != nil {
			return nil, err
		}
		name := ""
		if u != nil {
			name = fragmentName(u)
		}
		return socksToShare(t, name, raw), nil
	case "vless":
		if err != nil {
			return nil, fmt.Errorf("VLESS 链接无效")
		}
		return parseVLESS(u, raw)
	case "trojan":
		if err != nil {
			return nil, fmt.Errorf("Trojan 链接无效")
		}
		return parseTrojan(u, raw)
	case "ss":
		return parseSS(raw)
	case "":
		return nil, fmt.Errorf("无法识别链接，请粘贴 vless://、ss://、trojan:// 或 socks5://")
	default:
		return nil, fmt.Errorf("暂不支持 %s 链接，请用 vless / ss / trojan / socks5", scheme)
	}
}

func socksToShare(t *SocksTarget, name, raw string) *ShareTarget {
	if t == nil {
		return nil
	}
	return &ShareTarget{
		Scheme:   "socks5",
		Host:     t.Host,
		Port:     t.Port,
		User:     t.User,
		Password: t.Pass,
		Name:     name,
		Network:  "tcp",
		Security: "none",
		Raw:      raw,
	}
}

func (t *ShareTarget) SocksTarget() *SocksTarget {
	if t == nil {
		return nil
	}
	switch t.Scheme {
	case "socks", "socks5", "socks5h":
		return &SocksTarget{Host: t.Host, Port: t.Port, User: t.User, Pass: t.Password}
	default:
		return nil
	}
}

func (t *ShareTarget) ProtoShort() string {
	if t == nil {
		return ""
	}
	switch t.Scheme {
	case "vless":
		return "VLESS"
	case "ss":
		return "SS"
	case "trojan":
		return "Trojan"
	case "socks", "socks5", "socks5h":
		return "SK5"
	default:
		return strings.ToUpper(t.Scheme)
	}
}

func (t *ShareTarget) Label() string {
	if t == nil {
		return ""
	}
	proto := t.ProtoShort()
	hp := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	if strings.TrimSpace(t.Name) != "" {
		return strings.TrimSpace(t.Name) + " · " + proto
	}
	return proto + " " + hp
}

func (t *ShareTarget) Preview() map[string]any {
	if t == nil {
		return nil
	}
	return map[string]any{
		"scheme":   t.Scheme,
		"host":     t.Host,
		"port":     t.Port,
		"name":     t.Name,
		"security": t.Security,
		"network":  t.Network,
		"sni":      t.SNI,
		"label":    t.Label(),
	}
}

func shareExit(entry *db.Inbound) (*ShareTarget, error) {
	if entry == nil {
		return nil, nil
	}
	uri := strings.TrimSpace(entry.ExitURI)
	if uri == "" {
		return nil, nil
	}
	return ParseShareURI(uri)
}

func fragmentName(u *url.URL) string {
	if u == nil {
		return ""
	}
	s := strings.TrimSpace(u.Fragment)
	if s == "" {
		return ""
	}
	if d, err := url.QueryUnescape(s); err == nil {
		return strings.TrimSpace(d)
	}
	return s
}

func queryVal(q url.Values, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(q.Get(k)); v != "" {
			return v
		}
		for key, vs := range q {
			if strings.EqualFold(key, k) && len(vs) > 0 {
				if v := strings.TrimSpace(vs[0]); v != "" {
					return v
				}
			}
		}
	}
	return ""
}

func parseNetwork(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "tcp", "raw":
		return "tcp", nil
	case "ws", "websocket":
		return "ws", nil
	case "grpc":
		return "grpc", nil
	case "httpupgrade", "http_upgrade":
		return "httpupgrade", nil
	case "xhttp", "splithttp":
		return "xhttp", nil
	case "h2", "http":
		return "http", nil
	default:
		return "", fmt.Errorf("暂不支持这种传输: %s", s)
	}
}

func parseSecurity(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "tls", "xtls":
		return "tls"
	case "reality":
		return "reality"
	case "none", "zero":
		return "none"
	default:
		return s
	}
}

func parseHostPort(u *url.URL, def int) (string, int, error) {
	if u == nil {
		return "", 0, fmt.Errorf("链接缺少主机")
	}
	host := u.Hostname()
	if host == "" {
		return "", 0, fmt.Errorf("链接缺少主机")
	}
	port := def
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", 0, fmt.Errorf("端口无效")
		}
		port = n
	}
	if port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("端口无效")
	}
	return host, port, nil
}

func parseVLESS(u *url.URL, raw string) (*ShareTarget, error) {
	host, port, err := parseHostPort(u, 443)
	if err != nil {
		return nil, err
	}
	uuid := ""
	if u.User != nil {
		uuid = strings.TrimSpace(u.User.Username())
	}
	if uuid == "" {
		return nil, fmt.Errorf("VLESS 缺少 UUID")
	}
	q := u.Query()
	netw, err := parseNetwork(queryVal(q, "type", "network"))
	if err != nil {
		return nil, err
	}
	sec := parseSecurity(queryVal(q, "security"))
	pbk := queryVal(q, "pbk", "publicKey", "publickey")
	if sec == "" && pbk != "" {
		sec = "reality"
	}
	if sec == "" {
		sec = "none"
	}
	if sec == "reality" && pbk == "" {
		return nil, fmt.Errorf("VLESS REALITY 缺少公钥")
	}
	alpn := splitCSV(queryVal(q, "alpn"))
	insecure := queryVal(q, "allowInsecure", "allowinsecure", "insecure")
	t := &ShareTarget{
		Scheme:        "vless",
		Host:          host,
		Port:          port,
		Name:          fragmentName(u),
		UUID:          uuid,
		Flow:          queryVal(q, "flow"),
		Encryption:    nz(queryVal(q, "encryption"), "none"),
		Security:      sec,
		Network:       netw,
		SNI:           queryVal(q, "sni", "peer"),
		Fingerprint:   queryVal(q, "fp", "fingerprint"),
		ALPN:          alpn,
		PublicKey:     pbk,
		ShortID:       queryVal(q, "sid", "shortId", "shortid"),
		SpiderX:       queryVal(q, "spx", "spiderX", "spiderx"),
		Path:          queryVal(q, "path"),
		HostHeader:    queryVal(q, "host", "authority"),
		ServiceName:   queryVal(q, "serviceName", "servicename", "service"),
		Mode:          queryVal(q, "mode"),
		AllowInsecure: insecure == "1" || strings.EqualFold(insecure, "true"),
		Raw:           raw,
	}
	return t, nil
}

func parseTrojan(u *url.URL, raw string) (*ShareTarget, error) {
	host, port, err := parseHostPort(u, 443)
	if err != nil {
		return nil, err
	}
	pass := ""
	if u.User != nil {
		pass = u.User.Username()
		if p, ok := u.User.Password(); ok && p != "" {
			pass = pass + ":" + p
		}
	}
	if strings.TrimSpace(pass) == "" {
		return nil, fmt.Errorf("Trojan 缺少密码")
	}
	q := u.Query()
	netw, err := parseNetwork(queryVal(q, "type", "network"))
	if err != nil {
		return nil, err
	}
	sec := parseSecurity(queryVal(q, "security"))
	if sec == "" {
		sec = "tls"
	}
	insecure := queryVal(q, "allowInsecure", "allowinsecure", "insecure")
	return &ShareTarget{
		Scheme:        "trojan",
		Host:          host,
		Port:          port,
		Name:          fragmentName(u),
		Password:      pass,
		Security:      sec,
		Network:       netw,
		SNI:           queryVal(q, "sni", "peer"),
		Fingerprint:   queryVal(q, "fp", "fingerprint"),
		ALPN:          splitCSV(queryVal(q, "alpn")),
		Path:          queryVal(q, "path"),
		HostHeader:    queryVal(q, "host", "authority"),
		ServiceName:   queryVal(q, "serviceName", "servicename", "service"),
		AllowInsecure: insecure == "1" || strings.EqualFold(insecure, "true"),
		Raw:           raw,
	}, nil
}

func parseSS(raw string) (*ShareTarget, error) {
	raw = strings.TrimSpace(raw)
	name := ""
	rest := raw
	if i := strings.Index(strings.ToLower(rest), "ss://"); i >= 0 {
		rest = rest[i+5:]
	}
	if i := strings.Index(rest, "#"); i >= 0 {
		name, _ = url.QueryUnescape(rest[i+1:])
		rest = rest[:i]
	}
	if i := strings.Index(rest, "?"); i >= 0 {
		q, err := url.ParseQuery(strings.TrimPrefix(rest[i:], "?"))
		if err == nil && strings.TrimSpace(q.Get("plugin")) != "" {
			return nil, fmt.Errorf("暂不支持带插件的 SS 链接")
		}
		rest = rest[:i]
	}
	rest = strings.TrimSpace(rest)
	// SIP002 has userinfo@host; legacy is base64(method:pass@host:port) and has no '@'.
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		host, port, err := splitHostPortDefault(strings.TrimSuffix(rest[at+1:], "/"), 8388)
		if err != nil {
			return nil, err
		}
		method, pass, err := splitSSUserinfo(rest[:at])
		if err != nil {
			return nil, err
		}
		return &ShareTarget{
			Scheme:   "ss",
			Host:     host,
			Port:     port,
			Name:     name,
			Method:   method,
			Password: pass,
			Network:  "tcp",
			Security: "none",
			Raw:      raw,
		}, nil
	}
	return parseSSLegacy(rest, name, raw)
}

func parseSSLegacy(payload, name, raw string) (*ShareTarget, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, fmt.Errorf("SS 链接无效")
	}
	b, err := decodeB64(payload)
	if err != nil {
		return nil, fmt.Errorf("SS 链接无法解码")
	}
	s := string(b)
	at := strings.LastIndex(s, "@")
	if at < 0 {
		return nil, fmt.Errorf("SS 链接缺少主机")
	}
	method, pass, err := splitSSUserinfo(s[:at])
	if err != nil {
		return nil, err
	}
	host, port, err := splitHostPortDefault(s[at+1:], 8388)
	if err != nil {
		return nil, err
	}
	return &ShareTarget{
		Scheme:   "ss",
		Host:     host,
		Port:     port,
		Name:     name,
		Method:   method,
		Password: pass,
		Network:  "tcp",
		Security: "none",
		Raw:      raw,
	}, nil
}

func splitSSUserinfo(userinfo string) (string, string, error) {
	userinfo = strings.TrimSpace(userinfo)
	if userinfo == "" {
		return "", "", fmt.Errorf("SS 缺少密码")
	}
	if method, pass, ok := cutMethodPass(userinfo); ok {
		return method, pass, nil
	}
	b, err := decodeB64(userinfo)
	if err != nil {
		return "", "", fmt.Errorf("SS 用户信息无效")
	}
	method, pass, ok := cutMethodPass(string(b))
	if !ok {
		return "", "", fmt.Errorf("SS 用户信息无效")
	}
	return method, pass, nil
}

func cutMethodPass(s string) (string, string, bool) {
	method, pass, ok := strings.Cut(s, ":")
	method = strings.TrimSpace(method)
	if !ok || method == "" || pass == "" {
		return "", "", false
	}
	return method, pass, true
}

func splitHostPortDefault(hp string, def int) (string, int, error) {
	hp = strings.TrimSpace(hp)
	if hp == "" {
		return "", 0, fmt.Errorf("链接缺少主机")
	}
	host, portStr, err := net.SplitHostPort(hp)
	if err != nil {
		if strings.Count(hp, ":") == 0 {
			if def < 1 {
				return "", 0, fmt.Errorf("端口无效")
			}
			return hp, def, nil
		}
		return "", 0, fmt.Errorf("SS 主机无效")
	}
	n, err := strconv.Atoi(portStr)
	if err != nil || n < 1 || n > 65535 {
		return "", 0, fmt.Errorf("端口无效")
	}
	return host, n, nil
}

func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	if s == "" {
		return nil, fmt.Errorf("empty")
	}
	trimmed := strings.TrimRight(s, "=")
	if b, err := base64.RawURLEncoding.DecodeString(trimmed); err == nil && len(b) > 0 {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(padB64(s)); err == nil && len(b) > 0 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(trimmed); err == nil && len(b) > 0 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(padB64(s)); err == nil && len(b) > 0 {
		return b, nil
	}
	return nil, fmt.Errorf("base64")
}

func padB64(s string) string {
	s = strings.TrimRight(s, "=")
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return s
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
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
