package corecfg

import (
	"database/sql"
	"sort"
	"strconv"

	"liking/internal/db"
	"liking/internal/wsproto"
)

// SpeedMarkBase is 'L' in the high 8 bits. Low 24 bits are user id.
const SpeedMarkBase uint32 = 0x4C000000

func SpeedMark(userID int64) uint32 {
	if userID < 1 || userID > 0x00FFFFFF {
		return 0
	}
	return SpeedMarkBase | uint32(userID)
}

func limitTag(mark uint32) string {
	return "limit-" + strconv.FormatUint(uint64(mark), 10)
}

func throttleProfile(profile string) bool {
	switch profile {
	case ProfileVLESSReality, ProfileVLESSRealityVision, ProfileVLESSXHTTP, ProfileTrojanTLS, ProfileSS2022, ProfileAnyTLS:
		return true
	default:
		return false
	}
}

func directThrottle(in *db.Inbound) bool {
	return in != nil && in.Enabled && in.LineKind != "chain" && throttleProfile(in.Profile)
}

func loadUserSpeeds(d *sql.DB, clients map[int64][]*db.Client) map[int64]int64 {
	out := map[int64]int64{}
	if d == nil {
		return out
	}
	seen := map[int64]struct{}{}
	for _, cs := range clients {
		for _, c := range cs {
			if c == nil || c.UserID < 1 {
				continue
			}
			if _, ok := seen[c.UserID]; ok {
				continue
			}
			seen[c.UserID] = struct{}{}
			u, err := db.GetUser(d, c.UserID)
			if err != nil || u == nil || u.SpeedLimit < 1 {
				continue
			}
			out[u.ID] = u.SpeedLimit
		}
	}
	return out
}

func collectSpeedLimits(ins []*db.Inbound, clients map[int64][]*db.Client, speeds map[int64]int64) []wsproto.SpeedLimit {
	byMark := map[uint32]int64{}
	for _, in := range ins {
		if !directThrottle(in) {
			continue
		}
		for _, c := range clients[in.ID] {
			if c == nil || !c.Enabled {
				continue
			}
			mbps := speeds[c.UserID]
			if mbps < 1 {
				continue
			}
			mark := SpeedMark(c.UserID)
			if mark == 0 {
				continue
			}
			byMark[mark] = mbps
		}
	}
	if len(byMark) == 0 {
		return nil
	}
	out := make([]wsproto.SpeedLimit, 0, len(byMark))
	for mark, mbps := range byMark {
		out = append(out, wsproto.SpeedLimit{Mark: mark, Mbps: mbps})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mark < out[j].Mark })
	return out
}

func clientSpeedMark(c *db.Client, speeds map[int64]int64) (uint32, bool) {
	if c == nil || !c.Enabled || c.Email == "" {
		return 0, false
	}
	if speeds[c.UserID] < 1 {
		return 0, false
	}
	mark := SpeedMark(c.UserID)
	if mark == 0 {
		return 0, false
	}
	return mark, true
}
