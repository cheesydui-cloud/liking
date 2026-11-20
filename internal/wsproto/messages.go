// Package wsproto is the JSON envelope between liking-server and liking-agent.
package wsproto

import "encoding/json"

const (
	TypeHello         = "hello"
	TypeHelloAck      = "hello_ack"
	TypeApply         = "apply_config"
	TypeApplyAck      = "apply_ack"
	TypeStats         = "stats"
	TypePing          = "ping"
	TypePong          = "pong"
	TypeError         = "error"
	TypeUpgrade       = "upgrade"
	TypeUpgradeAck    = "upgrade_ack"
	TypeUninstall     = "uninstall"
	TypeUninstallAck  = "uninstall_ack"
	TypeEnsureCore    = "ensure_core"
	TypeEnsureCoreAck = "ensure_core_ack"
	TypeRemoveCore    = "remove_core"
	TypeRemoveCoreAck = "remove_core_ack"
	TypeProbe         = "probe"
	TypeProbeAck      = "probe_ack"
	TypeKick          = "kick"
	TypeKickAck       = "kick_ack"
	TypeEnsureNFT     = "ensure_nft"
	TypeEnsureNFTAck  = "ensure_nft_ack"
	TypeEnsureWG      = "ensure_wg"
	TypeEnsureWGAck   = "ensure_wg_ack"
	TypeRemoveWG      = "remove_wg"
	TypeRemoveWGAck   = "remove_wg_ack"
	TypeAllowPort     = "allow_port"
	TypeAllowPortAck  = "allow_port_ack"
	TypeSyncTime      = "sync_time"
	TypeSyncTimeAck   = "sync_time_ack"
	TypeTuneBBR       = "tune_bbr"
	TypeTuneBBRAck    = "tune_bbr_ack"

	CapCores     = "cores"
	CapProbe     = "probe"
	CapKick      = "kick"
	CapNFT       = "nft"
	CapWG        = "wireguard"
	CapWGRemove  = "remove_wg"
	CapAllowPort = "allow_port"
	CapForward   = "forward"
	CapTime      = "time"
	CapBBR       = "bbr"
)

type Envelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Hello struct {
	Token        string   `json:"token"`
	AgentVersion string   `json:"agent_version"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	LastRev      string   `json:"last_rev,omitempty"`
	Cores        []string `json:"cores,omitempty"`
	Caps         []string `json:"caps,omitempty"`
}

type HelloAck struct {
	ServerID int64  `json:"server_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Error    string `json:"error,omitempty"`
}

type SpeedLimit struct {
	Mark uint32 `json:"mark"`
	Mbps int64  `json:"mbps"`
	// KBps is exact kilobytes/second. Agents that only read Mbps
	// treat a limit under 1 Mbps as 1 Mbps.
	KBps int64 `json:"kbps,omitempty"`
}

type RejectCN struct {
	Ports []int    `json:"ports"`
	Allow []string `json:"allow,omitempty"`
}

type AcctMark struct {
	Mark  uint32 `json:"mark"`
	Email string `json:"email"`
}

// ForwardRule is one listen port on this machine. TCP is relayed in
// userspace. UDP is DNATed by nftables. Old agents ignore the field.
type ForwardRule struct {
	ID       int64  `json:"id"`
	Port     int    `json:"port"`
	DestHost string `json:"dest_host"`
	DestPort int    `json:"dest_port"`
	Proto    string `json:"proto"`
}

type ApplyConfig struct {
	Rev         string          `json:"rev"`
	Xray        json.RawMessage `json:"xray,omitempty"`
	Singbox     json.RawMessage `json:"singbox,omitempty"`
	Mita        json.RawMessage `json:"mita,omitempty"`
	WireGuard   json.RawMessage `json:"wireguard,omitempty"`
	XrayAPI     string          `json:"xray_api,omitempty"`
	SingboxAPI  string          `json:"singbox_api,omitempty"`
	SpeedLimits []SpeedLimit    `json:"speed_limits,omitempty"`
	AcctMarks   []AcctMark      `json:"acct_marks,omitempty"`
	DisableIPv6 bool            `json:"disable_ipv6"`
	RejectCN    RejectCN        `json:"reject_cn"`
	// Forwards is always sent by a panel that knows traffic forwards, including
	// an empty list, so the agent can clear stale relays. omitempty would hide
	// that clear. A missing field (old panel) must not be treated as empty.
	Forwards []ForwardRule `json:"forwards"`
}

type WireGuardPeer struct {
	PublicKey string `json:"public_key"`
	Email     string `json:"email,omitempty"`
}

// WireGuardTunnel is one interface on the entry. Each home gets its own
// interface, fwmark, and route table. An old agent ignores this field and
// still applies Iface/Conf/Peers, which must stay equal to Tunnels[0].
type WireGuardTunnel struct {
	Iface string          `json:"iface"`
	Conf  string          `json:"conf,omitempty"`
	Peers []WireGuardPeer `json:"peers,omitempty"`
}

