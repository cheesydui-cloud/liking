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

const (
	SiteFilterDeny  = "deny"
	SiteFilterAllow = "allow"
)

// ClientHealthCheckURL is Clash / sing-box url-test. Not Google, so a Google
// deny still lets the node look alive. Allow-only always passes this host.
const ClientHealthCheckURL = "https://cp.cloudflare.com/"

var siteDenyCatalog = []struct {
	Name  string
	Label string
	Group string
}{
	{Name: "tiktok", Label: "TikTok", Group: "社交"},
	{Name: "facebook", Label: "Facebook", Group: "社交"},
	{Name: "instagram", Label: "Instagram", Group: "社交"},
	{Name: "twitter", Label: "Twitter / X", Group: "社交"},
	{Name: "telegram", Label: "Telegram", Group: "社交"},
	{Name: "discord", Label: "Discord", Group: "社交"},
	{Name: "whatsapp", Label: "WhatsApp", Group: "社交"},
	{Name: "line", Label: "LINE", Group: "社交"},
	{Name: "reddit", Label: "Reddit", Group: "社交"},
	{Name: "linkedin", Label: "LinkedIn", Group: "社交"},
	{Name: "pinterest", Label: "Pinterest", Group: "社交"},
	{Name: "snapchat", Label: "Snapchat", Group: "社交"},
	{Name: "threads", Label: "Threads", Group: "社交"},
	{Name: "weibo", Label: "微博", Group: "社交"},
	{Name: "xiaohongshu", Label: "小红书", Group: "社交"},
	{Name: "douyin", Label: "抖音", Group: "社交"},
	{Name: "youtube", Label: "油管", Group: "视频"},
	{Name: "netflix", Label: "Netflix", Group: "视频"},
	{Name: "twitch", Label: "Twitch", Group: "视频"},
	{Name: "bilibili", Label: "哔哩哔哩", Group: "视频"},
	{Name: "openai", Label: "ChatGPT", Group: "AI"},
	{Name: "claude", Label: "Claude", Group: "AI"},
	{Name: "gemini", Label: "Gemini", Group: "AI"},
	{Name: "grok", Label: "Grok", Group: "AI"},
	{Name: "perplexity", Label: "Perplexity", Group: "AI"},
	{Name: "deepseek", Label: "DeepSeek", Group: "AI"},
	{Name: "huggingface", Label: "Hugging Face", Group: "AI"},
	{Name: "midjourney", Label: "Midjourney", Group: "AI"},
	{Name: "characterai", Label: "Character.AI", Group: "AI"},
	{Name: "copilot", Label: "Copilot", Group: "AI"},
	{Name: "kimi", Label: "Kimi", Group: "AI"},
	{Name: "tongyi", Label: "通义千问", Group: "AI"},
	{Name: "doubao", Label: "豆包", Group: "AI"},
	{Name: "poe", Label: "Poe", Group: "AI"},
	{Name: "google", Label: "谷歌", Group: "工具"},
	{Name: "speedtest", Label: "测速", Group: "工具"},
	{Name: "iplookup", Label: "IP 查询", Group: "工具"},
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
		"musical.ly", "muscdn.com", "byteoversea.com", "ibyteimg.com",
		"ibytedtos.com", "isnssdk.com", "ttlivecdn.com", "tiktokrow-cdn.com",
	},
	"facebook": {
		"facebook.com", "fb.com", "fbcdn.net", "facebook.net", "messenger.com",
		"fbsbx.com", "fb.watch", "fbcdn.com",
	},
	"instagram": {
		"instagram.com", "cdninstagram.com", "ig.me",
	},
	"twitter": {
		"twitter.com", "x.com", "t.co", "twimg.com", "ads-twitter.com",
		"pscp.tv", "periscope.tv", "tweetdeck.com",
	},
	"telegram": {
		"telegram.org", "t.me", "telegram.me", "telegram.dog", "tdesktop.com",
		"telegra.ph", "telegram-cdn.org", "cdn-telegram.org",
	},
	"discord": {
		"discord.com", "discordapp.com", "discord.gg", "discord.media",
		"discordapp.net", "discordcdn.com", "discord.gift",
	},
	"netflix": {
		"netflix.com", "netflix.net", "nflxvideo.net", "nflximg.net",
		"nflxso.net", "nflxext.com",
	},
	"whatsapp": {
		"whatsapp.com", "whatsapp.net", "wa.me",
	},
	"line": {
		"line.me", "line-scdn.net", "line-apps.com",
	},
	"reddit": {
		"reddit.com", "redd.it", "redditstatic.com", "redditmedia.com",
		"reddituploads.com",
	},
	"linkedin": {
		"linkedin.com", "licdn.com", "lnkd.in",
	},
	"pinterest": {
		"pinterest.com", "pinimg.com", "pin.it",
	},
	"snapchat": {
		"snapchat.com", "snap.com", "sc-cdn.net", "snapkit.com",
	},
	"threads": {
		"threads.net", "threads.com",
	},
	"weibo": {
		"weibo.com", "weibo.cn", "weibocdn.com",
	},
	"xiaohongshu": {
		"xiaohongshu.com", "xhslink.com", "xhscdn.com",
	},
	"douyin": {
		"douyin.com", "iesdouyin.com", "douyinvod.com", "douyincdn.com",
		"amemv.com",
	},
	"twitch": {
		"twitch.tv", "twitchcdn.net", "jtvnw.net", "ttvnw.net", "ext-twitch.tv",
	},
	"bilibili": {
		"bilibili.com", "b23.tv", "hdslb.com", "bilivideo.com",
		"biliapi.net", "biliapi.com",
	},
	"openai": {
		"openai.com", "chatgpt.com", "chat.com", "oaistatic.com",
		"oaiusercontent.com",
	},
	"claude": {
		"anthropic.com", "claude.ai", "claude.com",
	},
	"gemini": {
		"gemini.google.com", "aistudio.google.com", "notebooklm.google.com",
		"deepmind.com", "deepmind.google", "ai.google.dev",
		"makersuite.google.com", "bard.google.com",
	},
	"grok": {
		"x.ai", "grok.com",
	},
	"perplexity": {
		"perplexity.ai", "pplx.ai",
	},
	"deepseek": {
		"deepseek.com",
	},
	"huggingface": {
		"huggingface.co", "hf.co",
	},
	"midjourney": {
		"midjourney.com",
	},
	"characterai": {
		"character.ai", "characterai.io",
	},
	"copilot": {
		"copilot.microsoft.com", "githubcopilot.com", "copilot.github.com",
	},
	"kimi": {
		"kimi.com", "moonshot.cn", "moonshot.ai",
	},
	"tongyi": {
		"tongyi.aliyun.com", "qianwen.aliyun.com", "dashscope.aliyun.com",
	},
	"doubao": {
		"doubao.com",
	},
	"poe": {
		"poe.com",
	},
	"speedtest": {
		"speedtest.net", "ookla.com", "fast.com", "speed.cloudflare.com",
		"measurementlab.net", "openspeedtest.com", "librespeed.org",
		"speedof.me", "testmy.net", "ping.pe", "speedtest.cn", "itdog.cn",
		"17ce.com",
	},
	"iplookup": {
		"ipinfo.io", "ipify.org", "icanhazip.com", "ifconfig.me", "ifconfig.co",
		"ip.sb", "ipapi.co", "ip-api.com", "ipwho.is", "ipwhois.io",
		"ident.me", "ipecho.net", "seeip.org", "myip.com", "whatismyip.com",
		"whatismyipaddress.com", "iplocation.net", "checkip.amazonaws.com",
		"cip.cc", "ip138.com", "ipip.net", "ip.skk.moe", "ping0.cc", "ipw.cn",
	},
}

