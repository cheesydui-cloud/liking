package corecfg

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"liking/internal/db"
)

const (
	ProfileVLESSReality       = "vless-reality"
	ProfileVLESSRealityVision = "vless-reality-vision"
	ProfileVLESSXHTTP         = "vless-xhttp-tls"
	ProfileTrojanTLS          = "trojan-tls"
	ProfileSS2022             = "ss2022"
	ProfileAnyTLS             = "anytls"
	ProfileMieru              = "mieru"
	ProfileSOCKS5             = "socks5"
	ProfilePortForward        = "port-forward"

	CoreXray    = "xray"
	CoreSingbox = "singbox"
	CoreMita    = "mita"

	DefaultRealityDest = "www.microsoft.com:443"
)

type Meta struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Core    string `json:"core"`
	Desc    string `json:"desc"`
	NeedTLS bool   `json:"need_tls"`
	Direct  bool   `json:"direct"`
	Landing bool   `json:"landing"` // can be a chain next-hop in v1
}

func Catalog() []Meta {
	return []Meta{
		{ID: ProfileVLESSRealityVision, Title: "VLESS + REALITY + Vision", Core: CoreXray, Landing: true, Direct: true, Desc: "伪装成访问真实网站，不需要证书。Vision 再包一层流量，抗主动探测更好。"},
		{ID: ProfileVLESSReality, Title: "VLESS + REALITY", Core: CoreXray, Landing: true, Direct: true, Desc: "伪装成访问真实网站，不需要证书。适合没有域名的机器。"},
		{ID: ProfileVLESSXHTTP, Title: "VLESS + XHTTP + TLS", Core: CoreXray, NeedTLS: true, Landing: true, Direct: true, Desc: "走 HTTPS 外观，需要证书。路径和 SNI 可自定义。"},
		{ID: ProfileTrojanTLS, Title: "Trojan + TLS", Core: CoreXray, NeedTLS: true, Landing: true, Direct: true, Desc: "经典 TLS 隧道，需要证书。实现简单，客户端支持广。"},
		{ID: ProfileSS2022, Title: "Shadowsocks 2022", Core: CoreXray, Landing: true, Direct: true, Desc: "2022-blake3 AEAD。没有 TLS 指纹，实现干净。"},
		{ID: ProfileAnyTLS, Title: "AnyTLS + TCP + TLS", Core: CoreSingbox, NeedTLS: true, Direct: true, Desc: "sing-box 实现，需要证书。不能当链式落地。"},
		{ID: ProfileMieru, Title: "Mieru", Core: CoreMita, Direct: true, Desc: "只当入口，不能当链式落地。可同时开 TCP / UDP。"},
		{ID: ProfileSOCKS5, Title: "SOCKS5", Core: CoreSingbox, Landing: true, Direct: true, Desc: "用户名密码认证，可 UDP。浏览器和 Clash 可直连。走 sing-box。可当链式落地。"},
	}
}

func Known(profile string) bool {
	if profile == ProfilePortForward {
		return true
	}
	for _, m := range Catalog() {
		if m.ID == profile {
			return true
		}
	}
	return false
}

// UserFacing is false for inbounds that occupy a port but never appear in
// user subscriptions (port-forward pipes).
func UserFacing(profile string) bool {
	return profile != ProfilePortForward
}

func CoreFor(profile string) string {
	switch profile {
	case ProfileAnyTLS, ProfileSOCKS5:
		return CoreSingbox
	case ProfileMieru:
		return CoreMita
	default:
		return CoreXray
	}
}

func NormalizeCore(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "sing-box", "singbox":
		return CoreSingbox
	case "mieru", "mita":
		return CoreMita
	case "xray":
		return CoreXray
	default:
		return s
	}
}

func KnownCore(s string) bool {
	switch NormalizeCore(s) {
	case CoreXray, CoreSingbox, CoreMita:
		return true
	default:
		return false
	}
}

func CoreNames() []string {
	return []string{CoreXray, CoreSingbox, CoreMita}
}

