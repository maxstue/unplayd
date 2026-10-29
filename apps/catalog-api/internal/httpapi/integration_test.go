package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
	"github.com/maxstue/unplayd/apps/catalog-api/internal/database/databasetest"
)

// providerStub is a provider that knows a fixed set of games and can fail.
type providerStub struct {
	mu       sync.Mutex
	games    []catalog.SourceGame
	err      error
	searches int
}

func (p *providerStub) Search(_ context.Context, query string, _ catalog.SearchFilter, _ int) ([]catalog.SourceGame, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.searches++
	words := strings.Fields(strings.ToLower(query))
	return p.match(func(game catalog.SourceGame) bool {
		return !slices.ContainsFunc(words, func(word string) bool { return !strings.Contains(strings.ToLower(game.Title), word) })
	}), p.err
}

func (p *providerStub) Games(_ context.Context, ids []string) ([]catalog.SourceGame, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.match(func(game catalog.SourceGame) bool { return slices.Contains(ids, game.ExternalID) }), p.err
}

func (p *providerStub) Children(_ context.Context, ids []string) ([]catalog.SourceGame, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.match(func(game catalog.SourceGame) bool { return slices.Contains(ids, game.ParentExternalID) }), p.err
}

func (p *providerStub) match(keep func(catalog.SourceGame) bool) []catalog.SourceGame {
	if p.err != nil {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(p.games), func(game catalog.SourceGame) bool { return !keep(game) })
}

func (p *providerStub) fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

func (p *providerStub) searchCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.searches
}

// routeFixture serves the catalog routes from PostgreSQL with a stub provider.
type routeFixture struct {
	db       *sql.DB
	provider *providerStub
	handler  http.Handler
	witcher  searchHitDTO
}

func newRouteFixture(t *testing.T) *routeFixture {
	db := databasetest.Open(t)
	provider := &providerStub{games: []catalog.SourceGame{
		{Source: "igdb", ExternalID: "1942", Title: "The Witcher 3: Wild Hunt", Kind: catalog.KindGame,
			Platforms: []catalog.SourcePlatform{{ExternalID: "6", Name: "PC (Microsoft Windows)"}}},
		{Source: "igdb", ExternalID: "13166", Title: "The Witcher 3: Wild Hunt - Blood and Wine", Kind: catalog.KindExpansion, ParentExternalID: "1942"},
		{Source: "igdb", ExternalID: "12503", Title: "The Witcher 3: Wild Hunt - Hearts of Stone", Kind: catalog.KindExpansion, ParentExternalID: "1942"},
		{Source: "igdb", ExternalID: "20", Title: "The Witcher 2: Assassins of Kings", Kind: catalog.KindGame},
		{Source: "igdb", ExternalID: "21", Title: "The Witcher", Kind: catalog.KindGame},
	}}
	store := catalog.NewStore(db)
	policy := catalog.SearchPolicy{FreshFor: time.Hour, Wait: 5 * time.Second, RefreshTimeout: 10 * time.Second, Cooldown: time.Minute}
	service := catalog.NewService(store, catalog.NewImporter(provider, store), policy, slog.New(slog.DiscardHandler))
	t.Cleanup(service.Wait)
	return &routeFixture{db: db, provider: provider, handler: NewHandler(testToken, service, slog.New(slog.DiscardHandler))}
}

func (f *routeFixture) get(target string) *httptest.ResponseRecorder {
	return serve(f.handler, http.MethodGet, target, "")
}

func (f *routeFixture) batch(t *testing.T, ids ...string) *httptest.ResponseRecorder {
	body, err := json.Marshal(map[string][]string{"gameIds": append([]string{}, ids...)})
	if err != nil {
		t.Fatal(err)
	}
	return serve(f.handler, http.MethodPost, "/games/batch", string(body))
}

