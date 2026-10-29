package catalog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestService(store *Store, source Source, policy SearchPolicy) *Service {
	var importer *Importer
	if source != nil {
		importer = NewImporter(source, store)
	}
	return NewService(store, importer, policy, slog.New(slog.DiscardHandler))
}

var testPolicy = SearchPolicy{FreshFor: time.Hour, Wait: 10 * time.Second, RefreshTimeout: 10 * time.Second, Cooldown: time.Minute}

func searchPage(t *testing.T, service *Service, request SearchRequest) SearchPage {
	t.Helper()
	if request.Limit == 0 {
		request.Limit = 20
	}
	page, err := service.Search(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func providerSearches(source *fakeSource) int {
	return len(slices.DeleteFunc(slices.Clone(source.requests), func(request string) bool { return request != "search" }))
}

func expireSearches(t *testing.T, service *Service) {
	t.Helper()
	if _, err := service.store.db.ExecContext(t.Context(), `UPDATE catalog.search_queries SET refreshed_at = now() - interval '2 hours'`); err != nil {
		t.Fatal(err)
	}
}

func TestSearchRequestsAreValidatedBeforeAnyLookup(t *testing.T) {
	service := newTestService(nil, &fakeSource{}, testPolicy)
	valid := SearchRequest{Query: "zelda", Limit: 20}
	for _, tc := range []struct {
		name   string
		change func(*SearchRequest)
	}{
		{"query too short", func(r *SearchRequest) { r.Query = "  z  " }},
		{"query too long", func(r *SearchRequest) { r.Query = strings.Repeat("ü", 201) }},
		{"zero limit", func(r *SearchRequest) { r.Limit = 0 }},
		{"limit above maximum", func(r *SearchRequest) { r.Limit = 51 }},
		{"unknown kind", func(r *SearchRequest) { r.Categories = []Category{"sequel"} }},
		{"duplicate kind", func(r *SearchRequest) { r.Categories = []Category{CategoryDLC, CategoryDLC} }},
		{"platform is not a UUID", func(r *SearchRequest) { r.PlatformID = "switch" }},
		{"cursor is not base64", func(r *SearchRequest) { r.Cursor = "not a cursor!" }},
		{"cursor is too long", func(r *SearchRequest) { r.Cursor = strings.Repeat("a", 513) }},
		{"cursor has unknown fields", func(r *SearchRequest) { r.Cursor = "eyJ4IjoxfQ" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := valid
			tc.change(&request)
			if _, err := service.Search(t.Context(), request); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("err = %v, want invalid request", err)
			}
		})
	}
	for _, id := range []string{"", "not-a-uuid", "6f1c1e0a8a514b4e9d430f6f9d2f0a01", "6f1c1e0a-8a51-4b4e-9d43-0f6f9d2f0a01x"} {
		if _, err := service.Detail(t.Context(), id); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("Detail(%q) err = %v, want invalid request", id, err)
		}
	}
}

