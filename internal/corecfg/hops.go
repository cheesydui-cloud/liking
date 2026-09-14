package corecfg

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"liking/internal/db"
)

// MaxHops is the landing plus intermediate hops on one chain entry.
const MaxHops = 5

// Hop is one intermediate step before the chain landing (exit_*).
type Hop struct {
	Kind          string
	InboundID     int64
	URI           string
	RelayUUID     string
	RelayUsername string
	RelayPassword string
}

func hopOutboundTag(entryID int64, i int) string {
	return fmt.Sprintf("ob-%d-h%d", entryID, i)
}

func hopsArray(st Settings) ([]any, error) {
	if st == nil {
		return nil, nil
	}
	raw, ok := st["hops"]
	if !ok || raw == nil {
		return nil, nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("hops 格式无效")
	}
	return arr, nil
}

// ParseHops reads intermediate hops. Invalid items are skipped.
func ParseHops(st Settings) []Hop {
	arr, err := hopsArray(st)
	if err != nil || len(arr) == 0 {
		return nil
	}
	var out []Hop
	for _, x := range arr {
		h, err := parseHop(x)
		if err != nil {
			continue
		}
		if h.Kind == "" && h.InboundID == 0 && h.URI == "" {
			continue
		}
		out = append(out, h)
	}
	return out
}

// NormalizeHops checks hop shape and rewrites a clean hops array.
func NormalizeHops(st Settings) error {
	arr, err := hopsArray(st)
	if err != nil {
		return err
	}
	if len(arr) == 0 {
		delete(st, "hops")
		return nil
	}
	if len(arr) > MaxHops-1 {
		return fmt.Errorf("最多 %d 跳", MaxHops)
	}
	cleaned := make([]any, 0, len(arr))
	for _, x := range arr {
		h, err := parseHop(x)
		if err != nil {
			return err
		}
		if err := h.validate(); err != nil {
			return err
		}
		cleaned = append(cleaned, h.Map())
	}
	st["hops"] = cleaned
	return nil
}

func (h Hop) validate() error {
	switch h.Kind {
	case "panel":
		if h.InboundID == 0 {
			return fmt.Errorf("请选择这一跳的节点")
		}
		if strings.TrimSpace(h.URI) != "" {
			return fmt.Errorf("每一跳只能是本面板节点或出口链接")
		}
	case "socks", "uri":
		if h.InboundID != 0 {
			return fmt.Errorf("每一跳只能是本面板节点或出口链接")
		}
		if _, err := ParseShareURI(h.URI); err != nil {
			return err
		}
	default:
		return fmt.Errorf("跳类型无效")
	}
	return nil
}

func parseHop(v any) (Hop, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return Hop{}, fmt.Errorf("hops 格式无效")
	}
	h := Hop{
		Kind:          strings.ToLower(strings.TrimSpace(hopString(m["kind"]))),
		InboundID:     hopInt64(m["inbound_id"]),
		URI:           strings.TrimSpace(hopString(m["uri"])),
		RelayUUID:     hopString(m["relay_uuid"]),
		RelayUsername: hopString(m["relay_username"]),
		RelayPassword: hopString(m["relay_password"]),
	}
	if h.Kind == "" {
		if h.URI != "" {
			h.Kind = "socks"
		} else {
			h.Kind = "panel"
		}
	}
	if h.Kind == "uri" {
		h.Kind = "socks"
	}
	return h, nil
}

// Map is the JSON object stored in settings.hops.
func (h Hop) Map() map[string]any {
	m := map[string]any{"kind": h.Kind}
	if h.Kind == "panel" && h.InboundID != 0 {
		m["inbound_id"] = h.InboundID
	}
	if h.Kind == "socks" && h.URI != "" {
		m["uri"] = h.URI
	}
	if h.RelayUUID != "" {
		m["relay_uuid"] = h.RelayUUID
	}
	if h.RelayUsername != "" {
		m["relay_username"] = h.RelayUsername
	}
	if h.RelayPassword != "" {
		m["relay_password"] = h.RelayPassword
	}
	return m
}

func (h Hop) cred() Settings {
	st := Settings{}
	if h.RelayUUID != "" {
		st["relay_uuid"] = h.RelayUUID
	}
	if h.RelayUsername != "" {
		st["relay_username"] = h.RelayUsername
	}
	if h.RelayPassword != "" {
		st["relay_password"] = h.RelayPassword
	}
	return st
}

