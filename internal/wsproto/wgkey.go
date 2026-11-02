package wsproto

import (
	"encoding/base64"
	"strings"
)

// CanonicalWGKey returns the standard base64 form of a WireGuard public key.
// wg show and the panel may differ only by padding. A value that is not a
// 32-byte key is returned trimmed, so a lookup can still try the raw text.
func CanonicalWGKey(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "(none)" {
		return ""
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return base64.StdEncoding.EncodeToString(b)
	}
	raw := strings.TrimRight(s, "=")
	if b, err := base64.RawStdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return base64.StdEncoding.EncodeToString(b)
	}
	return s
}
