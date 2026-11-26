package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
)

func TestLivenessReturnsServiceIdentity(t *testing.T) {
	response := httptest.NewRecorder()
	newTestHandler(strings.Repeat("x", 32), &fakeCatalog{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected response: %d %v", response.Code, response.Header())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["service"] != "catalog-api" {
		t.Fatalf("unexpected health payload: %v", body)
	}
}

// fakeCatalog records the requests it receives and answers with fixed results.
type fakeCatalog struct {
	search     catalog.SearchRequest
	suggestion string
	detailID   string
	batchIDs   []string
	calls      int
	page       catalog.SearchPage
	detail     catalog.Detail
	batch      catalog.Batch
	err        error
	deadline   time.Time
}

func (f *fakeCatalog) Search(ctx context.Context, request catalog.SearchRequest) (catalog.SearchPage, error) {
	f.calls++
	f.deadline, _ = ctx.Deadline()
	f.search = request
	return f.page, f.err
}

func (f *fakeCatalog) Suggestions(_ context.Context, text string) ([]catalog.SearchHit, error) {
	f.calls++
	f.suggestion = text
	return f.page.Hits, f.err
}

func (f *fakeCatalog) Detail(_ context.Context, id string) (catalog.Detail, error) {
	f.calls++
	f.detailID = id
	return f.detail, f.err
}

func (f *fakeCatalog) Batch(_ context.Context, ids []string) (catalog.Batch, error) {
	f.calls++
	f.batchIDs = ids
	return f.batch, f.err
}

func newTestHandler(token string, games Catalog) http.Handler {
	return NewHandler(token, games, slog.New(slog.DiscardHandler))
}

const testToken = "test-service-token-with-at-least-32-bytes"

func serve(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Request-ID", "games_test-1")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func assertError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d: %s", response.Code, status, response.Body)
	}
	var body struct {
		Error struct{ Code, Message, RequestID string }
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != code || body.Error.Message == "" || body.Error.RequestID != response.Header().Get("X-Request-ID") {
		t.Fatalf("error envelope = %+v, want code %s", body.Error, code)
	}
}

func decode[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	var body T
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}
