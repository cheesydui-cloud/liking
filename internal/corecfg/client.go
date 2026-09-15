package corecfg

import (
	"database/sql"
	"fmt"
	"strings"

	"liking/internal/db"
)

func ProvisionUser(d *sql.DB, u *db.User) ([]int64, error) {
	var pkg *db.Package
	if u.PackageID != nil {
		p, err := db.GetPackage(d, *u.PackageID)
		if err == nil {
			pkg = p
		}
	}
	ok := db.UserAccessOK(u, pkg)
	ids, err := db.InboundIDsForUser(d, u)
	if err != nil {
		return nil, err
	}
	existing, err := db.ListClientsByUser(d, u.ID)
	if err != nil {
		return nil, err
	}
	keep := map[int64]struct{}{}
	for _, id := range ids {
		keep[id] = struct{}{}
	}
	touched := map[int64]struct{}{}
	for _, c := range existing {
		if c == nil {
			continue
		}
		if _, ok := keep[c.InboundID]; ok {
			continue
		}
		if in, err := db.GetInbound(d, c.InboundID); err == nil && in != nil {
			touched[in.ServerID] = struct{}{}
		}
	}
	if err := db.DeleteClientsNotIn(d, u.ID, ids); err != nil {
		return nil, err
	}
	srvs, _ := db.ListServers(d)
	totals, _ := db.ServerQuotaTotals(d, srvs, db.ClockNow(d))
	for _, iid := range ids {
		in, err := db.GetInbound(d, iid)
		if err != nil {
			continue
		}
		if !UserFacing(in.Profile) {
			continue
		}
		created := false
		c, err := db.GetClient(d, in.ID, u.ID)
		if err == sql.ErrNoRows {
			c, err = newClient(u, in)
			if err != nil {
				return nil, err
			}
			created = true
		} else if err != nil {
			return nil, err
		}
		srv, _ := db.GetServer(d, in.ServerID)
		used := totals[in.ServerID]
		over := db.ServerOverQuota(srv, used.Up+used.Down)
		wantEnabled := ok && in.Enabled && !over
		wantEmail := db.EmailFor(u.ID, in.ID)
		wantUser := c.Username
		if in.Profile == ProfileMieru {
			wantUser = wantEmail
		}
		if !created && c.Enabled == wantEnabled && c.Email == wantEmail && c.Username == wantUser {
			continue
		}
		c.Enabled = wantEnabled
		c.Email = wantEmail
		if in.Profile == ProfileMieru {
			c.Username = wantUser
		}
		if err := db.UpsertClient(d, c); err != nil {
			return nil, err
		}
		touched[in.ServerID] = struct{}{}
	}
	out := make([]int64, 0, len(touched))
	for id := range touched {
		out = append(out, id)
	}
	return out, nil
}

func newClient(u *db.User, in *db.Inbound) (*db.Client, error) {
	if in != nil && !UserFacing(in.Profile) {
		return nil, fmt.Errorf("该入站没有用户")
	}
	c := &db.Client{
		InboundID: in.ID,
		UserID:    u.ID,
		Email:     db.EmailFor(u.ID, in.ID),
		Enabled:   true,
	}
	st := ParseSettings(in.Settings)
	switch in.Profile {
	case ProfileMieru:
		c.Username = db.EmailFor(u.ID, in.ID)
		pw, err := RandomHex(8)
		if err != nil {
			return nil, err
		}
		c.Password = pw
	case ProfileSOCKS5:
		c.Username = db.EmailFor(u.ID, in.ID)
		pw, err := RandomHex(16)
		if err != nil {
			return nil, err
		}
		c.Password = pw
	case ProfileTrojanTLS, ProfileAnyTLS:
		pw, err := RandomHex(16)
		if err != nil {
			return nil, err
		}
		c.Password = pw
	case ProfileSS2022:
		pw, err := RandomBase64(SSKeyLen(st.String("method")))
		if err != nil {
			return nil, err
		}
		c.Password = pw
	default:
		id, err := NewUUID()
		if err != nil {
			return nil, err
		}
		c.UUID = id
	}
	return c, nil
}

func ProvisionAll(d *sql.DB) ([]int64, error) {
	users, err := db.ListUsers(d)
	if err != nil {
		return nil, err
	}
	seen := map[int64]struct{}{}
	for _, u := range users {
		ids, err := ProvisionUser(d, u)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			seen[id] = struct{}{}
		}
	}
	out := make([]int64, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out, nil
}

func IsRelayEmail(email string) bool {
	return strings.HasPrefix(email, "relay.")
}
