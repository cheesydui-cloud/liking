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

	CapCores = "cores"
	CapProbe = "probe"
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
}

type ApplyConfig struct {
	Rev         string          `json:"rev"`
	Xray        json.RawMessage `json:"xray,omitempty"`
	Singbox     json.RawMessage `json:"singbox,omitempty"`
	Mita        json.RawMessage `json:"mita,omitempty"`
	XrayAPI     string          `json:"xray_api,omitempty"`
	SingboxAPI  string          `json:"singbox_api,omitempty"`
	SpeedLimits []SpeedLimit    `json:"speed_limits,omitempty"`
}

type ApplyAck struct {
	Rev   string   `json:"rev"`
	OK    bool     `json:"ok"`
	Error string   `json:"error,omitempty"`
	Cores []string `json:"cores,omitempty"`
}

type Stats struct {
	Samples      []Sample `json:"samples"`
	NetUp        int64    `json:"net_up_bps,omitempty"`
	NetDown      int64    `json:"net_down_bps,omitempty"`
	HasNet       bool     `json:"has_net,omitempty"`
	Cores        []string `json:"cores,omitempty"`
	CoresRunning []string `json:"cores_running,omitempty"`
	DiskFree     int64    `json:"disk_free,omitempty"`
	DiskTotal    int64    `json:"disk_total,omitempty"`
	MemAvail     int64    `json:"mem_avail,omitempty"`
	MemTotal     int64    `json:"mem_total,omitempty"`
	LoadMilli    int64    `json:"load_milli,omitempty"`
	Conns        int      `json:"conns,omitempty"`
}

type Upgrade struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256,omitempty"`
	URL     string `json:"url"`
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

type Ping struct {
	TS int64 `json:"ts"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
