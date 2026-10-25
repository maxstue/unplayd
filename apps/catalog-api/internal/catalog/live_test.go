package catalog_test

import (
	"os"
	"slices"
	"testing"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
	"github.com/maxstue/unplayd/apps/catalog-api/internal/igdb"
)

// TestLiveIGDBBundling checks the bundling against the real IGDB API. It only
// runs with IGDB_LIVE=1 and IGDB credentials, for example:
//
//	IGDB_LIVE=1 infisical run --env=dev --path=/Catalog_API -- go test -run TestLiveIGDBBundling -v ./internal/catalog/
func TestLiveIGDBBundling(t *testing.T) {
	if os.Getenv("IGDB_LIVE") != "1" {
		t.Skip("set IGDB_LIVE=1 with IGDB credentials to check against the real IGDB API")
	}
	config, err := igdb.Load(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	client, err := igdb.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := catalog.NewTestStore(t)
	importer := catalog.NewImporter(client, store)
	search := func(t *testing.T, query string, categories ...catalog.Category) []catalog.SearchHit {
		t.Helper()
		hits, err := importer.Search(t.Context(), query, categories, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range hits {
			parent := ""
			if hit.Game.Parent != nil {
				parent = " · " + string(hit.Game.Kind) + " of " + hit.Game.Parent.Title
			}
			t.Logf("%s [%s]%s counts=%v matched=%d", hit.Game.Title, hit.Game.Kind, parent, hit.ChildCounts, len(hit.MatchedChildren))
		}
		return hits
	}
	for _, tc := range []struct {
		query, title string
		kinds        []catalog.Kind
	}{
		{"witcher 3", "The Witcher 3: Wild Hunt", []catalog.Kind{catalog.KindEdition, catalog.KindExpansion}},
		{"zelda breath of the wild", "The Legend of Zelda: Breath of the Wild", []catalog.Kind{catalog.KindEdition, catalog.KindDLC}},
		{"elden ring", "Elden Ring", []catalog.Kind{catalog.KindEdition, catalog.KindExpansion}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			hits := search(t, tc.query)
			assertBundled(t, hits)
			main := hitsTitled(hits, tc.title)
			if len(main) != 1 {
				t.Fatalf("%q yields %d hits for %q, want exactly one", tc.query, len(main), tc.title)
			}
			for _, kind := range tc.kinds {
				if !slices.ContainsFunc(main[0].ChildCounts, func(count catalog.KindCount) bool { return count.Kind == kind }) {
					t.Errorf("%s has no %s children: %v", tc.title, kind, main[0].ChildCounts)
				}
			}
		})
	}
	t.Run("resident evil 2", func(t *testing.T) {
		hits := search(t, "resident evil 2")
		assertBundled(t, hits)
		if !slices.ContainsFunc(hitsTitled(hits, "Resident Evil 2"), func(hit catalog.SearchHit) bool {
			return slices.ContainsFunc(hit.MatchedChildren, func(child catalog.Game) bool { return child.Kind == catalog.KindRemake })
		}) {
			t.Fatal("the Resident Evil 2 remake is not bundled under the original")
		}
	})
	for _, tc := range []struct {
		category catalog.Category
		kinds    []catalog.Kind
	}{
		{catalog.CategoryExpansion, []catalog.Kind{catalog.KindExpansion, catalog.KindStandaloneExpansion}},
		{catalog.CategoryDLC, []catalog.Kind{catalog.KindDLC, catalog.KindPack, catalog.KindUpdate}},
	} {
		t.Run("witcher 3 "+string(tc.category), func(t *testing.T) {
			hits := search(t, "witcher 3", tc.category)
			if len(hits) == 0 {
				t.Fatal("no hits")
			}
			for _, hit := range hits {
				if !slices.Contains(tc.kinds, hit.Game.Kind) || hit.Game.Parent == nil || len(hit.MatchedChildren) != 0 {
					t.Errorf("unexpected %s hit: %+v", tc.category, hit.Game)
				}
			}
		})
	}
}

// assertBundled checks that every game appears once and only parents are hits.
func assertBundled(t *testing.T, hits []catalog.SearchHit) {
	t.Helper()
	seen := map[string]bool{}
	for _, hit := range hits {
		if hit.Game.Parent != nil {
			t.Errorf("child %s appears as its own hit", hit.Game.Title)
		}
		for _, game := range append([]catalog.Game{hit.Game}, hit.MatchedChildren...) {
			if seen[game.ID] {
				t.Errorf("%s appears more than once", game.Title)
			}
			seen[game.ID] = true
		}
	}
}

func hitsTitled(hits []catalog.SearchHit, title string) []catalog.SearchHit {
	var result []catalog.SearchHit
	for _, hit := range hits {
		if hit.Game.Title == title {
			result = append(result, hit)
		}
	}
	return result
}
