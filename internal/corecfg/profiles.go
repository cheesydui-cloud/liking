package corecfg

import (
	"encoding/json"
	"fmt"
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

	CoreXray    = "xray"
	CoreSingbox = "singbox"
	CoreMita    = "mita"

	DefaultRealityDest = "www.cloudflare.com:443"
)

type Meta struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Core    string `json:"core"`
	NeedTLS bool   `json:"need_tls"`
	Direct  bool   `json:"direct"`
	Landing bool   `json:"landing"` // can be a chain next-hop in v1
}

func Catalog() []Meta {
	return []Meta{
		{ID: ProfileVLESSReality, Title: "VLESS + REALITY", Core: CoreXray, Landing: true, Direct: true},
		{ID: ProfileVLESSRealityVision, Title: "VLESS + REALITY + Vision", Core: CoreXray, Landing: true, Direct: true},
		{ID: ProfileVLESSXHTTP, Title: "VLESS + XHTTP + TLS", Core: CoreXray, NeedTLS: true, Landing: true, Direct: true},
		{ID: ProfileTrojanTLS, Title: "Trojan + TLS", Core: CoreXray, NeedTLS: true, Landing: true, Direct: true},
		{ID: ProfileSS2022, Title: "Shadowsocks 2022", Core: CoreXray, Landing: true, Direct: true},
		{ID: ProfileAnyTLS, Title: "AnyTLS + TCP + TLS", Core: CoreSingbox, NeedTLS: true, Direct: true},
		{ID: ProfileMieru, Title: "Mieru", Core: CoreMita, Direct: true},
	}
}

func Known(profile string) bool {
	for _, m := range Catalog() {
		if m.ID == profile {
			return true
		}
	}
	return false
}

func CoreFor(profile string) string {
	switch profile {
	case ProfileAnyTLS:
		return CoreSingbox
	case ProfileMieru:
		return CoreMita
	default:
		return CoreXray
	}
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
	default:
		return "", "", ""
	}
}

func NeedTLS(profile string) bool {
	return profile == ProfileVLESSXHTTP || profile == ProfileTrojanTLS || profile == ProfileAnyTLS
}

func CanLand(profile string) bool {
	switch profile {
	case ProfileVLESSReality, ProfileVLESSRealityVision, ProfileVLESSXHTTP, ProfileTrojanTLS, ProfileSS2022:
		return true
	default:
		return false
	}
}

func Vision(profile string) bool {
	return profile == ProfileVLESSRealityVision
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
		if st.String("dest") == "" {
			st["dest"] = DefaultRealityDest
		}
		if len(st.Strings("server_names")) == 0 {
			host := destHost(st.String("dest"))
			st["server_names"] = []string{host}
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
		if len(st.Strings("short_ids")) == 0 {
			sid, err := RandomHex(4)
			if err != nil {
				return err
			}
			st["short_ids"] = []string{sid}
		}
		if st.String("fingerprint") == "" {
			st["fingerprint"] = "chrome"
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
		if st.String("mode") == "" {
			st["mode"] = "auto"
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
	}

	if in.LineKind == "chain" {
		if exit == nil && strings.TrimSpace(in.ExitURI) == "" {
			return fmt.Errorf("链式线路需要落地入站")
		}
		if exit != nil {
			if exit.ID == in.ID && in.ID != 0 {
				return fmt.Errorf("不能把本线路当作落地")
			}
			if !CanLand(exit.Profile) {
				return fmt.Errorf("v1 链式落地仅支持 VLESS / Trojan / SS2022（Mieru / AnyTLS 不能当落地）")
			}
			if exit.LineKind == "chain" {
				return fmt.Errorf("落地必须是直出线路")
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
		in.Name = in.Profile
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
	}
	return nil
}

func destHost(dest string) string {
	host, _, ok := strings.Cut(dest, ":")
	if !ok {
		return dest
	}
	return host
}

func SSKeyLen(method string) int {
	if strings.Contains(method, "256") {
		return 32
	}
	return 16
}
