package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"liking/internal/wsproto"
)

func TestNftMbpsToKBps(t *testing.T) {
	if nftMbpsToKBps(0) != 0 || nftMbpsToKBps(-1) != 0 {
		t.Fatal("zero")
	}
	if nftMbpsToKBps(50) != 6250 {
		t.Fatalf("50 Mbps -> %d", nftMbpsToKBps(50))
	}
	if nftMbpsToKBps(1) != 125 {
		t.Fatalf("1 Mbps -> %d", nftMbpsToKBps(1))
	}
}

func TestNftSpeedTable(t *testing.T) {
	if nftSpeedTable(nil) != "" {
		t.Fatal("empty")
	}
	if nftSpeedTable([]wsproto.SpeedLimit{{Mark: 0, Mbps: 50}, {Mark: 1, Mbps: 0}}) != "" {
		t.Fatal("invalid")
	}
	mark := uint32(0x4C4B0001)
	body := nftSpeedTable([]wsproto.SpeedLimit{
		{Mark: mark, Mbps: 50},
		{Mark: mark, Mbps: 50},
	})
	if !strings.Contains(body, "table inet liking_speed") {
		t.Fatalf("table %s", body)
	}
	if !strings.Contains(body, "meta mark 0x4c4b0001 ct mark set meta mark") {
		t.Fatalf("save %s", body)
	}
	if strings.Count(body, "limit rate over 6250 kbytes/second drop") != 2 {
		t.Fatalf("drop rules %s", body)
	}
	if !strings.Contains(body, "chain output") || !strings.Contains(body, "chain input") {
		t.Fatalf("chains %s", body)
	}
}

func TestApplyConfigSpeedLimitsIgnoredByOldAgent(t *testing.T) {
	raw, err := json.Marshal(wsproto.ApplyConfig{
		Rev:         "abc",
		SpeedLimits: []wsproto.SpeedLimit{{Mark: 0x4C4B0001, Mbps: 50}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"speed_limits"`) {
		t.Fatalf("marshal %s", raw)
	}
	var old struct {
		Rev  string          `json:"rev"`
		Xray json.RawMessage `json:"xray,omitempty"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if old.Rev != "abc" {
		t.Fatal(old.Rev)
	}
}