var siteDenyIPs = map[string][]string{
	"google": {
		"8.8.8.8/32", "8.8.4.4/32",
		"2001:4860:4860::8888/128", "2001:4860:4860::8844/128",
	},
}

// Common resolvers so v2rayN / Clash still resolve when the user is allow-only.
var siteAllowDNS = []string{
	"1.1.1.1/32", "1.0.0.1/32",
	"8.8.8.8/32", "8.8.4.4/32",
	"9.9.9.9/32", "149.112.112.112/32",
	"223.5.5.5/32", "223.6.6.6/32",
	"119.29.29.29/32", "114.114.114.114/32",
	"2606:4700:4700::1111/128", "2606:4700:4700::1001/128",
	"2001:4860:4860::8888/128", "2001:4860:4860::8844/128",
}

type SiteDeny struct {
	Mode       string
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
		out = append(out, map[string]string{"name": c.Name, "label": c.Label, "group": c.Group})
	}
	return out
}

func NormalizeSiteFilterMode(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "off", "none":
		return "", nil
	case SiteFilterDeny:
		return SiteFilterDeny, nil
	case SiteFilterAllow:
		return SiteFilterAllow, nil
	default:
		return "", fmt.Errorf("访问限制模式无效")
	}
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

func SiteFilterFromUser(u *db.User) SiteDeny {
	return siteDenyFromUser(u)
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
	mode, _ := NormalizeSiteFilterMode(u.SiteFilterMode)
	if d.Empty() {
		d.Mode = ""
		return d
	}
	if mode == "" {
		mode = SiteFilterDeny
	}
	d.Mode = mode
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

func clientHealthHost() string {
	u, err := url.Parse(ClientHealthCheckURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(u.Hostname()))
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
	if s.Mode == SiteFilterAllow {
		if h := clientHealthHost(); h != "" {
			addD([]string{h})
		}
		addI(siteAllowDNS)
	}
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
			if sd.Empty() || sd.Mode == "" {
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

func userOutboundTag(in *db.Inbound, c *db.Client, speeds map[int64]int64) string {
	if in != nil && in.LineKind == "chain" {
		return outboundTag(in.ID)
	}
	if in != nil && directThrottle(in) {
		if mark, ok := clientSpeedMark(c, speeds); ok {
			return limitTag(mark)
		}
	}
	return "direct"
}

type siteDenyGroup struct {
	Tags    []string
	Users   []string
	Domains []string
	IPs     []string
}

type siteAllowPassGroup struct {
	Tags     []string
	Users    []string
	Domains  []string
	IPs      []string
	Outbound string
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
			if d.Mode == SiteFilterAllow {
				continue
			}
			if d.Mode != SiteFilterDeny && d.Mode != "" {
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

func collectSiteAllowGroups(inbounds []*db.Inbound, clients map[int64][]*db.Client, denies map[int64]SiteDeny, speeds map[int64]int64, core string) (pass []siteAllowPassGroup, block []siteDenyGroup) {
	type acc struct {
		tags     []string
		users    []string
		seenT    map[string]bool
		seenU    map[string]bool
		domains  []string
		ips      []string
		outbound string
	}
	byKey := map[string]*acc{}
	var order []string
	blockAcc := &acc{seenT: map[string]bool{}, seenU: map[string]bool{}}
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
			if !ok || d.Empty() || d.Mode != SiteFilterAllow {
				continue
			}
			ob := userOutboundTag(in, c, speeds)
			if ob == "" {
				continue
			}
			key := d.key() + "\x00" + ob
			a := byKey[key]
			if a == nil {
				doms, ips := ResolveSiteDeny(d)
				if len(doms) == 0 && len(ips) == 0 {
					continue
				}
				a = &acc{seenT: map[string]bool{}, seenU: map[string]bool{}, domains: doms, ips: ips, outbound: ob}
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
			if !blockAcc.seenT[tag] {
				blockAcc.seenT[tag] = true
				blockAcc.tags = append(blockAcc.tags, tag)
			}
			if !blockAcc.seenU[user] {
				blockAcc.seenU[user] = true
				blockAcc.users = append(blockAcc.users, user)
			}
		}
	}
	pass = make([]siteAllowPassGroup, 0, len(order))
	for _, k := range order {
		a := byKey[k]
		pass = append(pass, siteAllowPassGroup{
			Tags: a.tags, Users: a.users, Domains: a.domains, IPs: a.ips, Outbound: a.outbound,
		})
	}
	if len(blockAcc.users) > 0 && len(blockAcc.tags) > 0 {
		block = []siteDenyGroup{{Tags: blockAcc.tags, Users: blockAcc.users}}
	}
	return pass, block
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

func xraySiteAllowPassRules(groups []siteAllowPassGroup) []any {
	var rules []any
	for _, g := range groups {
		if len(g.Users) == 0 || len(g.Tags) == 0 || g.Outbound == "" {
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
				"outboundTag": g.Outbound,
			})
		}
		if len(g.IPs) > 0 {
			rules = append(rules, map[string]any{
				"type":        "field",
				"inboundTag":  g.Tags,
				"user":        g.Users,
				"ip":          g.IPs,
				"outboundTag": g.Outbound,
			})
		}
	}
	return rules
}

func xraySiteAllowBlockRules(groups []siteDenyGroup) []any {
	var rules []any
	for _, g := range groups {
		if len(g.Users) == 0 || len(g.Tags) == 0 {
			continue
		}
		rules = append(rules, map[string]any{
			"type":        "field",
			"inboundTag":  g.Tags,
			"user":        g.Users,
			"outboundTag": "block",
		})
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

func singSiteAllowPassRules(groups []siteAllowPassGroup) []any {
	var rules []any
	for _, g := range groups {
		if len(g.Users) == 0 || len(g.Tags) == 0 || g.Outbound == "" {
			continue
		}
		if len(g.Domains) > 0 {
			rules = append(rules, map[string]any{
				"inbound":       g.Tags,
				"auth_user":     g.Users,
				"domain_suffix": g.Domains,
				"outbound":      g.Outbound,
			})
		}
		if len(g.IPs) > 0 {
			rules = append(rules, map[string]any{
				"inbound":   g.Tags,
				"auth_user": g.Users,
				"ip_cidr":   g.IPs,
				"outbound":  g.Outbound,
			})
		}
	}
	return rules
}

func singSiteAllowBlockRules(groups []siteDenyGroup) []any {
	var rules []any
	for _, g := range groups {
		if len(g.Users) == 0 || len(g.Tags) == 0 {
			continue
		}
		rules = append(rules, map[string]any{
			"inbound":   g.Tags,
			"auth_user": g.Users,
			"action":    "reject",
		})
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
