package server

import (
	"net/http"
	"sort"
	"sync"

	"liking/internal/db"
)

type serverSyncRow struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Code   string `json:"code,omitempty"`
}

func (s *Server) handleSyncAllServers(w http.ResponseWriter, _ *http.Request) {
	list, err := db.ListServers(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reconcileOnline(list)
	rows := make([]serverSyncRow, len(list))
	var jobs []int
	for i, srv := range list {
		rows[i] = serverSyncRow{ID: srv.ID, Name: srv.Name, Status: "skipped"}
		if srv.Online != 1 {
			rows[i].Error = "离线"
			rows[i].Code = "offline"
			continue
		}
		jobs = append(jobs, i)
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, upgradeAllParallel)
	for _, i := range jobs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			srv := list[i]
			row := serverSyncRow{ID: srv.ID, Name: srv.Name, Status: "synced"}
			if err := s.pushServer(srv.ID); err != nil {
				row.Status = "failed"
				row.Error = err.Error()
			}
			rows[i] = row
		}(i)
	}
	wg.Wait()
	synced, failed, skipped := 0, 0, 0
	for _, row := range rows {
		switch row.Status {
		case "synced":
			synced++
		case "failed":
			failed++
		default:
			skipped++
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	jsonOK(w, map[string]any{
		"ok":      true,
		"synced":  synced,
		"failed":  failed,
		"skipped": skipped,
		"results": rows,
	})
}
