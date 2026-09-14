package server

import (
	"context"
	"log"
	"sync"
	"time"

	"liking/internal/certs"
	"liking/internal/corecfg"
	"liking/internal/db"
)

func (s *Server) lockServer(id int64) *sync.Mutex {
	v, _ := s.pushMu.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (s *Server) pushServer(id int64) error {
	mu := s.lockServer(id)
	mu.Lock()
	defer mu.Unlock()

	b, err := corecfg.Build(s.DB, id)
	if err != nil {
		_ = db.SetServerApplyError(s.DB, id, err)
		return err
	}
	if !s.Hub.IsOnline(id) {
		_ = db.SetServerRev(s.DB, id, b.Rev)
		return nil
	}
	if err := s.Hub.SendApply(id, b.Apply); err != nil {
		_ = db.SetServerApplyError(s.DB, id, err)
		return err
	}
	_ = db.SetServerApplyError(s.DB, id, nil)
	return db.SetServerRev(s.DB, id, b.Rev)
}

func (s *Server) syncServers(ids ...int64) {
	seen := map[int64]struct{}{}
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		s.kickSync(id)
	}
}

func (s *Server) kickSync(id int64) {
	s.kickMu.Lock()
	s.kickWant[id] = true
	if s.kickRun[id] {
		s.kickMu.Unlock()
		return
	}
	s.kickRun[id] = true
	s.kickMu.Unlock()
	go s.syncLoop(id)
}

func (s *Server) syncLoop(id int64) {
	var last string
	for {
		s.kickMu.Lock()
		if !s.kickWant[id] {
			s.kickRun[id] = false
			s.kickMu.Unlock()
			return
		}
		s.kickWant[id] = false
		s.kickMu.Unlock()
		if err := s.pushServer(id); err != nil {
			msg := err.Error()
			if msg != last {
				log.Printf("sync server %d: %v", id, err)
				last = msg
			}
		} else {
			last = ""
		}
	}
}

func (s *Server) syncAll() {
	list, err := db.ListServers(s.DB)
	if err != nil {
		return
	}
	ids := make([]int64, 0, len(list))
	for _, x := range list {
		ids = append(ids, x.ID)
	}
	s.syncServers(ids...)
}

func (s *Server) provisionAndSyncUser(u *db.User) {
	ids, err := corecfg.ProvisionUser(s.DB, u)
	if err != nil {
		log.Printf("provision user %d: %v", u.ID, err)
		return
	}
	s.syncServers(ids...)
}

func (s *Server) enforceLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.enforceOnce()
		}
	}
}

func (s *Server) enforceOnce() {
	users, err := db.ListUsers(s.DB)
	if err != nil {
		return
	}
	touched := map[int64]struct{}{}
	nowT := db.ClockNow(s.DB)
	now := nowT.Unix()
	for _, u := range users {
		if u.Role == "admin" {
			// Admin clients are provisioned on inbound changes and 用户页, not every 30s.
			continue
		}
		var pkg *db.Package
		if u.PackageID != nil {
			pkg, _ = db.GetPackage(s.DB, *u.PackageID)
		}
		if shouldResetAt(u, pkg, nowT) {
			_ = db.ResetUserTraffic(s.DB, u.ID)
			u.UsedUp, u.UsedDown = 0, 0
			u.CycleStart = now
		}
		ids, err := corecfg.ProvisionUser(s.DB, u)
		if err != nil {
			continue
		}
		for _, id := range ids {
			touched[id] = struct{}{}
		}
	}
	var ids []int64
	for id := range touched {
		ids = append(ids, id)
	}
	s.syncServers(ids...)
	_ = db.DeleteExpiredSessions(s.DB)
}

func (s *Server) certRenewLoop() {
	timer := time.NewTimer(90 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-timer.C:
			s.renewDueCerts()
		case <-ticker.C:
			s.renewDueCerts()
		}
	}
}

func (s *Server) renewDueCerts() {
	ids, err := db.ListCertIDsDueRenew(s.DB, time.Now().Add(30*24*time.Hour).Unix())
	if err != nil || len(ids) == 0 {
		return
	}
	changed := false
	for _, id := range ids {
		cur, err := db.GetCert(s.DB, id)
		if err != nil || cur.Source != "acme-cf" {
			continue
		}
		_, err = s.issueACME(context.Background(), id, cur.Name, certs.SplitNames(cur.Domains), cur.AcmeEmail)
		if err != nil {
			_ = db.SetCertError(s.DB, id, err.Error())
			log.Printf("renew cert %d: %v", id, err)
			continue
		}
		changed = true
	}
	if changed {
		s.syncAll()
	}
}

func shouldReset(u *db.User, pkg *db.Package, now int64) bool {
	return shouldResetAt(u, pkg, time.Unix(now, 0))
}

func shouldResetAt(u *db.User, pkg *db.Package, t time.Time) bool {
	if u == nil {
		return false
	}
	day := 0
	cycle := 0
	if pkg != nil {
		day = pkg.ResetDay
		cycle = pkg.CycleDays
	}
	if u.TrafficResetDay > 0 {
		day = u.TrafficResetDay
	}
	if day > 0 {
		if t.Day() != day {
			return false
		}
		if u.CycleStart == 0 {
			return true
		}
		prev := time.Unix(u.CycleStart, 0).In(t.Location())
		return prev.Year() != t.Year() || prev.Month() != t.Month()
	}
	if cycle > 0 && u.CycleStart > 0 {
		return t.Unix() >= u.CycleStart+int64(cycle)*86400
	}
	return false
}