// ApplyHopRelays mints per-hop relay secrets and rejects cycles / non-landings.
func ApplyHopRelays(st Settings, lands map[int64]*db.Inbound, entryID, exitID int64) error {
	arr, err := hopsArray(st)
	if err != nil {
		return err
	}
	if len(arr) == 0 {
		delete(st, "hops")
		return nil
	}
	if len(arr) > MaxHops-1 {
		return fmt.Errorf("最多 %d 跳", MaxHops)
	}
	seen := map[int64]struct{}{}
	if entryID != 0 {
		seen[entryID] = struct{}{}
	}
	if exitID != 0 {
		seen[exitID] = struct{}{}
	}
	out := make([]any, 0, len(arr))
	for _, x := range arr {
		h, err := parseHop(x)
		if err != nil {
			return err
		}
		if err := h.validate(); err != nil {
			return err
		}
		if h.Kind == "socks" {
			out = append(out, h.Map())
			continue
		}
		land := lands[h.InboundID]
		if land == nil {
			return fmt.Errorf("跳点不存在")
		}
		if _, ok := seen[land.ID]; ok {
			return fmt.Errorf("路径里不能重复同一节点")
		}
		if land.LineKind == "chain" {
			return fmt.Errorf("跳必须是直出线路")
		}
		if !CanLand(land.Profile) {
			return fmt.Errorf("v1 链式落地仅支持 VLESS / Trojan / SS2022 / SOCKS5（Mieru / AnyTLS 不能当落地）")
		}
		seen[land.ID] = struct{}{}
		cred := h.cred()
		if err := ensureRelay(cred, land.Profile); err != nil {
			return err
		}
		h.RelayUUID = cred.String("relay_uuid")
		h.RelayUsername = cred.String("relay_username")
		h.RelayPassword = cred.String("relay_password")
		out = append(out, h.Map())
	}
	st["hops"] = out
	return nil
}

// HopInboundIDs returns panel hop inbound ids (not the landing).
func HopInboundIDs(in *db.Inbound) []int64 {
	if in == nil {
		return nil
	}
	var ids []int64
	for _, h := range ParseHops(ParseSettings(in.Settings)) {
		if h.InboundID != 0 {
			ids = append(ids, h.InboundID)
		}
	}
	return ids
}

// CountHopsTo counts chain entries that use landingID as an intermediate hop.
func CountHopsTo(list []*db.Inbound, landingID int64) int {
	if landingID == 0 {
		return 0
	}
	n := 0
	for _, in := range list {
		if in == nil {
			continue
		}
		for _, h := range ParseHops(ParseSettings(in.Settings)) {
			if h.InboundID == landingID {
				n++
			}
		}
	}
	return n
}

func forEachRelayTo(landingID int64, byID map[int64]*db.Inbound, fn func(email string, st Settings)) {
	if landingID == 0 || fn == nil {
		return
	}
	for _, in := range byID {
		if in == nil || !in.Enabled {
			continue
		}
		if in.ExitInboundID != nil && *in.ExitInboundID == landingID {
			fn(fmt.Sprintf("relay.i%d", in.ID), ParseSettings(in.Settings))
		}
		for i, h := range ParseHops(ParseSettings(in.Settings)) {
			if h.InboundID != landingID {
				continue
			}
			fn(fmt.Sprintf("relay.i%d.h%d", in.ID, i), h.cred())
		}
	}
}

// PathHop is one outbound on the entry machine (dialerProxy / detour chain).
type PathHop struct {
	Tag   string
	Land  *db.Inbound
	Socks *SocksTarget
	Share *ShareTarget
	Cred  Settings
}

// ChainPath is intermediate hops plus the landing, in dial order.
func ChainPath(entry *db.Inbound, byID map[int64]*db.Inbound) ([]PathHop, error) {
	if entry == nil {
		return nil, fmt.Errorf("链式线路不存在")
	}
	st := ParseSettings(entry.Settings)
	hops := ParseHops(st)
	if len(hops)+1 > MaxHops {
		return nil, fmt.Errorf("最多 %d 跳", MaxHops)
	}
	var out []PathHop
	for i, h := range hops {
		ph, err := hopToPath(h, byID)
		if err != nil {
			return nil, fmt.Errorf("链式线路 %s 第 %d 跳: %w", entry.Name, i+1, err)
		}
		ph.Tag = hopOutboundTag(entry.ID, i)
		out = append(out, ph)
	}
	last := PathHop{Tag: outboundTag(entry.ID), Cred: Settings{
		"relay_uuid":     st.String("relay_uuid"),
		"relay_username": st.String("relay_username"),
		"relay_password": st.String("relay_password"),
	}}
	if t, err := shareExit(entry); err != nil {
		return nil, err
	} else if t != nil {
		last.Share = t
		last.Socks = t.SocksTarget()
	} else if entry.ExitInboundID == nil {
		return nil, fmt.Errorf("链式线路 %s 没有落地", entry.Name)
	} else {
		land := byID[*entry.ExitInboundID]
		if land == nil {
			return nil, fmt.Errorf("链式线路 %s 的落地不存在", entry.Name)
		}
		last.Land = land
	}
	out = append(out, last)
	return out, nil
}

func hopToPath(h Hop, byID map[int64]*db.Inbound) (PathHop, error) {
	if h.Kind == "socks" || h.Kind == "uri" || h.URI != "" {
		t, err := ParseShareURI(h.URI)
		if err != nil {
			return PathHop{}, err
		}
		return PathHop{Share: t, Socks: t.SocksTarget()}, nil
	}
	if h.InboundID == 0 {
		return PathHop{}, fmt.Errorf("没有节点")
	}
	land := byID[h.InboundID]
	if land == nil {
		return PathHop{}, fmt.Errorf("节点不存在")
	}
	return PathHop{Land: land, Cred: h.cred()}, nil
}

func hopString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func hopInt64(v any) int64 {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return 0
		}
		return n
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}
