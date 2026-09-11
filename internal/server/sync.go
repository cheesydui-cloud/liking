package server

import (
	"log"
	"sync"
	"time"

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
	now := time.Now().Unix()
	for _, u := range users {
		if u.Role == "admin" {
			continue
		}
		var pkg *db.Package
		if u.PackageID != nil {
			pkg, _ = db.GetPackage(s.DB, *u.PackageID)
		}
		if pkg != nil && shouldReset(u, pkg, now) {
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

func shouldReset(u *db.User, pkg *db.Package, now int64) bool {
	if pkg == nil {
		return false
	}
	if pkg.ResetDay > 0 {
		t := time.Unix(now, 0)
		if t.Day() != pkg.ResetDay {
			return false
		}
		if u.CycleStart == 0 {
			return true
		}
		prev := time.Unix(u.CycleStart, 0)
		return prev.Year() != t.Year() || prev.Month() != t.Month()
	}
	if pkg.CycleDays > 0 && u.CycleStart > 0 {
		return now >= u.CycleStart+int64(pkg.CycleDays)*86400
	}
	return false
}
