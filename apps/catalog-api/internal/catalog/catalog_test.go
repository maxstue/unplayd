package catalog

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// fakeSource answers searches with games and looks up details and children
// in games plus known, like a provider database.
type fakeSource struct {
	games       []SourceGame
	known       []SourceGame
	err         error
	query       string
	filter      SearchFilter
	limit       int
	externalIDs []string
	calls       int
	requests    []string
}

func (f *fakeSource) Search(_ context.Context, query string, filter SearchFilter, limit int) ([]SourceGame, error) {
	f.calls++
	f.requests = append(f.requests, "search")
	f.query, f.filter, f.limit = query, filter, limit
	return f.games, f.err
}

func (f *fakeSource) Games(_ context.Context, externalIDs []string) ([]SourceGame, error) {
	f.calls++
	f.requests = append(f.requests, "games")
	if f.externalIDs == nil {
		f.externalIDs = externalIDs
	}
	return f.lookup(func(game SourceGame) bool { return slices.Contains(externalIDs, game.ExternalID) }), f.err
}

func (f *fakeSource) Children(_ context.Context, parentIDs []string) ([]SourceGame, error) {
	f.calls++
	f.requests = append(f.requests, "children")
	return f.lookup(func(game SourceGame) bool { return slices.Contains(parentIDs, game.ParentExternalID) }), f.err
}

func (f *fakeSource) lookup(match func(SourceGame) bool) []SourceGame {
	var found []SourceGame
	for _, game := range slices.Concat(f.games, f.known) {
		if match(game) && !slices.ContainsFunc(found, func(g SourceGame) bool { return g.ExternalID == game.ExternalID }) {
			found = append(found, game)
		}
	}
	return found
}

func TestImporterSearchStoresProviderMatches(t *testing.T) {
	store, db := newTestStore(t)
	source := &fakeSource{games: []SourceGame{zelda}}
	importer := NewImporter(source, store)
	hits, err := importer.Search(t.Context(), "  zelda  ", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if source.query != "zelda" || source.limit != 20 || !source.filter.empty() {
		t.Fatalf("provider received query %q, filter %+v and limit %d", source.query, source.filter, source.limit)
	}
	if len(hits) != 1 || hits[0].Game.Title != zelda.Title || hits[0].Game.CoverURL == nil || len(hits[0].Game.Platforms) != 2 {
		t.Fatalf("unexpected search result: %+v", hits)
	}
	games := []Game{hits[0].Game}
	again, err := importer.Search(t.Context(), "zelda", nil, 20)
	if err != nil || again[0].Game.ID != games[0].ID {
		t.Fatalf("repeated search changed identity: %+v, %v", again, err)
	}
	assertCount(t, db, 1, `SELECT count(*) FROM catalog.games`)

	t.Run("import by external ID reuses the catalog entry", func(t *testing.T) {
		imported, err := importer.Import(t.Context(), []string{"7346"})
		if err != nil || len(imported) != 1 || imported[0].ID != games[0].ID {
			t.Fatalf("import = %+v, %v", imported, err)
		}
		if len(source.externalIDs) != 1 || source.externalIDs[0] != "7346" {
			t.Fatalf("provider received IDs %v", source.externalIDs)
		}
	})
}

func TestImporterRejectsInvalidRequestsWithoutCallingProvider(t *testing.T) {
	source := &fakeSource{}
	importer := NewImporter(source, nil)
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"query too short", func() error { _, err := importer.Search(t.Context(), " z ", nil, 20); return err }},
		{"query too long", func() error { _, err := importer.Search(t.Context(), strings.Repeat("ü", 201), nil, 20); return err }},
		{"zero limit", func() error { _, err := importer.Search(t.Context(), "zelda", nil, 0); return err }},
		{"limit above maximum", func() error { _, err := importer.Search(t.Context(), "zelda", nil, 51); return err }},
		{"unknown kind filter", func() error {
			_, err := importer.Search(t.Context(), "zelda", []Category{"sequel"}, 20)
			return err
		}},
		{"empty import", func() error { _, err := importer.Import(t.Context(), nil); return err }},
		{"import above maximum", func() error { _, err := importer.Import(t.Context(), make([]string, 101)); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected invalid request, got %v", err)
			}
		})
	}
	if source.calls != 0 {
		t.Fatal("invalid requests reached the provider")
	}
}

func TestImporterAcceptsQueryAndLimitBoundaries(t *testing.T) {
	source := &fakeSource{}
	importer := NewImporter(source, NewStore(nil))
	for _, tc := range []struct {
		query string
		limit int
	}{
		{"ab", 1},
		{strings.Repeat("ü", 200), 50},
	} {
		if games, err := importer.Search(t.Context(), tc.query, nil, tc.limit); err != nil || len(games) != 0 {
			t.Fatalf("boundary search rejected: %v", err)
		}
	}
	if games, err := importer.Import(t.Context(), make([]string, 100)); err != nil || len(games) != 0 {
		t.Fatalf("boundary import rejected: %v", err)
	}
}

func TestImporterPropagatesProviderFailures(t *testing.T) {
	failure := errors.New("provider unavailable")
	importer := NewImporter(&fakeSource{err: failure}, nil)
	if _, err := importer.Search(t.Context(), "zelda", nil, 20); !errors.Is(err, failure) {
		t.Fatalf("search err = %v", err)
	}
	if _, err := importer.Import(t.Context(), []string{"1"}); !errors.Is(err, failure) {
		t.Fatalf("import err = %v", err)
	}
}
