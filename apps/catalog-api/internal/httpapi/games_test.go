package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
)

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

func TestSuggestionsUseABoundedSearch(t *testing.T) {
	games := &fakeCatalog{page: catalog.SearchPage{Hits: []catalog.SearchHit{{Game: parent}}, NextCursor: "unused"}}
	response := serve(newTestHandler(testToken, games), http.MethodGet, "/games/suggestions?query=witcher", "")
	body := decode[gamePageDTO](t, response)
	if games.suggestion != "witcher" || games.search.Query != "" {
		t.Fatalf("suggestion request = %q, provider-backed request = %+v", games.suggestion, games.search)
	}
	if len(body.Items) != 1 || body.Items[0].Game.ID != parent.ID || body.NextCursor != nil {
		t.Fatalf("suggestion response = %+v", body)
	}
	for _, query := range []string{"", "query=witcher&query=zelda", "query=witcher&limit=50", "query=witcher&cursor=page", "query=%zz"} {
		before := games.calls
		response := serve(newTestHandler(testToken, games), http.MethodGet, "/games/suggestions?"+query, "")
		assertError(t, response, http.StatusBadRequest, "invalid_request")
		if games.calls != before {
			t.Fatal("invalid suggestion parameters reached the catalog")
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/games/suggestions?query=witcher", nil)
	response = httptest.NewRecorder()
	newTestHandler(testToken, games).ServeHTTP(response, req)
	assertError(t, response, http.StatusUnauthorized, "unauthorized")
}
