package catalog

import (
	"context"
	"database/sql"
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

func assertTitles(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("titles = %v, want %v", got, want)
	}
}

func assertProviderSearches(t *testing.T, source *fakeSource, want int) {
	t.Helper()
	if got := providerSearches(source); got != want {
		t.Fatalf("provider searches = %d, want %d", got, want)
	}
}

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("err = %v, want %v", err, target)
	}
}

func execSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestSearchRefreshesOnlyStaleSearchesFromTheProvider(t *testing.T) {
	store, _ := newTestStore(t)
	source := &fakeSource{games: []SourceGame{witcher, bloodAndWine}, known: searchable}
	service := newTestService(store, source, testPolicy)

	page := searchPage(t, service, SearchRequest{Query: "Witcher 3"})
	assertTitles(t, hitTitles(page.Hits), witcher.Title)
	// Every stored relative whose title contains every query word matches.
	assertTitles(t, titles(page.Hits[0].MatchedChildren), bloodAndWine.Title, complete.Title, anniversary.Title, nextGen.Title, goty.Title, heartsOfStone.Title)
	if want := []KindCount{{KindEdition, 3}, {KindExpansion, 2}, {KindUpdate, 1}}; !slices.Equal(page.Hits[0].ChildCounts, want) {
		t.Fatalf("child counts = %v", page.Hits[0].ChildCounts)
	}
	if page.NextCursor != "" || source.query != "Witcher 3" || source.limit != maxSearchLimit {
		t.Fatalf("cursor %q, provider query %q, limit %d", page.NextCursor, source.query, source.limit)
	}
	assertProviderSearches(t, source, 1)

	t.Run("fresh searches are answered locally regardless of case and spacing", func(t *testing.T) {
		for _, query := range []string{"Witcher 3", "  witcher   3 ", "WITCHER 3"} {
			assertTitles(t, hitTitles(searchPage(t, service, SearchRequest{Query: query}).Hits), witcher.Title)
		}
		assertProviderSearches(t, source, 1)
	})
	t.Run("kind filters are separate searches", func(t *testing.T) {
		page := searchPage(t, service, SearchRequest{Query: "witcher 3", Categories: []Category{CategoryExpansion}})
		assertProviderSearches(t, source, 2)
		assertTitles(t, hitTitles(page.Hits), bloodAndWine.Title, heartsOfStone.Title)
		assertChildHits(t, page.Hits, KindExpansion, witcher.Title)
	})
	t.Run("stale searches are refreshed", func(t *testing.T) {
		expireSearches(t, service)
		searchPage(t, service, SearchRequest{Query: "witcher 3"})
		assertProviderSearches(t, source, 3)
	})
	t.Run("follow-up pages never contact the provider", func(t *testing.T) {
		testFollowUpPagesStayLocal(t, service, source)
	})
}

func testFollowUpPagesStayLocal(t *testing.T, service *Service, source *fakeSource) {
	filtered := SearchRequest{Query: "witcher 3", Categories: []Category{CategoryExpansion}, Limit: 1}
	first := searchPage(t, service, filtered)
	expireSearches(t, service)
	before := providerSearches(source)
	filtered.Cursor = first.NextCursor
	second := searchPage(t, service, filtered)
	assertProviderSearches(t, source, before)
	assertTitles(t, hitTitles(second.Hits), heartsOfStone.Title)
	assertTitles(t, hitTitles(first.Hits), bloodAndWine.Title)
}

// rankFixture holds stored games for the local search tests, keyed by title.
type rankFixture struct {
	store   *Store
	db      *sql.DB
	service *Service
	stored  map[string]Game
}

func newRankFixture(t *testing.T) rankFixture {
	store, db := newTestStore(t)
	switchPlatform := SourcePlatform{ExternalID: "130", Name: "Nintendo Switch"}
	fixture := rankFixture{store: store, db: db, service: newTestService(store, nil, testPolicy), stored: map[string]Game{}}
	for _, game := range []SourceGame{
		{Source: "igdb", ExternalID: "1", Title: "The Legend of Zelda", Kind: KindGame},
		{Source: "igdb", ExternalID: "2", Title: "Zelda II: The Adventure of Link", Kind: KindGame},
		{Source: "igdb", ExternalID: "3", Title: "Zelda", Kind: KindGame, Platforms: []SourcePlatform{switchPlatform}},
		{Source: "igdb", ExternalID: "4", Title: "Zelda Classic", Kind: KindMod, ParentExternalID: "1"},
		{Source: "igdb", ExternalID: "5", Title: "The Legend of Zelda: Breath of the Wild", Kind: KindGame, Platforms: []SourcePlatform{switchPlatform}},
		{Source: "igdb", ExternalID: "6", Title: "Link's Awakening", Kind: KindGame},
		{Source: "igdb", ExternalID: "7", Title: "100% Orange_Juice", Kind: KindGame},
	} {
		fixture.stored[game.Title] = upsertOne(t, store, game)
	}
	return fixture
}