func (f *routeFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func assertEqual[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func TestCatalogRoutesServeStoredGamesThroughPostgreSQL(t *testing.T) {
	fixture := newRouteFixture(t)
	page := decode[gamePageDTO](t, fixture.get("/games?query=witcher+3"))
	assertEqual(t, "hits", len(page.Items), 1)
	assertEqual(t, "next cursor", page.NextCursor, nil)
	fixture.witcher = page.Items[0]
	assertEqual(t, "title", fixture.witcher.Game.Title, "The Witcher 3: Wild Hunt")
	assertEqual(t, "matched children", len(fixture.witcher.MatchedChildren), 2)
	assertEqual(t, "child counts", fmt.Sprint(fixture.witcher.ChildCounts), "[{expansion 2}]")
	assertEqual(t, "platforms", len(fixture.witcher.Game.Platforms), 1)

	t.Run("search pages with an opaque cursor", fixture.testPagination)
	t.Run("detail validates the UUID and lists children", fixture.testDetail)
	t.Run("batch reports missing and inactive IDs explicitly", fixture.testBatch)
	t.Run("provider outage keeps stored games readable", fixture.testProviderOutage)
	t.Run("every catalog route requires the service token", fixture.testAuthentication)
}

func (f *routeFixture) testPagination(t *testing.T) {
	decode[gamePageDTO](t, f.get("/games?query=witcher&limit=50"))
	var seen []string
	target := "/games?query=witcher&limit=2"
	for target != "" {
		page := decode[gamePageDTO](t, f.get(target))
		for _, item := range page.Items {
			seen = append(seen, item.Game.Title)
		}
		target = ""
		if page.NextCursor != nil {
			target = "/games?query=witcher&limit=2&cursor=" + url.QueryEscape(*page.NextCursor)
		}
	}
	assertEqual(t, "paged titles", fmt.Sprint(seen), fmt.Sprint([]string{"The Witcher", "The Witcher 2: Assassins of Kings", "The Witcher 3: Wild Hunt"}))
	assertError(t, f.get("/games?query=zelda&limit=2&cursor=bm90LWEtY3Vyc29y"), http.StatusBadRequest, "invalid_request")
}

func (f *routeFixture) testDetail(t *testing.T) {
	detail := decode[gameDetailDTO](t, f.get("/games/"+strings.ToUpper(f.witcher.Game.ID)))
	assertEqual(t, "id", detail.Game.ID, f.witcher.Game.ID)
	assertEqual(t, "child groups", len(detail.Children), 1)
	assertEqual(t, "children", len(detail.Children[0].Games), 2)
	assertError(t, f.get("/games/not-a-uuid"), http.StatusBadRequest, "invalid_request")
	assertError(t, f.get("/games/00000000-0000-4000-8000-000000000000"), http.StatusNotFound, "not_found")
}

func (f *routeFixture) testBatch(t *testing.T) {
	inactive, child := f.witcher.MatchedChildren[1].ID, f.witcher.MatchedChildren[0].ID
	f.exec(t, `UPDATE catalog.games SET is_active = false WHERE id = $1`, inactive)
	missing := "00000000-0000-4000-8000-000000000001"
	batch := decode[batchResponseDTO](t, f.batch(t, f.witcher.Game.ID, missing, inactive, child))
	assertEqual(t, "items", len(batch.Items), 2)
	assertEqual(t, "missing", fmt.Sprint(batch.MissingGameIDs), fmt.Sprint([]string{missing}))
	assertEqual(t, "inactive", fmt.Sprint(batch.InactiveGameIDs), fmt.Sprint([]string{inactive}))
	assertEqual(t, "child parent", fmt.Sprint(batch.Items[1].Parent), fmt.Sprint(&parentDTO{ID: f.witcher.Game.ID, Title: f.witcher.Game.Title}))
	assertError(t, f.batch(t, missing, missing), http.StatusBadRequest, "invalid_request")
	assertError(t, f.batch(t), http.StatusBadRequest, "invalid_request")
}

func (f *routeFixture) testProviderOutage(t *testing.T) {
	f.exec(t, `UPDATE catalog.search_queries SET refreshed_at = now() - interval '2 hours'`)
	f.provider.fail(errors.New("igdb unavailable"))
	before := f.provider.searchCount()
	page := decode[gamePageDTO](t, f.get("/games?query=witcher+3"))
	assertEqual(t, "stale hits", len(page.Items), 1)
	assertEqual(t, "provider searches", f.provider.searchCount(), before+1)
	assertError(t, f.get("/games?query=elden+ring"), http.StatusServiceUnavailable, "unavailable")
	decode[gameDetailDTO](t, f.get("/games/"+f.witcher.Game.ID))
	decode[batchResponseDTO](t, f.batch(t, f.witcher.Game.ID))
}

func (f *routeFixture) testAuthentication(t *testing.T) {
	for _, route := range []struct{ method, target string }{
		{http.MethodGet, "/games?query=witcher"},
		{http.MethodGet, "/games/" + f.witcher.Game.ID},
		{http.MethodPost, "/games/batch"},
	} {
		req := httptest.NewRequest(route.method, route.target, strings.NewReader(`{"gameIds":["`+f.witcher.Game.ID+`"]}`))
		req.Header.Set("X-Request-ID", "auth_rejected-123")
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, req)
		assertUnauthorizedResponse(t, response)
	}
}
