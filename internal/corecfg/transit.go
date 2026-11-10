package corecfg

import "strings"

// CloneTransitSettings copies the public protocol options from a marked node.
// Secrets are left empty so Normalize generates a new key for the new port.
func CloneTransitSettings(raw string) string {
	st := ParseSettings(raw)
	out := Settings{}
	for _, k := range []string{
		"dest", "method", "transport", "sni", "host", "alpn", "fingerprint",
		"min_version", "mode", "xver", "server_names", "path", "spider_x",
		"reject_unknown_sni", "masquerade",
	} {
		v, ok := st[k]
		if !ok || v == nil {
			continue
		}
		switch x := v.(type) {
		case string:
			if strings.TrimSpace(x) == "" {
				continue
			}
		case []any:
			if len(x) == 0 {
				continue
			}
		}
		out[k] = v
	}
	if v, ok := st["obfs"]; ok {
		out["obfs"] = v
	}
	up := st.Int("up_mbps", 0)
	down := st.Int("down_mbps", 0)
	if up > 0 && down > 0 {
		out["up_mbps"] = up
		out["down_mbps"] = down
	}
	s, err := out.Marshal()
	if err != nil {
		return "{}"
	}
	return s
}
