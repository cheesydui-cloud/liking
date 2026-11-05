package corecfg

import (
	"database/sql"
	"encoding/base64"
	"strings"

	"liking/internal/db"
)

func KnownSS2022Method(method string) bool {
	_, ok := canonicalSS2022Method(method)
	return ok
}

func canonicalSS2022Method(method string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		return strings.ToLower(strings.TrimSpace(method)), true
	default:
		return "", false
	}
}

func SSKeyLen(method string) int {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		return 32
	default:
		return 16
	}
}

func ssPasswordOK(pw string, n int) bool {
	pw = strings.TrimSpace(pw)
	if pw == "" || n < 1 {
		return false
	}
	decoders := []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
	}
	for _, dec := range decoders {
		b, err := dec(pw)
		if err == nil && len(b) == n {
			return true
		}
	}
	return false
}

func landingSSKeyLen(land *db.Inbound) (int, bool) {
	if land == nil || land.Profile != ProfileSS2022 {
		return 0, false
	}
	method := strings.TrimSpace(ParseSettings(land.Settings).String("method"))
	if method == "" {
		return SSKeyLen("2022-blake3-aes-128-gcm"), true
	}
	canon, ok := canonicalSS2022Method(method)
	if !ok {
		return 0, false
	}
	return SSKeyLen(canon), true
}

func rewriteRelayPassword(st Settings, land *db.Inbound) (bool, error) {
	n, ok := landingSSKeyLen(land)
	if !ok {
		return false, nil
	}
	if ssPasswordOK(st.String("relay_password"), n) {
		return false, nil
	}
	pw, err := RandomBase64(n)
	if err != nil {
		return false, err
	}
	st["relay_password"] = pw
	return true, nil
}

// RepairSS2022 rewrites SS2022 server, user, and relay keys whose decoded
// length does not match the method. Keys that already match are left as they
// are. An unknown method is left untouched so one bad node does not block
// every other server's sync.
func RepairSS2022(d *sql.DB) error {
	all, err := db.ListInbounds(d)
	if err != nil {
		return err
	}
	byID := map[int64]*db.Inbound{}
	for _, in := range all {
		if in == nil {
			continue
		}
		byID[in.ID] = in
	}
	for _, in := range all {
		if in == nil || in.Profile != ProfileSS2022 {
			continue
		}
		st := ParseSettings(in.Settings)
		rawMethod := strings.TrimSpace(st.String("method"))
		method := "2022-blake3-aes-128-gcm"
		if rawMethod != "" {
			canon, ok := canonicalSS2022Method(rawMethod)
			if !ok {
				continue
			}
			method = canon
		}
		changed := st.String("method") != method
		st["method"] = method
		n := SSKeyLen(method)
		if !ssPasswordOK(st.String("server_password"), n) {
			pw, err := RandomBase64(n)
			if err != nil {
				return err
			}
			st["server_password"] = pw
			changed = true
		}
		if changed {
			raw, err := st.Marshal()
			if err != nil {
				return err
			}
			in.Settings = raw
			if err := db.UpdateInbound(d, in); err != nil {
				return err
			}
		}
		clients, err := db.ListClientsByInbound(d, in.ID)
		if err != nil {
			return err
		}
		for _, c := range clients {
			if c == nil || ssPasswordOK(c.Password, n) {
				continue
			}
			pw, err := RandomBase64(n)
			if err != nil {
				return err
			}
			c.Password = pw
			if err := db.UpsertClient(d, c); err != nil {
				return err
			}
		}
	}
	for _, in := range all {
		if in == nil {
			continue
		}
		st := ParseSettings(in.Settings)
		changed := false
		if in.ExitInboundID != nil {
			ok, err := rewriteRelayPassword(st, byID[*in.ExitInboundID])
			if err != nil {
				return err
			}
			if ok {
				changed = true
			}
		}
		hops := ParseHops(st)
		if len(hops) > 0 {
			hopChanged := false
			out := make([]any, 0, len(hops))
			for _, h := range hops {
				if h.Kind == "panel" {
					cred := h.cred()
					ok, err := rewriteRelayPassword(cred, byID[h.InboundID])
					if err != nil {
						return err
					}
					if ok {
						h.RelayPassword = cred.String("relay_password")
						hopChanged = true
					}
				}
				out = append(out, h.Map())
			}
			if hopChanged {
				st["hops"] = out
				changed = true
			}
		}
		if !changed {
			continue
		}
		raw, err := st.Marshal()
		if err != nil {
			return err
		}
		in.Settings = raw
		if err := db.UpdateInbound(d, in); err != nil {
			return err
		}
	}
	return nil
}
