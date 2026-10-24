// Package httpapi contains the catalog HTTP boundary, independent of process startup.
package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

var requestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// NewHandler returns the catalog routes with validated request correlation IDs.
func NewHandler(serviceToken string) http.Handler {
	expected := sha256.Sum256([]byte(serviceToken))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "catalog-api"})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !requestIDPattern.MatchString(id) {
			id = rand.Text()
		}
		w.Header().Set("X-Request-ID", id)
		if r.Method != http.MethodGet || r.URL.Path != "/health/live" {
			w.Header().Set("Cache-Control", "no-store")
			scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
			provided := sha256.Sum256([]byte(token))
			if len(r.Header.Values("Authorization")) != 1 || !found || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t\r\n") || serviceToken == "" || subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "unauthorized", "message": "Unauthorized", "requestId": id}})
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