func (f rankFixture) titles(t *testing.T, request SearchRequest) []string {
	t.Helper()
	return hitTitles(searchPage(t, f.service, request).Hits)
}

func TestSearchRanksFiltersAndPaginatesStoredGames(t *testing.T) {
	fixture := newRankFixture(t)
	t.Run("hits rank by their best match: exact, prefix, then word", fixture.testRanking)
	t.Run("pages follow the same order without gaps or duplicates", fixture.testPagination)
	t.Run("cursors only continue the search they were issued for", fixture.testCursorBinding)
	t.Run("platform and kind filters", fixture.testFilters)
	t.Run("wildcards in queries match literally", func(t *testing.T) {
		assertTitles(t, fixture.titles(t, SearchRequest{Query: "0% orange_"}), "100% Orange_Juice")
		assertTitles(t, fixture.titles(t, SearchRequest{Query: "%_"}))
	})
	t.Run("provider matches without the query in their title are found", fixture.testProviderMatches)
	t.Run("inactive games are not found", func(t *testing.T) {
		execSQL(t, fixture.db, `UPDATE catalog.games SET is_active = false WHERE id = $1`, fixture.stored["Zelda"].ID)
		if slices.Contains(fixture.titles(t, SearchRequest{Query: "zelda"}), "Zelda") {
			t.Fatal("inactive game was found")
		}
	})
}

func (f rankFixture) testRanking(t *testing.T) {
	page := searchPage(t, f.service, SearchRequest{Query: "zelda"})
	// The Legend of Zelda ranks as a prefix match through its child Zelda Classic.
	assertTitles(t, hitTitles(page.Hits), "Zelda", "The Legend of Zelda", "Zelda II: The Adventure of Link", "The Legend of Zelda: Breath of the Wild")
	assertTitles(t, titles(page.Hits[1].MatchedChildren), "Zelda Classic")
	if page.NextCursor != "" {
		t.Fatal("single page has a next cursor")
	}
}

func (f rankFixture) testPagination(t *testing.T) {
	var collected []string
	request := SearchRequest{Query: "zelda", Limit: 1}
	for range 10 {
		page := searchPage(t, f.service, request)
		collected = append(collected, hitTitles(page.Hits)...)
		if len(page.NextCursor) > maxCursorLength {
			t.Fatalf("cursor exceeds %d bytes", maxCursorLength)
		}
		if request.Cursor = page.NextCursor; request.Cursor == "" {
			break
		}
	}
	assertTitles(t, collected, f.titles(t, SearchRequest{Query: "zelda"})...)
}

func (f rankFixture) testCursorBinding(t *testing.T) {
	cursor := searchPage(t, f.service, SearchRequest{Query: "zelda", Limit: 1}).NextCursor
	for _, request := range []SearchRequest{
		{Query: "link", Limit: 1, Cursor: cursor},
		{Query: "zelda", Limit: 1, Cursor: cursor, Categories: []Category{CategoryGame}},
		{Query: "zelda", Limit: 1, Cursor: cursor, PlatformID: f.stored["Zelda"].Platforms[0].ID},
	} {
		_, err := f.service.Search(t.Context(), request)
		assertErrorIs(t, err, ErrInvalidRequest)
	}
	assertTitles(t, f.titles(t, SearchRequest{Query: " ZELDA ", Limit: 1, Cursor: cursor}), "The Legend of Zelda")
}

