// Package httpapi contains the catalog HTTP boundary, independent of process startup.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
)

// Catalog answers the catalog routes.
type Catalog interface {
	Search(ctx context.Context, request catalog.SearchRequest) (catalog.SearchPage, error)
	Suggestions(ctx context.Context, text string) ([]catalog.SearchHit, error)
	Detail(ctx context.Context, id string) (catalog.Detail, error)
	Batch(ctx context.Context, ids []string) (catalog.Batch, error)
}

// serviceName identifies this service in health responses.
const serviceName = "catalog-api"

// readinessTimeout bounds the database check of the readiness probe.
const readinessTimeout = time.Second

// Readiness reports whether the dependencies the service needs to answer
// requests are reachable. Only the catalog database counts: IGDB outages leave
// stored games available, so they never make the service unready.
type Readiness func(ctx context.Context) error

// NewHandler returns the catalog routes with validated request correlation
// IDs, service authentication and one structured log entry per request.
//
// GET /health/live answers as long as the process serves HTTP. GET
// /health/ready additionally requires ready to succeed and answers 503
// otherwise.
func NewHandler(serviceToken string, games Catalog, ready Readiness, logger *slog.Logger) http.Handler {
	api := api{games: games, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": serviceName})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()
		if err := ready(ctx); err != nil {
			logger.WarnContext(r.Context(), "readiness check failed", "check", "database")
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "unavailable", "service": serviceName, "checks": map[string]string{"database": "unavailable"}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok", "service": serviceName, "checks": map[string]string{"database": "ok"}})
	})
	mux.HandleFunc("GET /games", api.search)
	mux.HandleFunc("GET /games/suggestions", api.suggestions)
	mux.HandleFunc("GET /games/{gameId}", api.detail)
	mux.HandleFunc("POST /games/batch", api.batch)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not_found", "Not found")
	})
	return withTimeout(withRequestID(withLogging(logger, withAuth(serviceToken, mux))))
}
