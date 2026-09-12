// Package wsproto is the JSON envelope between liking-server and liking-agent.
package wsproto

import "encoding/json"

const (
	TypeHello    = "hello"
	TypeHelloAck = "hello_ack"
	TypeApply    = "apply_config"
	TypeApplyAck = "apply_ack"
	TypeStats    = "stats"
	TypePing     = "ping"
	TypePong     = "pong"
	TypeError    = "error"
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
}

type HelloAck struct {
	ServerID int64  `json:"server_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Error    string `json:"error,omitempty"`
}

type ApplyConfig struct {
	Rev     string          `json:"rev"`
	Xray    json.RawMessage `json:"xray,omitempty"`
	Singbox json.RawMessage `json:"singbox,omitempty"`
	Mita    json.RawMessage `json:"mita,omitempty"`
	XrayAPI string          `json:"xray_api,omitempty"`
}

type ApplyAck struct {
	Rev   string `json:"rev"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type Stats struct {
	Samples []Sample `json:"samples"`
	NetUp   int64    `json:"net_up_bps,omitempty"`
	NetDown int64    `json:"net_down_bps,omitempty"`
	Cores   []string `json:"cores,omitempty"`
}

type Sample struct {
	Email string `json:"email"`
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
}

type Ping struct {
	TS int64 `json:"ts"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
