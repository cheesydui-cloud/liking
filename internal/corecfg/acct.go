package corecfg

import (
	"sort"

	"liking/internal/db"
	"liking/internal/wsproto"
)

// acctProfile is a direct inbound whose users can be split onto marked outbounds.
// Chain lines keep the shared chain outbound. Port forward and Mieru have no
// per-user route key on this core.
func acctProfile(in *db.Inbound) bool {
	if in == nil || !in.Enabled || in.LineKind == "chain" {
		return false
	}
	switch in.Profile {
	case ProfileVLESSReality, ProfileVLESSRealityVision, ProfileVLESSXHTTP,
		ProfileTrojanTLS, ProfileSS2022, ProfileAnyTLS, ProfileSOCKS5:
		return true
	default:
		return false
	}
}

// AcctMark is the fwmark written on that client's outbound sockets.
// The low 24 bits prefer the client row id so two protocols of one user
// stay separate. A zero id falls back to user and inbound so tests still split.
func AcctMark(c *db.Client, inboundID int64) uint32 {
	if c == nil {
		return 0
	}
	id := c.ID
	if id < 1 {
		inID := c.InboundID
		if inID < 1 {
			inID = inboundID
		}
		id = c.UserID<<12 ^ inID
		if id < 1 {
			id = c.UserID
		}
	}
	if id < 1 {
		return 0
	}
	if id > 0x00FFFFFF {
		id &= 0x00FFFFFF
		if id < 1 {
			return 0
		}
	}
	return SpeedMarkBase | uint32(id)
}

func clientAcct(in *db.Inbound, c *db.Client) (mark uint32, email string, ok bool) {
	if !acctProfile(in) || c == nil || !c.Enabled {
		return 0, "", false
	}
	email = clientRouteUser(in, c)
	if email == "" || IsRelayEmail(email) {
		return 0, "", false
	}
	mark = AcctMark(c, in.ID)
	if mark == 0 {
		return 0, "", false
	}
	return mark, email, true
}

func collectAcctMarks(ins []*db.Inbound, clients map[int64][]*db.Client) []wsproto.AcctMark {
	seen := map[uint32]string{}
	var order []uint32
	for _, in := range ins {
		if in == nil {
			continue
		}
		for _, c := range clients[in.ID] {
			mark, email, ok := clientAcct(in, c)
			if !ok {
				continue
			}
			if prev, exists := seen[mark]; exists {
				if prev != email {
					continue
				}
				continue
			}
			seen[mark] = email
			order = append(order, mark)
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]wsproto.AcctMark, 0, len(order))
	for _, mark := range order {
		out = append(out, wsproto.AcctMark{Mark: mark, Email: seen[mark]})
	}
	return out
}
