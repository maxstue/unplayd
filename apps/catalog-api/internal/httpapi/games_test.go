package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
	"github.com/maxstue/unplayd/apps/catalog-api/internal/requestid"
)

// fakeCatalog records the requests it receives and answers with fixed results.
type fakeCatalog struct {
	search   catalog.SearchRequest
	detailID string
	batchIDs []string
	calls    int
	page     catalog.SearchPage
	detail   catalog.Detail
	batch    catalog.Batch
	err      error
}

func (f *fakeCatalog) Search(_ context.Context, request catalog.SearchRequest) (catalog.SearchPage, error) {
	f.calls++
	f.search = request
	return f.page, f.err
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

var (
	cover     = "https://images.igdb.com/igdb/image/upload/t_cover_big/co1.jpg"
	updatedAt = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	parent    = catalog.Game{
		ID: "6f1c1e0a-8a51-4b4e-9d43-0f6f9d2f0a01", Title: "The Witcher 3: Wild Hunt", Kind: catalog.KindGame,
		CoverURL: &cover, Platforms: []catalog.Platform{{ID: "6f1c1e0a-8a51-4b4e-9d43-0f6f9d2f0a02", Name: "PC"}},
		IsActive: true, UpdatedAt: updatedAt,
	}
	child = catalog.Game{
		ID: "6f1c1e0a-8a51-4b4e-9d43-0f6f9d2f0a03", Title: "Blood and Wine", Kind: catalog.KindExpansion,
		Parent: &catalog.ParentRef{ID: parent.ID, Title: parent.Title}, Platforms: []catalog.Platform{},
		IsActive: true, UpdatedAt: updatedAt,
	}
)

func TestSearchPassesParametersAndRendersTheContract(t *testing.T) {
	games := &fakeCatalog{page: catalog.SearchPage{
		Hits: []catalog.SearchHit{{
			Game:            parent,
			ChildCounts:     []catalog.KindCount{{Kind: catalog.KindExpansion, Count: 2}},
			MatchedChildren: []catalog.Game{child},
		}},
		NextCursor: "next-page",
	}}
	response := serve(newTestHandler(testToken, games), http.MethodGet,
		"/games?query=witcher+3&kind=dlc&kind=expansion&platformId="+parent.Platforms[0].ID+"&limit=7&cursor=abc", "")
	want := catalog.SearchRequest{
		Query: "witcher 3", Categories: []catalog.Category{catalog.CategoryDLC, catalog.CategoryExpansion},
		PlatformID: parent.Platforms[0].ID, Limit: 7, Cursor: "abc",
	}
	if !slices.Equal(games.search.Categories, want.Categories) || games.search.Query != want.Query ||
		games.search.PlatformID != want.PlatformID || games.search.Limit != want.Limit || games.search.Cursor != want.Cursor {
		t.Fatalf("search request = %+v, want %+v", games.search, want)
	}
	var expected map[string]any
	if err := json.Unmarshal([]byte(`{"items":[{"game":{"id":"6f1c1e0a-8a51-4b4e-9d43-0f6f9d2f0a01","title":"The Witcher 3: Wild Hunt",
		"kind":"game","parent":null,"coverUrl":"`+cover+`","platforms":[{"id":"6f1c1e0a-8a51-4b4e-9d43-0f6f9d2f0a02","name":"PC"}],
		"isActive":true,"updatedAt":"2026-10-29T12:00:00Z"},"childCounts":[{"kind":"expansion","count":2}],
		"matchedChildren":[{"id":"6f1c1e0a-8a51-4b4e-9d43-0f6f9d2f0a03","title":"Blood and Wine","kind":"expansion",
		"parent":{"id":"6f1c1e0a-8a51-4b4e-9d43-0f6f9d2f0a01","title":"The Witcher 3: Wild Hunt"},"coverUrl":null,
		"platforms":[],"isActive":true,"updatedAt":"2026-10-29T12:00:00Z"}]}],"nextCursor":"next-page"}`), &expected); err != nil {
		t.Fatal(err)
	}
	if body := decode[map[string]any](t, response); !reflect.DeepEqual(body, expected) {
		t.Fatalf("body = %v\nwant %v", body, expected)
	}

	t.Run("defaults and last page", func(t *testing.T) {
		games := &fakeCatalog{page: catalog.SearchPage{Hits: []catalog.SearchHit{}}}
		response := serve(newTestHandler(testToken, games), http.MethodGet, "/games?query=zelda", "")
		body := decode[map[string]any](t, response)
		if games.search.Limit != 20 || games.search.Cursor != "" || games.search.PlatformID != "" || len(games.search.Categories) != 0 {
			t.Fatalf("search request = %+v", games.search)
		}
		if items, ok := body["items"].([]any); !ok || len(items) != 0 || body["nextCursor"] != nil {
			t.Fatalf("body = %v, want empty items and null cursor", body)
		}
	})
}

func TestSearchRejectsMalformedParametersBeforeTheCatalog(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		{"missing query", "/games"},
		{"repeated query", "/games?query=zelda&query=mario"},
		{"repeated limit", "/games?query=zelda&limit=1&limit=2"},
		{"non numeric limit", "/games?query=zelda&limit=ten"},
		{"empty cursor", "/games?query=zelda&cursor="},
		{"empty platform", "/games?query=zelda&platformId="},
		{"malformed escape", "/games?query=%zz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			games := &fakeCatalog{}
			assertError(t, serve(newTestHandler(testToken, games), http.MethodGet, tc.query, ""), http.StatusBadRequest, "invalid_request")
			if games.calls != 0 {
				t.Fatal("malformed request reached the catalog")
			}
		})
	}
}

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