func Spec(profile string) (protocol, network, security string) {
	switch profile {
	case ProfileVLESSReality, ProfileVLESSRealityVision:
		return "vless", "tcp", "reality"
	case ProfileVLESSXHTTP:
		return "vless", "xhttp", "tls"
	case ProfileTrojanTLS:
		return "trojan", "tcp", "tls"
	case ProfileSS2022:
		return "shadowsocks", "tcp", "none"
	case ProfileAnyTLS:
		return "anytls", "tcp", "tls"
	case ProfileMieru:
		return "mieru", "tcp", "none"
	case ProfileSOCKS5:
		return "socks", "tcp", "none"
	case ProfilePortForward:
		return "dokodemo-door", "tcp", "none"
	default:
		return "", "", ""
	}
}

func NeedTLS(profile string) bool {
	return profile == ProfileVLESSXHTTP || profile == ProfileTrojanTLS || profile == ProfileAnyTLS
}

func CanLand(profile string) bool {
	switch profile {
	case ProfileVLESSReality, ProfileVLESSRealityVision, ProfileVLESSXHTTP, ProfileTrojanTLS, ProfileSS2022, ProfileSOCKS5:
		return true
	default:
		return false
	}
}

func Vision(profile string) bool {
	return profile == ProfileVLESSRealityVision
}

func Reality(profile string) bool {
	return profile == ProfileVLESSReality || profile == ProfileVLESSRealityVision
}

type Settings map[string]any

