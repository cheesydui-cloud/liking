package corecfg

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"liking/internal/db"
	"liking/internal/wsproto"
)

type Bundle struct {
	Apply wsproto.ApplyConfig
	Rev   string
}

func Build(d *sql.DB, serverID int64) (*Bundle, error) {
	srv, err := db.GetServer(d, serverID)
	if err != nil {
		return nil, err
	}
	ins, err := db.ListInboundsByServer(d, serverID)
	if err != nil {
		return nil, err
	}
	all, err := db.ListInbounds(d)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*db.Inbound{}
	for _, in := range all {
		byID[in.ID] = in
	}
	certs := map[int64]*db.Certificate{}
	clients := map[int64][]*db.Client{}
	for _, in := range ins {
		cs, err := db.ListClientsByInbound(d, in.ID)
		if err != nil {
			return nil, err
		}
		clients[in.ID] = cs
		if in.CertID != nil {
			if _, ok := certs[*in.CertID]; !ok {
				c, err := db.GetCert(d, *in.CertID)
				if err != nil {
					return nil, fmt.Errorf("证书 %d: %w", *in.CertID, err)
				}
				certs[*in.CertID] = c
			}
		}
	}
	// Landing inbounds on *other* servers still need to be in byID (already are).
	// Relay users live on the landing server's config, so when we build the
	// landing server we must see every chain entry pointing here — ListInbounds
	// already loaded them.

	speeds := loadUserSpeeds(d, clients)
	denies := loadUserSiteDenies(d, clients)
	apiPort := pickAPIPort(ins, XrayAPIPort)
	sbPort := pickAPIPort(ins, SingboxAPIPort, apiPort)
	xray, err := buildXray(ins, clients, certs, byID, apiPort, speeds, denies)
	if err != nil {
		return nil, err
	}
	sb, err := buildSingbox(ins, clients, certs, byID, sbPort, speeds, denies)
	if err != nil {
		return nil, err
	}
	mita, err := buildMita(ins, clients)
	if err != nil {
		return nil, err
	}

	b := &Bundle{}
	if xray != nil {
		raw, err := json.Marshal(xray)
		if err != nil {
			return nil, err
		}
		b.Apply.Xray = raw
		b.Apply.XrayAPI = fmt.Sprintf("127.0.0.1:%d", apiPort)
	}
	if sb != nil {
		raw, err := json.Marshal(sb)
		if err != nil {
			return nil, err
		}
		b.Apply.Singbox = raw
		b.Apply.SingboxAPI = fmt.Sprintf("127.0.0.1:%d", sbPort)
	}
	if mita != nil {
		raw, err := json.Marshal(mita)
		if err != nil {
			return nil, err
		}
		b.Apply.Mita = raw
	}
	b.Apply.SpeedLimits = collectSpeedLimits(ins, clients, speeds)
	b.Apply.DisableIPv6 = srv.DisableIPv6
	b.Apply.RejectCN = CollectRejectCN(d, serverID, all)
	sum := sha256.New()
	sum.Write(b.Apply.Xray)
	sum.Write(b.Apply.Singbox)
	sum.Write(b.Apply.Mita)
	if raw, err := json.Marshal(b.Apply.SpeedLimits); err == nil {
		sum.Write(raw)
	}
	if srv.DisableIPv6 {
		sum.Write([]byte("disable_ipv6=1"))
	} else {
		sum.Write([]byte("disable_ipv6=0"))
	}
	if raw, err := json.Marshal(b.Apply.RejectCN); err == nil {
		sum.Write(raw)
	}
	b.Rev = hex.EncodeToString(sum.Sum(nil))[:16]
	b.Apply.Rev = b.Rev
	return b, nil
}

func pickAPIPort(ins []*db.Inbound, start int, extra ...int) int {
	used := map[int]struct{}{}
	for _, in := range ins {
		used[in.Port] = struct{}{}
		used[SocksPort(in.ID)] = struct{}{}
	}
	for _, p := range extra {
		if p > 0 {
			used[p] = struct{}{}
		}
	}
	for p := start; p < start+100; p++ {
		if _, ok := used[p]; !ok {
			return p
		}
	}
	return start
}