func TestDetailPassesThePathIDAndGroupsChildren(t *testing.T) {
	games := &fakeCatalog{detail: catalog.Detail{Game: parent, Children: []catalog.ChildGroup{{Kind: catalog.KindExpansion, Games: []catalog.Game{child}}}}}
	body := decode[gameDetailDTO](t, serve(newTestHandler(testToken, games), http.MethodGet, "/games/"+parent.ID, ""))
	if games.detailID != parent.ID || body.Game.ID != parent.ID || len(body.Children) != 1 || body.Children[0].Games[0].Parent.ID != parent.ID {
		t.Fatalf("detail id %q body %+v", games.detailID, body)
	}
}

func TestBatchRequiresAStrictJSONBody(t *testing.T) {
	for _, tc := range []struct{ name, contentType, body string }{
		{"wrong content type", "text/plain", `{"gameIds":[]}`},
		{"missing content type", "", `{"gameIds":[]}`},
		{"empty body", "application/json", ``},
		{"missing gameIds", "application/json", `{}`},
		{"null gameIds", "application/json", `{"gameIds":null}`},
		{"unknown field", "application/json", `{"gameIds":[],"extra":1}`},
		{"non string IDs", "application/json", `{"gameIds":[1]}`},
		{"trailing data", "application/json", `{"gameIds":[]} {}`},
		{"oversized body", "application/json", `{"gameIds":["` + strings.Repeat("a", maxBatchBody) + `"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			games := &fakeCatalog{}
			req := httptest.NewRequest(http.MethodPost, "/games/batch", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+testToken)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			response := httptest.NewRecorder()
			newTestHandler(testToken, games).ServeHTTP(response, req)
			assertError(t, response, http.StatusBadRequest, "invalid_request")
			if games.calls != 0 {
				t.Fatal("malformed batch reached the catalog")
			}
		})
	}
	t.Run("valid body with charset reaches the catalog", func(t *testing.T) {
		missing := "6f1c1e0a-8a51-4b4e-9d43-0f6f9d2f0a09"
		games := &fakeCatalog{batch: catalog.Batch{Games: []catalog.Game{parent}, Missing: []string{missing}, Inactive: []string{}}}
		req := httptest.NewRequest(http.MethodPost, "/games/batch", strings.NewReader(`{"gameIds":["`+parent.ID+`","`+missing+`"]}`))
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		response := httptest.NewRecorder()
		newTestHandler(testToken, games).ServeHTTP(response, req)
		body := decode[map[string]json.RawMessage](t, response)
		if !slices.Equal(games.batchIDs, []string{parent.ID, missing}) || string(body["missingGameIds"]) != `["`+missing+`"]` || string(body["inactiveGameIds"]) != `[]` {
			t.Fatalf("batch ids %v body %s", games.batchIDs, response.Body)
		}
	})
}

func TestRequestLogsCarryRequestIDWithoutCredentials(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(requestid.LogHandler{Handler: slog.NewJSONHandler(&logs, nil)})
	token := rand.Text() + rand.Text()
	handler := NewHandler(token, &fakeCatalog{err: errors.New("database down")}, logger)
	req := httptest.NewRequest(http.MethodGet, "/games?query=zelda", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Request-ID", "log_trace-42")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	var entries []map[string]any
	for line := range strings.Lines(logs.String()) {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	if len(entries) != 2 {
		t.Fatalf("log entries = %v, want failure and completion", entries)
	}
	for _, entry := range entries {
		if entry["requestId"] != "log_trace-42" {
			t.Fatalf("log entry without request ID: %v", entry)
		}
	}
	if entries[0]["error"] != "database down" || entries[1]["status"] != float64(500) || entries[1]["path"] != "/games" {
		t.Fatalf("log entries = %v", entries)
	}
	if strings.Contains(logs.String(), token) || strings.Contains(logs.String(), "zelda") {
		t.Fatal("logs contain credentials or query strings")
	}
}
