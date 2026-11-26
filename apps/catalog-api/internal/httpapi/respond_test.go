package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
)

func TestCatalogErrorsMapToContractResponses(t *testing.T) {
	secret := "postgres://catalog:secret@db/internal"
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid request", fmt.Errorf("%w: limit must be between 1 and 50", catalog.ErrInvalidRequest), http.StatusBadRequest, "invalid_request"},
		{"not found", catalog.ErrNotFound, http.StatusNotFound, "not_found"},
		{"provider unavailable", fmt.Errorf("%w: %s", catalog.ErrUnavailable, secret), http.StatusServiceUnavailable, "unavailable"},
		{"deadline exceeded", context.DeadlineExceeded, http.StatusServiceUnavailable, "unavailable"},
		{"internal failure", errors.New(secret), http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := newTestHandler(testToken, &fakeCatalog{err: tc.err})
			for _, request := range []struct{ method, target, body string }{
				{http.MethodGet, "/games?query=zelda", ""},
				{http.MethodGet, "/games/" + parent.ID, ""},
				{http.MethodPost, "/games/batch", `{"gameIds":["` + parent.ID + `"]}`},
			} {
				response := serve(handler, request.method, request.target, request.body)
				assertError(t, response, tc.status, tc.code)
				if strings.Contains(response.Body.String(), "secret") {
					t.Fatal("error response leaked internal details")
				}
			}
		})
	}
	t.Run("invalid request message is public", func(t *testing.T) {
		response := serve(newTestHandler(testToken, &fakeCatalog{err: fmt.Errorf("%w: limit must be between 1 and 50", catalog.ErrInvalidRequest)}),
			http.MethodGet, "/games?query=zelda", "")
		if !strings.Contains(response.Body.String(), `"message":"Limit must be between 1 and 50."`) {
			t.Fatalf("body = %s", response.Body)
		}
	})
}
