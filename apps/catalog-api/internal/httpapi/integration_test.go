package httpapi

import (
	"context"
	"errors"
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

func TestCatalogRoutesServeStoredGamesThroughPostgreSQL(t *testing.T) {
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
	handler := NewHandler(testToken, service, slog.New(slog.DiscardHandler))

	page := decode[gamePageDTO](t, serve(handler, http.MethodGet, "/games?query=witcher+3", ""))
	if len(page.Items) != 1 || page.Items[0].Game.Title != "The Witcher 3: Wild Hunt" || page.NextCursor != nil {
		t.Fatalf("page = %+v", page)
	}
	witcher := page.Items[0]
	if len(witcher.MatchedChildren) != 2 || len(witcher.ChildCounts) != 1 || witcher.ChildCounts[0].Count != 2 || len(witcher.Game.Platforms) != 1 {
		t.Fatalf("witcher hit = %+v", witcher)
	}

	t.Run("search pages with an opaque cursor", func(t *testing.T) {
		decode[gamePageDTO](t, serve(handler, http.MethodGet, "/games?query=witcher&limit=50", ""))
		var seen []string
		target := "/games?query=witcher&limit=2"
		for target != "" {
			page := decode[gamePageDTO](t, serve(handler, http.MethodGet, target, ""))
			for _, item := range page.Items {
				seen = append(seen, item.Game.Title)
			}
			target = ""
			if page.NextCursor != nil {
				target = "/games?query=witcher&limit=2&cursor=" + url.QueryEscape(*page.NextCursor)
			}
		}
		if want := []string{"The Witcher", "The Witcher 2: Assassins of Kings", "The Witcher 3: Wild Hunt"}; !slices.Equal(seen, want) {
			t.Fatalf("paged titles = %v, want %v", seen, want)
		}
		assertError(t, serve(handler, http.MethodGet, "/games?query=zelda&limit=2&cursor=bm90LWEtY3Vyc29y", ""), http.StatusBadRequest, "invalid_request")
	})
	t.Run("detail validates the UUID and lists children", func(t *testing.T) {
		detail := decode[gameDetailDTO](t, serve(handler, http.MethodGet, "/games/"+strings.ToUpper(witcher.Game.ID), ""))
		if detail.Game.ID != witcher.Game.ID || len(detail.Children) != 1 || len(detail.Children[0].Games) != 2 {
			t.Fatalf("detail = %+v", detail)
		}
		assertError(t, serve(handler, http.MethodGet, "/games/not-a-uuid", ""), http.StatusBadRequest, "invalid_request")
		assertError(t, serve(handler, http.MethodGet, "/games/00000000-0000-4000-8000-000000000000", ""), http.StatusNotFound, "not_found")
	})
	t.Run("batch reports missing and inactive IDs explicitly", func(t *testing.T) {
		inactive := witcher.MatchedChildren[1].ID
		if _, err := db.ExecContext(t.Context(), `UPDATE catalog.games SET is_active = false WHERE id = $1`, inactive); err != nil {
			t.Fatal(err)
		}
		missing := "00000000-0000-4000-8000-000000000001"
		body := `{"gameIds":["` + witcher.Game.ID + `","` + missing + `","` + inactive + `","` + witcher.MatchedChildren[0].ID + `"]}`
		batch := decode[batchResponseDTO](t, serve(handler, http.MethodPost, "/games/batch", body))
		if len(batch.Items) != 2 || !slices.Equal(batch.MissingGameIDs, []string{missing}) || !slices.Equal(batch.InactiveGameIDs, []string{inactive}) {
			t.Fatalf("batch = %+v", batch)
		}
		if batch.Items[1].Parent == nil || batch.Items[1].Parent.ID != witcher.Game.ID {
			t.Fatalf("child lacks its parent reference: %+v", batch.Items[1])
		}
		assertError(t, serve(handler, http.MethodPost, "/games/batch", `{"gameIds":["`+missing+`","`+missing+`"]}`), http.StatusBadRequest, "invalid_request")
		assertError(t, serve(handler, http.MethodPost, "/games/batch", `{"gameIds":[]}`), http.StatusBadRequest, "invalid_request")
	})
	t.Run("provider outage keeps stored games readable", func(t *testing.T) {
		if _, err := db.ExecContext(t.Context(), `UPDATE catalog.search_queries SET refreshed_at = now() - interval '2 hours'`); err != nil {
			t.Fatal(err)
		}
		provider.fail(errors.New("igdb unavailable"))
		before := provider.searchCount()
		page := decode[gamePageDTO](t, serve(handler, http.MethodGet, "/games?query=witcher+3", ""))
		if len(page.Items) != 1 || provider.searchCount() != before+1 {
			t.Fatalf("stale search during outage = %+v", page)
		}
		assertError(t, serve(handler, http.MethodGet, "/games?query=elden+ring", ""), http.StatusServiceUnavailable, "unavailable")
		decode[gameDetailDTO](t, serve(handler, http.MethodGet, "/games/"+witcher.Game.ID, ""))
		decode[batchResponseDTO](t, serve(handler, http.MethodPost, "/games/batch", `{"gameIds":["`+witcher.Game.ID+`"]}`))
	})
	t.Run("every catalog route requires the service token", func(t *testing.T) {
		for _, route := range []struct{ method, target string }{
			{http.MethodGet, "/games?query=witcher"},
			{http.MethodGet, "/games/" + witcher.Game.ID},
			{http.MethodPost, "/games/batch"},
		} {
			req := httptest.NewRequest(route.method, route.target, strings.NewReader(`{"gameIds":["`+witcher.Game.ID+`"]}`))
			req.Header.Set("X-Request-ID", "auth_rejected-123")
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			assertUnauthorizedResponse(t, response)
		}
	})
}
