package agent

import (
	"bytes"
	"encoding/json"
)

// stripV2RayAPI removes experimental.v2ray_api. Official sing-box builds omit
// that tag, and a failed check rolls back the whole SOCKS5 config.
func stripV2RayAPI(raw []byte) []byte {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return raw
	}
	exp, _ := top["experimental"].(map[string]any)
	if exp == nil {
		return raw
	}
	if _, ok := exp["v2ray_api"]; !ok {
		return raw
	}
	delete(exp, "v2ray_api")
	if len(exp) == 0 {
		delete(top, "experimental")
	} else {
		top["experimental"] = exp
	}
	out, err := json.Marshal(top)
	if err != nil {
		return raw
	}
	return out
}