func TestSearchRefreshesOnlyStaleSearchesFromTheProvider(t *testing.T) {
	store, _ := newTestStore(t)
	source := &fakeSource{games: []SourceGame{witcher, bloodAndWine}, known: searchable}
	service := newTestService(store, source, testPolicy)

	page := searchPage(t, service, SearchRequest{Query: "Witcher 3"})
	if len(page.Hits) != 1 || page.Hits[0].Game.Title != witcher.Title || page.NextCursor != "" {
		t.Fatalf("hits = %v", hitTitles(page.Hits))
	}
	if want := []string{bloodAndWine.Title, complete.Title, anniversary.Title, nextGen.Title, goty.Title, heartsOfStone.Title}; !slices.Equal(titles(page.Hits[0].MatchedChildren), want) {
		t.Fatalf("matched children = %v, want stored relatives matching every word %v", titles(page.Hits[0].MatchedChildren), want)
	}
	if want := []KindCount{{KindEdition, 3}, {KindExpansion, 2}, {KindUpdate, 1}}; !slices.Equal(page.Hits[0].ChildCounts, want) {
		t.Fatalf("child counts = %v", page.Hits[0].ChildCounts)
	}
	if providerSearches(source) != 1 || source.query != "Witcher 3" || source.limit != maxSearchLimit {
		t.Fatalf("provider searches = %d, query %q, limit %d", providerSearches(source), source.query, source.limit)
	}

	t.Run("fresh searches are answered locally regardless of case and spacing", func(t *testing.T) {
		for _, query := range []string{"Witcher 3", "  witcher   3 ", "WITCHER 3"} {
			if page := searchPage(t, service, SearchRequest{Query: query}); len(page.Hits) != 1 {
				t.Fatalf("%q hits = %v", query, hitTitles(page.Hits))
			}
		}
		if providerSearches(source) != 1 {
			t.Fatalf("fresh searches reached the provider %d times", providerSearches(source))
		}
	})
	t.Run("kind filters are separate searches", func(t *testing.T) {
		page := searchPage(t, service, SearchRequest{Query: "witcher 3", Categories: []Category{CategoryExpansion}})
		if providerSearches(source) != 2 || source.filter.Kinds[0] != KindExpansion {
			t.Fatalf("filtered search was not refreshed: %v", source.filter)
		}
		if !slices.Equal(hitTitles(page.Hits), []string{bloodAndWine.Title, heartsOfStone.Title}) {
			t.Fatalf("expansion hits = %v", hitTitles(page.Hits))
		}
		assertChildHits(t, page.Hits, KindExpansion, witcher.Title)
	})
	t.Run("stale searches are refreshed", func(t *testing.T) {
		expireSearches(t, service)
		searchPage(t, service, SearchRequest{Query: "witcher 3"})
		if providerSearches(source) != 3 {
			t.Fatalf("stale search was not refreshed")
		}
	})
	t.Run("follow-up pages never contact the provider", func(t *testing.T) {
		filtered := SearchRequest{Query: "witcher 3", Categories: []Category{CategoryExpansion}, Limit: 1}
		first := searchPage(t, service, filtered)
		expireSearches(t, service)
		before := providerSearches(source)
		filtered.Cursor = first.NextCursor
		second := searchPage(t, service, filtered)
		if providerSearches(source) != before || len(second.Hits) != 1 || second.Hits[0].Game.ID == first.Hits[0].Game.ID {
			t.Fatalf("second page %v after %v, provider searches %d -> %d", hitTitles(second.Hits), hitTitles(first.Hits), before, providerSearches(source))
		}
	})
}

