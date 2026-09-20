package corecfg

import (
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"liking/internal/db"
)

const MaxSiteDenyCustom = 50

// ClientHealthCheckURL is Clash / sing-box url-test. Not Google, so a Google
// deny still lets the node look alive.
const ClientHealthCheckURL = "https://cp.cloudflare.com/"

var siteDenyCatalog = []struct {
	Name  string
	Label string
}{
	{Name: "google", Label: "谷歌"},
	{Name: "youtube", Label: "油管"},
	{Name: "tiktok", Label: "TikTok"},
}

var siteDenyDomains = map[string][]string{
	"google": {
		"google.com", "google.com.hk", "google.com.tw", "google.com.sg",
		"google.com.au", "google.com.br", "google.co.jp", "google.co.kr",
		"google.co.uk", "google.co.in", "google.de", "google.fr",
		"googleapis.com", "gstatic.com", "googleusercontent.com",
		"gmail.com", "googlemail.com", "android.com", "chrome.com",
		"chromium.org", "googleadservices.com", "googlesyndication.com",
		"google-analytics.com", "googletagmanager.com", "googletagservices.com",
		"doubleclick.net", "gvt1.com", "gvt2.com", "gvt3.com", "1e100.net",
		"withgoogle.com", "googleblog.com", "ggpht.com",
	},
	"youtube": {
		"youtube.com", "youtu.be", "youtube-nocookie.com", "youtubekids.com",
		"googlevideo.com", "ytimg.com", "youtubei.googleapis.com",
		"youtubeeducation.com",
	},
	"tiktok": {
		"tiktok.com", "tiktokv.com", "tiktokcdn.com", "tiktokcdn-us.com",
		"tiktokcdn-eu.com", "tiktokv.us", "tiktokv.eu", "tiktokv.sg",
		"musical.ly", "muscdn.com", "bytedance.com", "bytedance.net",
		"byteoversea.com", "ibyteimg.com", "ibytedtos.com", "isnssdk.com",
		"ttlivecdn.com", "tiktokrow-cdn.com", "byteimg.com", "bytescm.com",
		"bytednsdoc.com",
	},
}

var siteDenyIPs = map[string][]string{
	"google": {
		"8.8.8.8/32", "8.8.4.4/32",
		"2001:4860:4860::8888/128", "2001:4860:4860::8844/128",
	},
}

type SiteDeny struct {
	Categories []string
	Domains    []string
}

func (s SiteDeny) Empty() bool {
	return len(s.Categories) == 0 && len(s.Domains) == 0
}

func (s SiteDeny) key() string {
	cats := append([]string{}, s.Categories...)
	doms := append([]string{}, s.Domains...)
	sort.Strings(cats)
	sort.Strings(doms)
	return strings.Join(cats, ",") + "|" + strings.Join(doms, ",")
}

func SiteDenyCatalogPublic() []map[string]string {
	out := make([]map[string]string, 0, len(siteDenyCatalog))
	for _, c := range siteDenyCatalog {
		out = append(out, map[string]string{"name": c.Name, "label": c.Label})
	}
	return out
}

func NormalizeSiteDenyCategories(names []string) []string {
	want := map[string]bool{}
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n != "" {
			want[n] = true
		}
	}
	var out []string
	for _, c := range siteDenyCatalog {
		if want[c.Name] {
			out = append(out, c.Name)
		}
	}
	return out
}

func NormalizeSiteDenyCustom(items []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, raw := range items {
		n := normalizeDenyHost(raw)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
		if len(out) > MaxSiteDenyCustom {
			return nil, fmt.Errorf("自定义域名最多 %d 个", MaxSiteDenyCustom)
		}
	}
	return out, nil
}

func NormalizeSiteDeny(cats, custom []string) (SiteDeny, error) {
	doms, err := NormalizeSiteDenyCustom(custom)
	if err != nil {
		return SiteDeny{}, err
	}
	return SiteDeny{
		Categories: NormalizeSiteDenyCategories(cats),
		Domains:    doms,
	}, nil
}

func siteDenyFromUser(u *db.User) SiteDeny {
	if u == nil {
		return SiteDeny{}
	}
	d, err := NormalizeSiteDeny(u.SiteDenyCategories, u.SiteDenyDomains)
	if err != nil {
		d.Categories = NormalizeSiteDenyCategories(u.SiteDenyCategories)
		d.Domains, _ = capDenyCustom(u.SiteDenyDomains)
	}
	return d
}

func capDenyCustom(items []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, raw := range items {
		n := normalizeDenyHost(raw)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
		if len(out) >= MaxSiteDenyCustom {
			break
		}
	}
	return out, nil
}

func normalizeDenyHost(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" || strings.HasPrefix(s, "#") {
		return ""
	}
	s = strings.TrimPrefix(s, "*.")
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err == nil && u.Host != "" {
			s = u.Host
		}
	}
	if _, n, err := net.ParseCIDR(s); err == nil {
		return n.String()
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(s, ".")
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	s = strings.Trim(s, "[]")
	if ip := net.ParseIP(s); ip != nil {
		if ip.To4() != nil {
			return ip.String() + "/32"
		}
		return ip.String() + "/128"
	}
	if !validDenyHostname(s) {
		return ""
	}
	return s
}

