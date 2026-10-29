// Package httpapi contains the catalog HTTP boundary, independent of process startup.
package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
	"github.com/maxstue/unplayd/apps/catalog-api/internal/requestid"
)

var requestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Catalog answers the catalog routes.
type Catalog interface {
	Search(ctx context.Context, request catalog.SearchRequest) (catalog.SearchPage, error)
	Detail(ctx context.Context, id string) (catalog.Detail, error)
	Batch(ctx context.Context, ids []string) (catalog.Batch, error)
}

// NewHandler returns the catalog routes with validated request correlation
// IDs, service authentication and one structured log entry per request.
func NewHandler(serviceToken string, games Catalog, logger *slog.Logger) http.Handler {
	expected := sha256.Sum256([]byte(serviceToken))
	api := api{games: games, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "catalog-api"})
	})
	mux.HandleFunc("GET /games", api.search)
	mux.HandleFunc("GET /games/{gameId}", api.detail)
	mux.HandleFunc("POST /games/batch", api.batch)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not_found", "Not found")
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		id := r.Header.Get("X-Request-ID")
		if !requestIDPattern.MatchString(id) {
			id = rand.Text()
		}
		r = r.WithContext(requestid.With(r.Context(), id))
		w.Header().Set("X-Request-ID", id)
		recorder := &statusRecorder{ResponseWriter: w}
		public := r.Method == http.MethodGet && r.URL.Path == "/health/live"
		defer func() {
			level := slog.LevelInfo
			if public {
				level = slog.LevelDebug
			}
			logger.Log(r.Context(), level, "request completed", "method", r.Method, "path", r.URL.Path,
				"status", recorder.code(), "durationMs", time.Since(started).Milliseconds())
		}()
		if !public {
			recorder.Header().Set("Cache-Control", "no-store")
			scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
			provided := sha256.Sum256([]byte(token))
			if len(r.Header.Values("Authorization")) != 1 || !found || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t\r\n") || serviceToken == "" || subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 {
				recorder.Header().Set("WWW-Authenticate", "Bearer")
				writeError(recorder, r, http.StatusUnauthorized, "unauthorized", "Unauthorized")
				return
			}
		}
		mux.ServeHTTP(recorder, r)
	})
}

// statusRecorder remembers the response status for the request log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}

func (r *statusRecorder) code() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError writes the contract's error envelope with a safe public message.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{
		"code": code, "message": message, "requestId": requestid.From(r.Context()),
	}})
}
