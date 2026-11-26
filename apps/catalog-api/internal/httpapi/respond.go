package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
	"github.com/maxstue/unplayd/apps/catalog-api/internal/requestid"
)

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

// fail translates catalog errors into the contract's error responses.
func (a api) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, catalog.ErrUnavailable):
		a.logger.WarnContext(r.Context(), "catalog request unavailable", "error", err.Error())
		writeError(w, r, http.StatusServiceUnavailable, "unavailable", "The game provider is unavailable. Try again later.")
	case errors.Is(err, catalog.ErrInvalidRequest):
		message := strings.TrimPrefix(err.Error(), catalog.ErrInvalidRequest.Error()+": ")
		writeError(w, r, http.StatusBadRequest, "invalid_request", strings.ToUpper(message[:1])+message[1:]+".")
	case errors.Is(err, catalog.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "Game not found.")
	case errors.Is(err, context.DeadlineExceeded):
		a.logger.WarnContext(r.Context(), "catalog request deadline exceeded")
		writeError(w, r, http.StatusServiceUnavailable, "unavailable", "The request timed out.")
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		a.logger.InfoContext(r.Context(), "catalog request canceled by the caller")
		writeError(w, r, http.StatusServiceUnavailable, "unavailable", "The request was canceled.")
	default:
		a.logger.ErrorContext(r.Context(), "catalog request failed", "error", err.Error())
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal error.")
	}
}