func TestSearchRanksFiltersAndPaginatesStoredGames(t *testing.T) {
	store, db := newTestStore(t)
	switchPlatform := SourcePlatform{ExternalID: "130", Name: "Nintendo Switch"}
	stored := map[string]Game{}
	for _, game := range []SourceGame{
		{Source: "igdb", ExternalID: "1", Title: "The Legend of Zelda", Kind: KindGame},
		{Source: "igdb", ExternalID: "2", Title: "Zelda II: The Adventure of Link", Kind: KindGame},
		{Source: "igdb", ExternalID: "3", Title: "Zelda", Kind: KindGame, Platforms: []SourcePlatform{switchPlatform}},
		{Source: "igdb", ExternalID: "4", Title: "Zelda Classic", Kind: KindMod, ParentExternalID: "1"},
		{Source: "igdb", ExternalID: "5", Title: "The Legend of Zelda: Breath of the Wild", Kind: KindGame, Platforms: []SourcePlatform{switchPlatform}},
		{Source: "igdb", ExternalID: "6", Title: "Link's Awakening", Kind: KindGame},
		{Source: "igdb", ExternalID: "7", Title: "100% Orange_Juice", Kind: KindGame},
	} {
		stored[game.Title] = upsertOne(t, store, game)
	}
	service := newTestService(store, nil, testPolicy)

	t.Run("hits rank by their best match: exact, prefix, then word", func(t *testing.T) {
		page := searchPage(t, service, SearchRequest{Query: "zelda"})
		// The Legend of Zelda ranks as a prefix match through its child Zelda Classic.
		want := []string{"Zelda", "The Legend of Zelda", "Zelda II: The Adventure of Link", "The Legend of Zelda: Breath of the Wild"}
		if !slices.Equal(hitTitles(page.Hits), want) || page.NextCursor != "" {
			t.Fatalf("hits = %v, want %v", hitTitles(page.Hits), want)
		}
		if !slices.Equal(titles(page.Hits[1].MatchedChildren), []string{"Zelda Classic"}) {
			t.Fatalf("matched children = %v", titles(page.Hits[1].MatchedChildren))
		}
	})
	t.Run("pages follow the same order without gaps or duplicates", func(t *testing.T) {
		var collected []string
		request := SearchRequest{Query: "zelda", Limit: 1}
		for range 10 {
			page := searchPage(t, service, request)
			collected = append(collected, hitTitles(page.Hits)...)
			if page.NextCursor == "" {
				break
			}
			if len(page.NextCursor) > maxCursorLength {
				t.Fatalf("cursor exceeds %d bytes", maxCursorLength)
			}
			request.Cursor = page.NextCursor
		}
		if want := hitTitles(searchPage(t, service, SearchRequest{Query: "zelda"}).Hits); !slices.Equal(collected, want) {
			t.Fatalf("paged = %v, want %v", collected, want)
		}
	})
	t.Run("cursors only continue the search they were issued for", func(t *testing.T) {
		cursor := searchPage(t, service, SearchRequest{Query: "zelda", Limit: 1}).NextCursor
		for _, request := range []SearchRequest{
			{Query: "link", Limit: 1, Cursor: cursor},
			{Query: "zelda", Limit: 1, Cursor: cursor, Categories: []Category{CategoryGame}},
			{Query: "zelda", Limit: 1, Cursor: cursor, PlatformID: stored["Zelda"].Platforms[0].ID},
		} {
			if _, err := service.Search(t.Context(), request); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("foreign cursor err = %v", err)
			}
		}
		if page := searchPage(t, service, SearchRequest{Query: " ZELDA ", Limit: 1, Cursor: cursor}); len(page.Hits) != 1 {
			t.Fatal("equivalent search rejected its cursor")
		}
	})
	t.Run("platform and kind filters", func(t *testing.T) {
		page := searchPage(t, service, SearchRequest{Query: "zelda", PlatformID: strings.ToUpper(stored["Zelda"].Platforms[0].ID)})
		if want := []string{"Zelda", "The Legend of Zelda: Breath of the Wild"}; !slices.Equal(hitTitles(page.Hits), want) {
			t.Fatalf("platform hits = %v", hitTitles(page.Hits))
		}
		page = searchPage(t, service, SearchRequest{Query: "zelda", Categories: []Category{CategoryMod}})
		if len(page.Hits) != 1 || page.Hits[0].Game.Parent == nil || page.Hits[0].Game.Parent.Title != "The Legend of Zelda" || len(page.Hits[0].MatchedChildren) != 0 {
			t.Fatalf("mod hits = %+v", page.Hits)
		}
		page = searchPage(t, service, SearchRequest{Query: "zelda", Categories: []Category{CategoryGame}})
		if slices.Contains(hitTitles(page.Hits), "Zelda Classic") || len(page.Hits) != 4 {
			t.Fatalf("parent hits = %v", hitTitles(page.Hits))
		}
	})
	t.Run("wildcards in queries match literally", func(t *testing.T) {
		if page := searchPage(t, service, SearchRequest{Query: "0% orange_"}); !slices.Equal(hitTitles(page.Hits), []string{"100% Orange_Juice"}) {
			t.Fatalf("hits = %v", hitTitles(page.Hits))
		}
		if page := searchPage(t, service, SearchRequest{Query: "%_"}); len(page.Hits) != 0 {
			t.Fatalf("wildcards matched %v", hitTitles(page.Hits))
		}
	})
	t.Run("provider matches without the query in their title are found", func(t *testing.T) {
		query, err := newLocalQuery(SearchRequest{Query: "botw", Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.recordSearch(t.Context(), query.key, []string{stored["The Legend of Zelda: Breath of the Wild"].ID}); err != nil {
			t.Fatal(err)
		}
		if page := searchPage(t, service, SearchRequest{Query: "BotW"}); !slices.Equal(hitTitles(page.Hits), []string{"The Legend of Zelda: Breath of the Wild"}) {
			t.Fatalf("hits = %v", hitTitles(page.Hits))
		}
	})
	t.Run("inactive games are not found", func(t *testing.T) {
		if _, err := db.ExecContext(t.Context(), `UPDATE catalog.games SET is_active = false WHERE id = $1`, stored["Zelda"].ID); err != nil {
			t.Fatal(err)
		}
		if page := searchPage(t, service, SearchRequest{Query: "zelda"}); slices.Contains(hitTitles(page.Hits), "Zelda") {
			t.Fatalf("hits = %v", hitTitles(page.Hits))
		}
	})
}

func TestSearchFallsBackToStoredGamesWhileTheProviderFails(t *testing.T) {
	store, _ := newTestStore(t)
	source := &fakeSource{games: []SourceGame{witcher}, known: searchable}
	service := newTestService(store, source, testPolicy)
	now := time.Now()
	service.now = func() time.Time { return now }
	searchPage(t, service, SearchRequest{Query: "witcher 3"})
	expireSearches(t, service)
	source.err = errors.New("igdb unavailable")

	page := searchPage(t, service, SearchRequest{Query: "witcher 3"})
	if len(page.Hits) != 1 || page.Hits[0].Game.Title != witcher.Title || providerSearches(source) != 2 {
		t.Fatalf("stale hits = %v after %d provider searches", hitTitles(page.Hits), providerSearches(source))
	}
	for _, step := range []struct {
		name     string
		advance  time.Duration
		searches int
	}{
		{"cooldown skips the provider", 0, 2},
		{"after the cooldown the provider is retried", testPolicy.Cooldown + time.Second, 3},
	} {
		t.Run(step.name, func(t *testing.T) {
			now = now.Add(step.advance)
			if _, err := service.Search(t.Context(), SearchRequest{Query: "elden ring", Limit: 20}); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("search without stored matches err = %v, want unavailable", err)
			}
			if providerSearches(source) != step.searches {
				t.Fatalf("provider searches = %d, want %d", providerSearches(source), step.searches)
			}
		})
	}
	t.Run("details and batches stay readable", func(t *testing.T) {
		calls := source.calls
		if _, err := service.Detail(t.Context(), page.Hits[0].Game.ID); err != nil {
			t.Fatal(err)
		}
		if batch, err := service.Batch(t.Context(), []string{page.Hits[0].Game.ID}); err != nil || len(batch.Games) != 1 {
			t.Fatalf("batch = %+v, %v", batch, err)
		}
		if source.calls != calls {
			t.Fatal("details or batches contacted the provider")
		}
	})
	t.Run("recovered provider refreshes again", func(t *testing.T) {
		source.err = nil
		now = now.Add(testPolicy.Cooldown + time.Second)
		searchPage(t, service, SearchRequest{Query: "elden ring"})
		if providerSearches(source) != 4 {
			t.Fatalf("provider searches = %d, want 4", providerSearches(source))
		}
	})
}

