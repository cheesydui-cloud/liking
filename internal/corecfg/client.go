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
	if err := db.DeleteClientsNotIn(d, u.ID, ids); err != nil {
		return nil, err
	}
	totals, _ := db.ServerTrafficTotals(d)
	servers := map[int64]struct{}{}
	for _, iid := range ids {
		in, err := db.GetInbound(d, iid)
		if err != nil {
			continue
		}
		if !UserFacing(in.Profile) {
			continue
		}
		servers[in.ServerID] = struct{}{}
		c, err := db.GetClient(d, in.ID, u.ID)
		if err == sql.ErrNoRows {
			c, err = newClient(u, in)
			if err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
		srv, _ := db.GetServer(d, in.ServerID)
		used := totals[in.ServerID]
		over := db.ServerOverQuota(srv, used.Up+used.Down)
		c.Enabled = ok && in.Enabled && !over
		c.Email = db.EmailFor(u.ID, in.ID)
		if err := db.UpsertClient(d, c); err != nil {
			return nil, err
		}
	}
	out := make([]int64, 0, len(servers))
	for id := range servers {
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
		c.Username = fmt.Sprintf("u%d", u.ID)
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
