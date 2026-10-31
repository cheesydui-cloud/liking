package server

import (
	"crypto/tls"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"liking/internal/db"
)

type panelCertCache struct {
	mu   sync.Mutex
	id   int64
	pem  string
	key  string
	cert *tls.Certificate
}

func (s *Server) WrapListener(ln net.Listener) (net.Listener, error) {
	cfg, err := s.panelTLSConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return ln, nil
	}
	s.TLSActive = true
	log.Printf("liking-server TLS enabled")
	return tls.NewListener(ln, cfg), nil
}

func (s *Server) panelTLSConfig() (*tls.Config, error) {
	idStr, _ := db.GetSetting(s.DB, "panel_tls_cert_id")
	idStr = strings.TrimSpace(idStr)
	if idStr == "" || idStr == "0" {
		return nil, nil
	}
	if _, err := s.loadPanelCert(); err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return s.loadPanelCert()
		},
	}, nil
}

func (s *Server) loadPanelCert() (*tls.Certificate, error) {
	idStr, _ := db.GetSetting(s.DB, "panel_tls_cert_id")
	id, _ := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
	if id <= 0 {
		return nil, errBad("未选择面板证书")
	}
	c, err := db.GetCert(s.DB, id)
	if err != nil {
		return nil, err
	}
	s.certCache.mu.Lock()
	defer s.certCache.mu.Unlock()
	if s.certCache.cert != nil && s.certCache.id == id && s.certCache.pem == c.CertPEM && s.certCache.key == c.KeyPEM {
		return s.certCache.cert, nil
	}
	cert, err := tls.X509KeyPair([]byte(c.CertPEM), []byte(c.KeyPEM))
	if err != nil {
		return nil, err
	}
	s.certCache.id = id
	s.certCache.pem = c.CertPEM
	s.certCache.key = c.KeyPEM
	s.certCache.cert = &cert
	return s.certCache.cert, nil
}
