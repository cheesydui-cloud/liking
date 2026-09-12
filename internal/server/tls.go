package server

import (
	"crypto/tls"
	"log"
	"net"
	"strconv"
	"strings"

	"liking/internal/db"
)

func (s *Server) WrapListener(ln net.Listener) net.Listener {
	cfg := s.panelTLSConfig()
	if cfg == nil {
		return ln
	}
	s.TLSActive = true
	log.Printf("liking-server TLS enabled")
	return tls.NewListener(ln, cfg)
}

func (s *Server) panelTLSConfig() *tls.Config {
	idStr, _ := db.GetSetting(s.DB, "panel_tls_cert_id")
	idStr = strings.TrimSpace(idStr)
	if idStr == "" || idStr == "0" {
		return nil
	}
	if _, err := s.loadPanelCert(); err != nil {
		log.Printf("liking-server: panel TLS cert: %v", err)
		return nil
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return s.loadPanelCert()
		},
	}
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
	cert, err := tls.X509KeyPair([]byte(c.CertPEM), []byte(c.KeyPEM))
	if err != nil {
		return nil, err
	}
	return &cert, nil
}