func ParseSettings(raw string) Settings {
	if strings.TrimSpace(raw) == "" {
		return Settings{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil || m == nil {
		return Settings{}
	}
	return Settings(m)
}

func (s Settings) String(key string) string {
	v, ok := s[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func (s Settings) Strings(key string) []string {
	v, ok := s[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if xs, ok := x.(string); ok && xs != "" {
				out = append(out, xs)
			}
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	}
	return nil
}

func (s Settings) Bool(key string, def bool) bool {
	v, ok := s[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	case float64:
		return t != 0
	}
	return def
}

func (s Settings) Int(key string, def int) int {
	v, ok := s[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return def
		}
		return int(n)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return def
		}
		return n
	}
	return def
}

func (s Settings) ALPN() []string {
	raw := s.Strings("alpn")
	var out []string
	for _, x := range raw {
		for _, p := range strings.Split(x, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	if len(out) == 0 {
		return []string{"h2", "http/1.1"}
	}
	return out
}

func (s Settings) Marshal() (string, error) {
	if s == nil {
		return "{}", nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Normalize fills protocol/core and generates missing secrets.
// exit is the landing inbound when in.LineKind == "chain".
func Normalize(in *db.Inbound, exit *db.Inbound) error {
	if !Known(in.Profile) {
		return fmt.Errorf("未知协议: %s", in.Profile)
	}
	if in.Port < 1 || in.Port > 65535 {
		return fmt.Errorf("端口无效")
	}
	p, n, sec := Spec(in.Profile)
	in.Protocol, in.Network, in.Security = p, n, sec
	in.Core = CoreFor(in.Profile)
	if strings.TrimSpace(in.Listen) == "" {
		in.Listen = "0.0.0.0"
	}
	if in.LineKind == "" {
		in.LineKind = "direct"
	}
	st := ParseSettings(in.Settings)

	switch in.Profile {
	case ProfileVLESSReality, ProfileVLESSRealityVision:
		dest := strings.TrimSpace(st.String("dest"))
		if dest == "" {
			dest = DefaultRealityDest
		}
		if !strings.Contains(dest, ":") {
			dest += ":443"
		}
		st["dest"] = dest
		if err := rejectLocalDest(dest); err != nil {
			return err
		}
		if len(st.Strings("server_names")) == 0 {
			st["server_names"] = []string{destHost(dest)}
		}
		if st.String("private_key") == "" {
			priv, pub, err := GenerateRealityKeyPair()
			if err != nil {
				return err
			}
			st["private_key"] = priv
			st["public_key"] = pub
		} else if st.String("public_key") == "" {
			pub, err := PublicFromPrivate(st.String("private_key"))
			if err != nil {
				return fmt.Errorf("REALITY 私钥无效: %w", err)
			}
			st["public_key"] = pub
		}
		ids, err := normalizeShortIDs(st.Strings("short_ids"))
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			sid, err := RandomHex(4)
			if err != nil {
				return err
			}
			ids = []string{sid}
		}
		st["short_ids"] = ids
		fp := strings.ToLower(strings.TrimSpace(st.String("fingerprint")))
		if !validFingerprint(fp) {
			fp = "chrome"
		}
		st["fingerprint"] = fp
		xver := st.Int("xver", 0)
		if xver < 0 || xver > 2 {
			return fmt.Errorf("xver 只能是 0 / 1 / 2")
		}
		st["xver"] = xver
		if spx := strings.TrimSpace(st.String("spider_x")); spx != "" {
			if !strings.HasPrefix(spx, "/") {
				spx = "/" + spx
			}
			st["spider_x"] = spx
		} else {
			delete(st, "spider_x")
		}
	case ProfileVLESSXHTTP:
		if st.String("path") == "" {
			p, err := RandomHex(6)
			if err != nil {
				return err
			}
			st["path"] = "/" + p
		}
		if !strings.HasPrefix(st.String("path"), "/") {
			st["path"] = "/" + st.String("path")
		}
		mode := st.String("mode")
		switch mode {
		case "", "auto":
			st["mode"] = "auto"
		case "packet-up", "stream-up", "stream-one":
		default:
			return fmt.Errorf("XHTTP mode 无效")
		}
		fillTLSDefaults(st)
		if fp := strings.ToLower(strings.TrimSpace(st.String("fingerprint"))); validFingerprint(fp) {
			st["fingerprint"] = fp
		} else if st.String("fingerprint") == "" {
			st["fingerprint"] = "chrome"
		}
	case ProfileTrojanTLS, ProfileAnyTLS:
		fillTLSDefaults(st)
		if fp := strings.ToLower(strings.TrimSpace(st.String("fingerprint"))); validFingerprint(fp) {
			st["fingerprint"] = fp
		} else if st.String("fingerprint") == "" {
			st["fingerprint"] = "chrome"
		}
	case ProfileSS2022:
		if st.String("method") == "" {
			st["method"] = "2022-blake3-aes-128-gcm"
		}
		if st.String("server_password") == "" {
			n := 16
			if strings.Contains(st.String("method"), "256") {
				n = 32
			}
			pw, err := RandomBase64(n)
			if err != nil {
				return err
			}
			st["server_password"] = pw
		}
	case ProfileMieru:
		tr := strings.ToUpper(st.String("transport"))
		if tr == "" {
			tr = "BOTH"
		}
		if tr != "TCP" && tr != "UDP" && tr != "BOTH" {
			return fmt.Errorf("Mieru transport 必须是 TCP / UDP / BOTH")
		}
		st["transport"] = tr
	case ProfileSOCKS5:
		st["udp"] = true
		if st.String("hold_user") == "" {
			id, err := RandomHex(8)
			if err != nil {
				return err
			}
			st["hold_user"] = "hold." + id
		}
		if st.String("hold_pass") == "" {
			pw, err := RandomHex(16)
			if err != nil {
				return err
			}
			st["hold_pass"] = pw
		}
	case ProfilePortForward:
		host := strings.TrimSpace(st.String("dest_host"))
		if host == "" {
			return fmt.Errorf("端口中转需要目标地址")
		}
		port := st.Int("dest_port", 0)
		if port < 1 || port > 65535 {
			return fmt.Errorf("端口中转目标端口无效")
		}
		st["dest_host"] = host
		st["dest_port"] = port
		netw := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(st.String("network")), " ", ""))
		switch netw {
		case "", "tcp":
			st["network"] = "tcp"
		case "tcp,udp":
			st["network"] = "tcp,udp"
		default:
			return fmt.Errorf("端口中转 network 必须是 tcp 或 tcp,udp")
		}
		in.LineKind = "direct"
		in.ExitInboundID = nil
		in.ExitURI = ""
		delete(st, "hops")
	}

	if in.LineKind == "chain" {
		if err := NormalizeHops(st); err != nil {
			return err
		}
		uri := strings.TrimSpace(in.ExitURI)
		if exit != nil && uri != "" {
			return fmt.Errorf("落地节点和出口链接不能同时填")
		}
		if exit == nil && uri == "" {
			return fmt.Errorf("链式线路需要落地入站或出口链接")
		}
		if uri != "" {
			t, err := ParseShareURI(uri)
			if err != nil {
				return err
			}
			in.ExitURI = strings.TrimSpace(t.Raw)
			if in.ExitURI == "" {
				in.ExitURI = uri
			}
			in.ExitInboundID = nil
		}
		if len(ParseHops(st))+1 > MaxHops {
			return fmt.Errorf("最多 %d 跳", MaxHops)
		}
		if exit != nil {
			if exit.ID == in.ID && in.ID != 0 {
				return fmt.Errorf("不能把本线路当作落地")
			}
			if !CanLand(exit.Profile) {
				return fmt.Errorf("v1 链式落地仅支持 VLESS / Trojan / SS2022 / SOCKS5（Mieru / AnyTLS 不能当落地）")
			}
			if exit.LineKind == "chain" {
				return fmt.Errorf("落地必须是直出线路")
			}
			for _, h := range ParseHops(st) {
				if h.InboundID == exit.ID {
					return fmt.Errorf("路径里不能重复同一节点")
				}
				if in.ID != 0 && h.InboundID == in.ID {
					return fmt.Errorf("不能把本线路当作跳点")
				}
			}
			if err := ensureRelay(st, exit.Profile); err != nil {
				return err
			}
		}
	}

	raw, err := st.Marshal()
	if err != nil {
		return err
	}
	in.Settings = raw
	if in.Name == "" {
		in.Name = fmt.Sprintf("%s-%d", in.Profile, in.Port)
	}
	return nil
}

func ensureRelay(st Settings, landingProfile string) error {
	switch landingProfile {
	case ProfileVLESSReality, ProfileVLESSRealityVision, ProfileVLESSXHTTP:
		if st.String("relay_uuid") == "" {
			id, err := NewUUID()
			if err != nil {
				return err
			}
			st["relay_uuid"] = id
		}
	case ProfileTrojanTLS:
		if st.String("relay_password") == "" {
			pw, err := RandomHex(16)
			if err != nil {
				return err
			}
			st["relay_password"] = pw
		}
	case ProfileSS2022:
		if st.String("relay_password") == "" {
			pw, err := RandomBase64(16)
			if err != nil {
				return err
			}
			st["relay_password"] = pw
		}
	case ProfileSOCKS5:
		if st.String("relay_username") == "" {
			id, err := RandomHex(8)
			if err != nil {
				return err
			}
			st["relay_username"] = "relay." + id
		}
		if st.String("relay_password") == "" {
			pw, err := RandomHex(16)
			if err != nil {
				return err
			}
			st["relay_password"] = pw
		}
	}
	return nil
}

func fillTLSDefaults(st Settings) {
	mv := st.String("min_version")
	if mv != "1.2" && mv != "1.3" {
		st["min_version"] = "1.3"
	}
	if _, ok := st["reject_unknown_sni"]; !ok {
		st["reject_unknown_sni"] = true
	}
	if len(st.Strings("alpn")) == 0 {
		st["alpn"] = []string{"h2", "http/1.1"}
	}
}

func validFingerprint(s string) bool {
	switch s {
	case "chrome", "firefox", "safari", "ios", "android", "edge", "qq", "random", "randomized":
		return true
	}
	return false
}

func normalizeShortIDs(ids []string) ([]string, error) {
	var out []string
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		if len(id)%2 != 0 || len(id) > 16 {
			return nil, fmt.Errorf("short_id 须为偶数位十六进制，最长 16 位")
		}
		for _, c := range id {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return nil, fmt.Errorf("short_id 须为十六进制")
			}
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func destHost(dest string) string {
	host, _, ok := strings.Cut(dest, ":")
	if !ok {
		return dest
	}
	return host
}

func rejectLocalDest(dest string) error {
	switch strings.ToLower(strings.TrimSpace(destHost(dest))) {
	case "", "localhost", "127.0.0.1", "0.0.0.0", "::1":
		return fmt.Errorf("REALITY dest 必须是可公开访问的网站，不能指向本机")
	}
	return nil
}

// DestIsSelf reports whether dest's host is this machine's public address
// (steal-self). REALITY must masquerade as someone else.
func DestIsSelf(dest string, addrs ...string) bool {
	h := strings.ToLower(strings.TrimSpace(destHost(dest)))
	if h == "" {
		return false
	}
	for _, a := range addrs {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if strings.Contains(a, ":") && !strings.HasPrefix(a, "[") {
			if host, _, ok := strings.Cut(a, ":"); ok {
				a = host
			}
		}
		if a == h {
			return true
		}
	}
	return false
}

func SSKeyLen(method string) int {
	if strings.Contains(method, "256") {
		return 32
	}
	return 16
}
