// Package httpapi contains the catalog HTTP boundary, independent of process startup.
package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"regexp"
)

var requestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// NewHandler returns the catalog routes with validated request correlation IDs.
func NewHandler() http.Handler {
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
		mux.ServeHTTP(w, r)
	})
}
