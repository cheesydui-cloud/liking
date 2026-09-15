package server

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

const maxJSONBody = 1 << 20

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func jsonErr(w http.ResponseWriter, code int, msg string) {
	jsonErrExtra(w, code, msg, nil)
}

func jsonErrExtra(w http.ResponseWriter, code int, msg string, extra map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	m := map[string]any{"error": msg}
	for k, v := range extra {
		m[k] = v
	}
	_ = json.NewEncoder(w).Encode(m)
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	ct := r.Header.Get("Content-Type")
	media, _, err := mime.ParseMediaType(ct)
	if err != nil || !strings.EqualFold(media, "application/json") {
		return fmt.Errorf("需要 JSON")
	}
	return json.NewDecoder(io.LimitReader(r.Body, maxJSONBody)).Decode(v)
}

func chiID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, name), 10, 64)
}
