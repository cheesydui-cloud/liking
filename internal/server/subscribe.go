package server

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"liking/internal/corecfg"
	"liking/internal/db"
)

func (s *Server) handleSub(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	fmtName := strings.ToLower(chi.URLParam(r, "fmt"))
	if fmtName == "" {
		fmtName = strings.ToLower(r.URL.Query().Get("format"))
	}
	if fmtName == "" {
		fmtName = detectSubFormat(r.UserAgent())
	}
	u, err := db.GetUserBySubToken(s.DB, token)
	if err != nil || u == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var pkg *db.Package
	if u.PackageID != nil {
		pkg, _ = db.GetPackage(s.DB, *u.PackageID)
	}
	if !db.UserAccessOK(u, pkg) {
		http.Error(w, "expired", http.StatusForbidden)
		return
	}
	clients, err := db.ListClientsByUser(s.DB, u.ID)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	switch fmtName {
	case "clash", "meta", "mihomo":
		body, err := buildClash(s.DB, clients)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.Header().Set("Profile-Title", "liking")
		_, _ = w.Write([]byte(body))
	case "singbox", "sing-box", "sfa":
		body, err := buildSingboxSub(s.DB, clients)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	default:
		body, err := buildURIList(s.DB, clients)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(body))))
	}
}

func detectSubFormat(ua string) string {
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "clash") || strings.Contains(l, "stash") || strings.Contains(l, "mihomo") || strings.Contains(l, "meta"):
		return "clash"
	case strings.Contains(l, "sing-box") || strings.Contains(l, "singbox") || strings.Contains(l, "sfa") || strings.Contains(l, "sfi"):
		return "singbox"
	default:
		return "uri"
	}
}

func buildURIList(d *sql.DB, clients []*db.Client) (string, error) {
	var lines []string
	for _, c := range clients {
		if c == nil || !c.Enabled {
			continue
		}
		in, err := db.GetInbound(d, c.InboundID)
		if err != nil {
			continue
		}
		uri, err := corecfg.ShareURI(in, c)
		if err != nil {
			continue
		}
		lines = append(lines, uri)
	}
	return strings.Join(lines, "\n"), nil
}

func buildClash(d *sql.DB, clients []*db.Client) (string, error) {
	var names []string
	var b strings.Builder
	for _, c := range clients {
		if c == nil || !c.Enabled {
			continue
		}
		in, err := db.GetInbound(d, c.InboundID)
		if err != nil {
			continue
		}
		name, yaml, err := corecfg.ClashProxyYAML(in, c)
		if err != nil {
			continue
		}
		names = append(names, name)
		b.WriteString(yaml)
	}
	return corecfg.ClashDocument(names, b.String()), nil
}

func buildSingboxSub(d *sql.DB, clients []*db.Client) ([]byte, error) {
	var tags []string
	var outs []any
	for _, c := range clients {
		if c == nil || !c.Enabled {
			continue
		}
		in, err := db.GetInbound(d, c.InboundID)
		if err != nil {
			continue
		}
		ob, err := corecfg.SingboxOutbound(in, c)
		if err != nil {
			if errors.Is(err, corecfg.ErrSkip) {
				continue
			}
			continue
		}
		outs = append(outs, ob)
		if tag, ok := ob["tag"].(string); ok {
			tags = append(tags, tag)
		}
	}
	if tags == nil {
		tags = []string{"direct"}
	}
	outs = append([]any{map[string]any{
		"type":      "selector",
		"tag":       "liking",
		"outbounds": tags,
	}}, outs...)
	outs = append(outs, map[string]any{"type": "direct", "tag": "direct"})
	return json.MarshalIndent(map[string]any{
		"outbounds": outs,
	}, "", "  ")
}
