package server

import (
	"database/sql"
	"sort"
	"strconv"
	"time"

	"liking/internal/db"
	"liking/internal/version"
)

const (
	alertTrafficWarn    = 80
	alertExpireSoonDays = 7
	alertDiskWarnPct    = 10
	alertCertSoonSec    = 30 * 86400
	alertMaxItems       = 40
)

type dashAlert struct {
	Text string `json:"text"`
	To   string `json:"to,omitempty"`
	Kind string `json:"kind,omitempty"`
}

func dashboardAlerts(d *sql.DB, servers []*db.Server, users []*db.User) ([]dashAlert, []string) {
	now := time.Now()
	if d != nil {
		now = db.ClockNow(d)
	}
	nowUnix := now.Unix()
	var items []dashAlert
	push := func(text, to, kind string) {
		items = append(items, dashAlert{Text: text, To: to, Kind: kind})
	}

	for _, x := range servers {
		if x == nil {
			continue
		}
		if x.LastError != "" {
			push(x.Name+" 下发失败", "/servers", "danger")
		}
		used := x.UsedUp + x.UsedDown
		if x.TrafficLimit > 0 {
			pct := quotaPct(used, x.TrafficLimit)
			if x.OverQuota || used >= x.TrafficLimit {
				push(x.Name+" 已达流量上限，节点已停用", "/servers", "danger")
			} else if pct >= alertTrafficWarn {
				push(x.Name+" 流量已用 "+strconv.Itoa(pct)+"%", "/servers", "warn")
			}
		}
		if x.ExpiresAt > 0 {
			if x.ExpiresAt < nowUnix {
				push(x.Name+" 已到期", "/servers", "danger")
			} else if left := daysUntil(now, x.ExpiresAt); left <= alertExpireSoonDays {
				push(x.Name+" "+expireSoonText(left), "/servers", "warn")
			}
		}
		if x.Online != 1 && x.LastSeen > 0 {
			push(x.Name+" 离线", "/servers", "warn")
		}
		if x.NeedsReinstall {
			push(x.Name+" Agent 太旧，请用安装命令重装", "/servers", "warn")
		} else if x.NeedsUpgrade {
			push(x.Name+" Agent 可升级到 "+version.Version, "/servers", "warn")
		}
		if x.DiskTotal > 0 && x.DiskFree*100/x.DiskTotal < alertDiskWarnPct {
			left := x.DiskFree * 100 / x.DiskTotal
			if left < 0 {
				left = 0
			}
			push(x.Name+" 磁盘剩余 "+strconv.FormatInt(left, 10)+"%", "/servers", "warn")
		}
	}

	for _, u := range users {
		if u == nil || u.Role == "admin" || !u.Enabled {
			continue
		}
		if u.QuotaRatio >= 100 {
			push(u.Username+" 流量已用尽", "/users", "danger")
		} else if u.QuotaRatio >= alertTrafficWarn {
			push(u.Username+" 流量已用 "+strconv.Itoa(u.QuotaRatio)+"%", "/users", "warn")
		}
		exp := soonerExpiry(u.ExpiresAt, u.PkgExpires)
		if exp > 0 {
			if exp < nowUnix {
				push(u.Username+" 已到期", "/users", "danger")
			} else if left := daysUntil(now, exp); left <= alertExpireSoonDays {
				push(u.Username+" "+expireSoonText(left), "/users", "warn")
			}
		}
	}

	if d != nil {
		certs, _ := db.ListCerts(d)
		for _, c := range certs {
			if c == nil || c.ExpiresAt <= 0 {
				continue
			}
			if c.ExpiresAt < nowUnix {
				push("证书 "+c.Name+" 已过期", "/settings?tab=certs", "danger")
			} else if c.ExpiresAt < nowUnix+alertCertSoonSec {
				push("证书 "+c.Name+" 即将到期", "/settings?tab=certs", "warn")
			}
		}
	}

	items = sortAndCapAlerts(items)
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Text)
	}
	return items, out
}

func sortAndCapAlerts(items []dashAlert) []dashAlert {
	sort.SliceStable(items, func(i, j int) bool {
		return kindRank(items[i].Kind) < kindRank(items[j].Kind)
	})
	if len(items) <= alertMaxItems {
		return items
	}
	extra := len(items) - alertMaxItems
	items = items[:alertMaxItems]
	return append(items, dashAlert{
		Text: "还有 " + strconv.Itoa(extra) + " 条报警",
		To:   "/users",
		Kind: "warn",
	})
}

func kindRank(k string) int {
	if k == "danger" {
		return 0
	}
	return 1
}

func quotaPct(used, limit int64) int {
	if limit <= 0 {
		return 0
	}
	if used >= limit {
		return 100
	}
	return int(used * 100 / limit)
}

func soonerExpiry(a, b int64) int64 {
	switch {
	case a <= 0:
		return b
	case b <= 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}

func daysUntil(now time.Time, unix int64) int {
	if unix <= 0 {
		return 0
	}
	loc := now.Location()
	exp := time.Unix(unix, 0).In(loc)
	nowDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	expDay := time.Date(exp.Year(), exp.Month(), exp.Day(), 0, 0, 0, 0, loc)
	d := int(expDay.Sub(nowDay).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

func expireSoonText(days int) string {
	if days <= 0 {
		return "今天到期"
	}
	return "将于 " + strconv.Itoa(days) + " 天后到期"
}
