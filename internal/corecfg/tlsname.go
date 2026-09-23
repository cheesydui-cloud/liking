package corecfg

import (
	"net"
	"strings"
)

// tlsServerName returns a hostname suitable for TLS SNI. IP addresses are
// rejected; an empty result means the caller should omit server_name.
func tlsServerName(explicit, fallback string) string {
	if s := strings.TrimSpace(explicit); s != "" && net.ParseIP(s) == nil {
		return s
	}
	if s := strings.TrimSpace(fallback); s != "" && net.ParseIP(s) == nil {
		return s
	}
	return ""
}

func singXHTTPTransport(path, mode, host string) map[string]any {
	tr := map[string]any{
		"type": "xhttp",
		"path": nz(path, "/"),
		"mode": nz(mode, "auto"),
	}
	if h := strings.TrimSpace(host); h != "" {
		tr["host"] = h
	}
	return tr
}