// blockingSource holds provider searches until released.
type blockingSource struct {
	*fakeSource
	release  chan struct{}
	searches atomic.Int32
}

func (b *blockingSource) Search(ctx context.Context, query string, filter SearchFilter, limit int) ([]SourceGame, error) {
	b.searches.Add(1)
	select {
	case <-b.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return b.fakeSource.Search(ctx, query, filter, limit)
}

func TestSlowRefreshesFinishInTheBackgroundAndAreShared(t *testing.T) {
	store, _ := newTestStore(t)
	source := &blockingSource{fakeSource: &fakeSource{games: []SourceGame{witcher}, known: searchable}, release: make(chan struct{})}
	policy := testPolicy
	policy.Wait = 50 * time.Millisecond
	service := newTestService(store, source, policy)
	t.Cleanup(service.Wait)

	for range 2 {
		if _, err := service.Search(t.Context(), SearchRequest{Query: "witcher 3", Limit: 20}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("pending refresh without stored games err = %v, want unavailable", err)
		}
	}
	close(source.release)
	service.Wait()
	page := searchPage(t, service, SearchRequest{Query: "witcher 3"})
	if len(page.Hits) != 1 || source.searches.Load() != 1 {
		t.Fatalf("hits = %v after %d provider searches, want one shared refresh", hitTitles(page.Hits), source.searches.Load())
	}
}

func TestBatchClassifiesEveryRequestedIDOnce(t *testing.T) {
	store, db := newTestStore(t)
	games, err := store.Upsert(t.Context(), []SourceGame{witcher, bloodAndWine, heartsOfStone})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE catalog.games SET is_active = false WHERE id = $1`, games[2].ID); err != nil {
		t.Fatal(err)
	}
	service := newTestService(store, nil, testPolicy)
	missing := "00000000-0000-4000-8000-000000000000"

	batch, err := service.Batch(t.Context(), []string{missing, strings.ToUpper(games[1].ID), games[2].ID, games[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(titles(batch.Games), []string{bloodAndWine.Title, witcher.Title}) || batch.Games[0].ID != games[1].ID ||
		batch.Games[0].Parent == nil || batch.Games[0].Parent.ID != games[0].ID {
		t.Fatalf("games = %+v", batch.Games)
	}
	if !slices.Equal(batch.Missing, []string{missing}) || !slices.Equal(batch.Inactive, []string{games[2].ID}) {
		t.Fatalf("missing %v inactive %v", batch.Missing, batch.Inactive)
	}

	t.Run("maximum batch size", func(t *testing.T) {
		ids := make([]string, maxBatchSize)
		for index := range ids {
			ids[index] = fmt.Sprintf("00000000-0000-4000-8000-%012d", index)
		}
		batch, err := service.Batch(t.Context(), ids)
		if err != nil || len(batch.Missing) != maxBatchSize {
			t.Fatalf("batch of %d: %d missing, %v", maxBatchSize, len(batch.Missing), err)
		}
	})
	for _, tc := range []struct {
		name string
		ids  []string
	}{
		{"empty", nil},
		{"above maximum", slices.Repeat([]string{missing}, maxBatchSize+1)},
		{"duplicate", []string{missing, missing}},
		{"duplicate in another case", []string{games[0].ID, strings.ToUpper(games[0].ID)}},
		{"not a UUID", []string{"7346"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.Batch(t.Context(), tc.ids); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("err = %v, want invalid request", err)
			}
		})
	}
	t.Run("details of inactive games stay readable", func(t *testing.T) {
		detail, err := service.Detail(t.Context(), strings.ToUpper(games[2].ID))
		if err != nil || detail.Game.IsActive || detail.Game.ID != games[2].ID {
			t.Fatalf("detail = %+v, %v", detail, err)
		}
		if _, err := service.Detail(t.Context(), missing); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing detail err = %v", err)
		}
	})
}

func TestDefaultSearchPolicyFitsTheServiceClientBudget(t *testing.T) {
	policy := DefaultSearchPolicy()
	if policy.Wait >= 3*time.Second || policy.FreshFor != 24*time.Hour || policy.RefreshTimeout <= policy.Wait || policy.Cooldown <= 0 {
		t.Fatalf("policy = %+v", policy)
	}
}

// TestTitleSearchUsesTheTrigramIndex checks that a large catalog answers title
// searches through the trigram index. Small catalogs are cheaper to scan.
func TestTitleSearchUsesTheTrigramIndex(t *testing.T) {
	_, db := newTestStore(t)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO catalog.games(title, kind)
		SELECT 'Generated game ' || md5(n::text), 'game' FROM generate_series(1, 200000) AS n`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `ANALYZE catalog.games`); err != nil {
		t.Fatal(err)
	}
	query, err := newLocalQuery(SearchRequest{Query: "the zelda", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(t.Context(), "EXPLAIN "+searchSQL, query.searchArgs()...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "games_active_title_trgm") || !strings.Contains(plan.String(), "'%zelda%'") {
		t.Fatalf("title search does not use the trigram index:\n%s", plan.String())
	}
}
