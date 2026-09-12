package server

import (
	"database/sql"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"liking/internal/db"
	"liking/internal/installscript"
	"liking/internal/version"
)

type Server struct {
	DB           *sql.DB
	Hub          *Hub
	loginLimiter *loginLimiter
	stop         chan struct{}
	stopOnce     sync.Once
	pushMu       sync.Map // int64 -> *sync.Mutex
	kickMu       sync.Mutex
	kickWant     map[int64]bool
	kickRun      map[int64]bool
	acmeMu       sync.Mutex
	backupMu     sync.Mutex
	CFAPI        string // test override for Cloudflare API base URL
}

func New(d *sql.DB) (*Server, error) {
	if err := db.MarkAllServersOffline(d); err != nil {
		log.Printf("server: MarkAllServersOffline: %v", err)
	}
	hub := NewHub(d)
	s := &Server{
		DB:           d,
		Hub:          hub,
		loginLimiter: newLoginLimiter(),
		stop:         make(chan struct{}),
		kickWant:     map[int64]bool{},
		kickRun:      map[int64]bool{},
	}
	hub.OnTrafficUpdate = func(userID int64) {
		u, err := db.GetUser(d, userID)
		if err != nil {
			return
		}
		s.provisionAndSyncUser(u)
	}
	hub.Redispatch = func(ids []int64) { s.syncServers(ids...) }
	go s.enforceLoop()
	go s.certRenewLoop()
	return s, nil
}

func (s *Server) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.Hub.Close()
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{"ok": true, "version": version.Version})
	})
	r.Get("/v1/agents", s.Hub.ServeWS)
	r.Get("/v1/install-agent", s.handleInstallAgent)
	r.Get("/v1/agent-bin", s.handleAgentBin)
	r.Head("/v1/agent-bin", s.handleAgentBin)

	r.Get("/api/branding", s.handleBranding)
	r.Post("/api/login", s.handleLogin)
	r.Get("/api/sub/{token}", s.handleSub)
	r.Get("/api/sub/{token}/{fmt}", s.handleSub)

	r.Group(func(r chi.Router) {
		r.Use(s.csrf)
		r.Use(s.requireAPIAuth)
		r.Post("/api/logout", s.handleLogout)
		r.Get("/api/me", s.handleMe)
		r.Put("/api/me", s.handleProfile)
		r.Post("/api/password", s.handlePassword)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAdmin)
			r.Get("/api/dashboard", s.handleDashboard)
			r.Get("/api/profiles", s.handleProfiles)

			r.Get("/api/servers", s.handleListServers)
			r.Post("/api/servers", s.handleCreateServer)
			r.Put("/api/servers/{id}", s.handleUpdateServer)
			r.Delete("/api/servers/{id}", s.handleDeleteServer)
			r.Post("/api/servers/{id}/sync", s.handleSyncServer)
			r.Get("/api/servers/{id}/install", s.handleServerInstall)
			r.Get("/api/servers/{id}/cf-domains", s.handleServerCFDomains)
			r.Get("/api/cf-domains", s.handleCFDomains)

			r.Get("/api/inbounds", s.handleListInbounds)
			r.Post("/api/inbounds", s.handleCreateInbound)
			r.Get("/api/inbounds/{id}/share", s.handleInboundShare)
			r.Put("/api/inbounds/{id}", s.handleUpdateInbound)
			r.Delete("/api/inbounds/{id}", s.handleDeleteInbound)

			r.Get("/api/users", s.handleListUsers)
			r.Post("/api/users", s.handleCreateUser)
			r.Put("/api/users/{id}", s.handleUpdateUser)
			r.Delete("/api/users/{id}", s.handleDeleteUser)
			r.Post("/api/users/{id}/reset-traffic", s.handleResetTraffic)
			r.Post("/api/users/{id}/rotate-sub", s.handleRotateSub)
			r.Post("/api/users/{id}/password", s.handleSetUserPassword)

			r.Get("/api/packages", s.handleListPackages)
			r.Post("/api/packages", s.handleCreatePackage)
			r.Put("/api/packages/{id}", s.handleUpdatePackage)
			r.Delete("/api/packages/{id}", s.handleDeletePackage)

			r.Get("/api/certs", s.handleListCerts)
			r.Get("/api/certs/{id}", s.handleGetCert)
			r.Post("/api/certs", s.handleCreateCert)
			r.Post("/api/certs/selfsign", s.handleSelfSignCert)
			r.Post("/api/certs/acme", s.handleIssueACME)
			r.Post("/api/certs/{id}/renew", s.handleRenewCert)
			r.Put("/api/certs/{id}", s.handleUpdateCert)
			r.Delete("/api/certs/{id}", s.handleDeleteCert)

			r.Get("/api/settings", s.handleGetSettings)
			r.Put("/api/settings", s.handlePutSettings)
			r.Get("/api/backup", s.handleBackupDownload)
			r.Get("/api/backup/summary", s.handleBackupSummary)
			r.Post("/api/backup/preview", s.handleBackupPreview)
			r.Post("/api/backup/restore", s.handleBackupRestore)
			r.Get("/api/audit", s.handleAudit)
		})
	})

	r.NotFound(spaHandler().ServeHTTP)
	return r
}

func (s *Server) handleBranding(w http.ResponseWriter, r *http.Request) {
	name, _ := db.GetSetting(s.DB, "panel_name")
	if name == "" {
		name = "liking"
	}
	jsonOK(w, map[string]any{"panel_name": name, "version": version.Version})
}

func (s *Server) handleInstallAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	body := strings.ReplaceAll(installscript.AgentInstall, "__LIKING_PANEL_URL__", panelURL(s.DB, r))
	_, _ = w.Write([]byte(body))
}

func panelURL(d *sql.DB, r *http.Request) string {
	if v, _ := db.GetSetting(d, "panel_url"); v != "" {
		return strings.TrimRight(v, "/")
	}
	scheme := "http"
	if isSecureRequest(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