type WireGuardConfig struct {
	Iface   string            `json:"iface"`
	Conf    string            `json:"conf,omitempty"`
	Peers   []WireGuardPeer   `json:"peers,omitempty"`
	Tunnels []WireGuardTunnel `json:"tunnels,omitempty"`
}

type ApplyAck struct {
	Rev   string   `json:"rev"`
	OK    bool     `json:"ok"`
	Error string   `json:"error,omitempty"`
	Cores []string `json:"cores,omitempty"`
}

type Stats struct {
	Samples      []Sample     `json:"samples"`
	NetUp        int64        `json:"net_up_bps,omitempty"`
	NetDown      int64        `json:"net_down_bps,omitempty"`
	HasNet       bool         `json:"has_net,omitempty"`
	Cores        []string     `json:"cores,omitempty"`
	CoresRunning []string     `json:"cores_running,omitempty"`
	DiskFree     int64        `json:"disk_free,omitempty"`
	DiskTotal    int64        `json:"disk_total,omitempty"`
	MemAvail     int64        `json:"mem_avail,omitempty"`
	MemTotal     int64        `json:"mem_total,omitempty"`
	LoadMilli    int64        `json:"load_milli,omitempty"`
	Conns        int          `json:"conns,omitempty"`
	Ports        []PortRate   `json:"ports,omitempty"`
	HasPorts     bool         `json:"has_ports,omitempty"`
	WGPeers      []WGPeerStat `json:"wg_peers,omitempty"`
	HasWGPeers   bool         `json:"has_wg_peers,omitempty"`
	// ClockSkewMS is local time minus the reference clock. Negative means
	// the machine is slow. SS2022 rejects a difference over 30 seconds.
	HasClock    bool  `json:"has_clock,omitempty"`
	ClockSkewMS int64 `json:"clock_skew_ms,omitempty"`
	NTPSynced   bool  `json:"ntp_synced,omitempty"`
	// HasTCP reports the live congestion control and qdisc. It is display-only.
	HasTCP     bool   `json:"has_tcp,omitempty"`
	Congestion string `json:"congestion,omitempty"`
	Qdisc      string `json:"qdisc,omitempty"`
}

// PortRate is one listen port's realtime speed. It is display-only:
// the panel must not bill it as a user's traffic.
type PortRate struct {
	Port    int   `json:"port"`
	UpBps   int64 `json:"up_bps"`
	DownBps int64 `json:"down_bps"`
}

// WGPeerStat is one kernel WireGuard peer's cumulative transfer.
// Rx is bytes received from the peer (client upload). Tx is bytes sent to
// the peer (client download). The agent reports every peer, including the
// exit and the probe. The panel decides who is billed.
type WGPeerStat struct {
	PublicKey  string `json:"public_key"`
	AllowedIPs string `json:"allowed_ips,omitempty"`
	Email      string `json:"email,omitempty"`
	Rx         int64  `json:"rx"`
	Tx         int64  `json:"tx"`
}

type Upgrade struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256,omitempty"`
	URL     string `json:"url"`
	// FallbackURL is a GitHub release asset. Agents that cannot fetch URL
	// try it, through the local GitHub mirror when one is configured.
	// Older agents ignore the field.
	FallbackURL string `json:"fallback_url,omitempty"`
}

type UpgradeAck struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Version string `json:"version,omitempty"`
}

type UninstallAck struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type CoreOp struct {
	Core string `json:"core"`
}

type CoreOpAck struct {
	OK    bool     `json:"ok"`
	Error string   `json:"error,omitempty"`
	Core  string   `json:"core,omitempty"`
	Cores []string `json:"cores,omitempty"`
}

type Sample struct {
	Email string `json:"email"`
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
}

type Probe struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type ProbeAck struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

type Kick struct {
	Emails []string `json:"emails"`
}

type KickAck struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Closed int    `json:"closed"`
}

type EnsureNFTAck struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Have  bool   `json:"have"`
}

type EnsureWGAck struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Have  bool   `json:"have"`
}

type RemoveWGAck struct {
	OK    bool     `json:"ok"`
	Error string   `json:"error,omitempty"`
	Have  bool     `json:"have"`
	Cores []string `json:"cores"`
}

type AllowPort struct {
	Port  int    `json:"port"`
	Proto string `json:"proto"`
}

type AllowPortAck struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Port  int    `json:"port,omitempty"`
	How   string `json:"how,omitempty"`
}

type SyncTimeAck struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	SkewMS  int64  `json:"skew_ms"`
	AfterMS int64  `json:"after_ms"`
	Stepped bool   `json:"stepped"`
	NTP     bool   `json:"ntp"`
}

type TuneBBR struct {
	Profile string `json:"profile"`
}

type TuneBBRAck struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	Congestion string `json:"congestion,omitempty"`
	Qdisc      string `json:"qdisc,omitempty"`
	Changed    bool   `json:"changed"`
	Assumed    bool   `json:"assumed,omitempty"`
}

type Ping struct {
	TS int64 `json:"ts"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