func (f rankFixture) testFilters(t *testing.T) {
	platform := strings.ToUpper(f.stored["Zelda"].Platforms[0].ID)
	assertTitles(t, f.titles(t, SearchRequest{Query: "zelda", PlatformID: platform}), "Zelda", "The Legend of Zelda: Breath of the Wild")
	mods := searchPage(t, f.service, SearchRequest{Query: "zelda", Categories: []Category{CategoryMod}}).Hits
	assertTitles(t, hitTitles(mods), "Zelda Classic")
	assertChildHits(t, mods, KindMod, "The Legend of Zelda")
	// Filtered hits are not bundled, so The Legend of Zelda keeps its own rank.
	assertTitles(t, f.titles(t, SearchRequest{Query: "zelda", Categories: []Category{CategoryGame}}),
		"Zelda", "Zelda II: The Adventure of Link", "The Legend of Zelda", "The Legend of Zelda: Breath of the Wild")
}

func (f rankFixture) testProviderMatches(t *testing.T) {
	query, err := newLocalQuery(SearchRequest{Query: "botw", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.recordSearch(t.Context(), query.key, []string{f.stored["The Legend of Zelda: Breath of the Wild"].ID}); err != nil {
		t.Fatal(err)
	}
	assertTitles(t, f.titles(t, SearchRequest{Query: "BotW"}), "The Legend of Zelda: Breath of the Wild")
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
	assertTitles(t, hitTitles(page.Hits), witcher.Title)
	assertProviderSearches(t, source, 2)
	unstored := SearchRequest{Query: "elden ring", Limit: 20}
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
			_, err := service.Search(t.Context(), unstored)
			assertErrorIs(t, err, ErrUnavailable)
			assertProviderSearches(t, source, step.searches)
		})
	}
	t.Run("details and batches stay readable", func(t *testing.T) {
		testReadsWithoutProvider(t, service, source, page.Hits[0].Game.ID)
	})
	t.Run("recovered provider refreshes again", func(t *testing.T) {
		source.err = nil
		now = now.Add(testPolicy.Cooldown + time.Second)
		searchPage(t, service, unstored)
		assertProviderSearches(t, source, 4)
	})
}

func testReadsWithoutProvider(t *testing.T, service *Service, source *fakeSource, id string) {
	calls := source.calls
	if _, err := service.Detail(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	batch, err := service.Batch(t.Context(), []string{id})
	if err != nil || len(batch.Games) != 1 {
		t.Fatalf("batch = %+v, %v", batch, err)
	}
	if source.calls != calls {
		t.Fatal("details or batches contacted the provider")
	}
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
		_, err := service.Search(t.Context(), SearchRequest{Query: "witcher 3", Limit: 20})
		assertErrorIs(t, err, ErrUnavailable)
	}
	close(source.release)
	service.Wait()
	assertTitles(t, hitTitles(searchPage(t, service, SearchRequest{Query: "witcher 3"}).Hits), witcher.Title)
	if source.searches.Load() != 1 {
		t.Fatalf("provider searches = %d, want one shared refresh", source.searches.Load())
	}
}

func TestBatchClassifiesEveryRequestedIDOnce(t *testing.T) {
	store, db := newTestStore(t)
	games, err := store.Upsert(t.Context(), []SourceGame{witcher, bloodAndWine, heartsOfStone})
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, db, `UPDATE catalog.games SET is_active = false WHERE id = $1`, games[2].ID)
	service := newTestService(store, nil, testPolicy)
	missing := "00000000-0000-4000-8000-000000000000"

	batch, err := service.Batch(t.Context(), []string{missing, strings.ToUpper(games[1].ID), games[2].ID, games[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	assertTitles(t, titles(batch.Games), bloodAndWine.Title, witcher.Title)
	assertTitles(t, []string{batch.Games[0].ID, batch.Games[0].Parent.ID}, games[1].ID, games[0].ID)
	assertTitles(t, batch.Missing, missing)
	assertTitles(t, batch.Inactive, games[2].ID)

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
			_, err := service.Batch(t.Context(), tc.ids)
			assertErrorIs(t, err, ErrInvalidRequest)
		})
	}
	t.Run("details of inactive games stay readable", func(t *testing.T) {
		detail, err := service.Detail(t.Context(), strings.ToUpper(games[2].ID))
		if err != nil || detail.Game.IsActive || detail.Game.ID != games[2].ID {
			t.Fatalf("detail = %+v, %v", detail, err)
		}
		_, err = service.Detail(t.Context(), missing)
		assertErrorIs(t, err, ErrNotFound)
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
