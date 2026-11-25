// Package httpapi contains the catalog HTTP boundary, independent of process startup.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
)

// Catalog answers the catalog routes.
type Catalog interface {
	Search(ctx context.Context, request catalog.SearchRequest) (catalog.SearchPage, error)
	Detail(ctx context.Context, id string) (catalog.Detail, error)
	Batch(ctx context.Context, ids []string) (catalog.Batch, error)
}

// NewHandler returns the catalog routes with validated request correlation
// IDs, service authentication and one structured log entry per request.
func NewHandler(serviceToken string, games Catalog, logger *slog.Logger) http.Handler {
	api := api{games: games, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "catalog-api"})
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