func validDenyHostname(s string) bool {
	if len(s) < 3 || len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	for _, lab := range strings.Split(s, ".") {
		if lab == "" || len(lab) > 63 {
			return false
		}
		if lab[0] == '-' || lab[len(lab)-1] == '-' {
			return false
		}
		for i := 0; i < len(lab); i++ {
			c := lab[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

func isCIDR(s string) bool {
	_, _, err := net.ParseCIDR(s)
	return err == nil
}

func ResolveSiteDeny(s SiteDeny) (domains, ips []string) {
	seenD := map[string]bool{}
	seenI := map[string]bool{}
	addD := func(list []string) {
		for _, d := range list {
			if d == "" || seenD[d] {
				continue
			}
			seenD[d] = true
			domains = append(domains, d)
		}
	}
	addI := func(list []string) {
		for _, ip := range list {
			if ip == "" || seenI[ip] {
				continue
			}
			seenI[ip] = true
			ips = append(ips, ip)
		}
	}
	for _, c := range s.Categories {
		addD(siteDenyDomains[c])
		addI(siteDenyIPs[c])
	}
	var customD, customI []string
	for _, n := range s.Domains {
		if isCIDR(n) {
			customI = append(customI, n)
		} else {
			customD = append(customD, n)
		}
	}
	addD(customD)
	addI(customI)
	return domains, ips
}

func loadUserSiteDenies(d *sql.DB, clients map[int64][]*db.Client) map[int64]SiteDeny {
	out := map[int64]SiteDeny{}
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
			if err != nil || u == nil {
				continue
			}
			sd := siteDenyFromUser(u)
			if sd.Empty() {
				continue
			}
			out[u.ID] = sd
		}
	}
	return out
}

func clientRouteUser(in *db.Inbound, c *db.Client) string {
	if c == nil || !c.Enabled {
		return ""
	}
	if IsRelayEmail(c.Email) {
		return ""
	}
	if in != nil && in.Profile == ProfileSOCKS5 {
		return strings.TrimSpace(clientSocksUser(c))
	}
	return strings.TrimSpace(c.Email)
}

type siteDenyGroup struct {
	Tags    []string
	Users   []string
	Domains []string
	IPs     []string
}

func collectSiteDenyGroups(inbounds []*db.Inbound, clients map[int64][]*db.Client, denies map[int64]SiteDeny, core string) []siteDenyGroup {
	type acc struct {
		tags    []string
		users   []string
		seenT   map[string]bool
		seenU   map[string]bool
		domains []string
		ips     []string
	}
	byKey := map[string]*acc{}
	var order []string
	for _, in := range inbounds {
		if in == nil || !in.Enabled || in.Core != core || in.Profile == ProfilePortForward || in.Profile == ProfileMieru {
			continue
		}
		tag := inboundTag(in.ID)
		for _, c := range clients[in.ID] {
			user := clientRouteUser(in, c)
			if user == "" {
				continue
			}
			d, ok := denies[c.UserID]
			if !ok || d.Empty() {
				continue
			}
			key := d.key()
			a := byKey[key]
			if a == nil {
				doms, ips := ResolveSiteDeny(d)
				if len(doms) == 0 && len(ips) == 0 {
					continue
				}
				a = &acc{seenT: map[string]bool{}, seenU: map[string]bool{}, domains: doms, ips: ips}
				byKey[key] = a
				order = append(order, key)
			}
			if !a.seenT[tag] {
				a.seenT[tag] = true
				a.tags = append(a.tags, tag)
			}
			if !a.seenU[user] {
				a.seenU[user] = true
				a.users = append(a.users, user)
			}
		}
	}
	out := make([]siteDenyGroup, 0, len(order))
	for _, k := range order {
		a := byKey[k]
		out = append(out, siteDenyGroup{Tags: a.tags, Users: a.users, Domains: a.domains, IPs: a.ips})
	}
	return out
}

func xraySiteDenyRules(groups []siteDenyGroup) []any {
	var rules []any
	for _, g := range groups {
		if len(g.Users) == 0 || len(g.Tags) == 0 {
			continue
		}
		if len(g.Domains) > 0 {
			items := make([]string, 0, len(g.Domains))
			for _, d := range g.Domains {
				items = append(items, "domain:"+d)
			}
			rules = append(rules, map[string]any{
				"type":        "field",
				"inboundTag":  g.Tags,
				"user":        g.Users,
				"domain":      items,
				"outboundTag": "block",
			})
		}
		if len(g.IPs) > 0 {
			rules = append(rules, map[string]any{
				"type":        "field",
				"inboundTag":  g.Tags,
				"user":        g.Users,
				"ip":          g.IPs,
				"outboundTag": "block",
			})
		}
	}
	return rules
}

func singSiteDenyRules(groups []siteDenyGroup) []any {
	var rules []any
	for _, g := range groups {
		if len(g.Users) == 0 || len(g.Tags) == 0 {
			continue
		}
		if len(g.Domains) > 0 {
			rules = append(rules, map[string]any{
				"inbound":       g.Tags,
				"auth_user":     g.Users,
				"domain_suffix": g.Domains,
				"action":        "reject",
			})
		}
		if len(g.IPs) > 0 {
			rules = append(rules, map[string]any{
				"inbound":   g.Tags,
				"auth_user": g.Users,
				"ip_cidr":   g.IPs,
				"action":    "reject",
			})
		}
	}
	return rules
}

func xraySniffing() map[string]any {
	return map[string]any{
		"enabled":      true,
		"destOverride": []string{"http", "tls", "quic"},
		"routeOnly":    true,
	}
}
