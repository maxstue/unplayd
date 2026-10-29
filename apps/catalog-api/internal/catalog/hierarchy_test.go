package catalog

import (
	"errors"
	"slices"
	"sync"
	"testing"
)

// Fixtures follow the live IGDB relations of The Witcher 3 and Resident Evil 2.
var (
	witcher       = SourceGame{Source: "igdb", ExternalID: "1942", Title: "The Witcher 3: Wild Hunt", Kind: KindGame}
	bloodAndWine  = SourceGame{Source: "igdb", ExternalID: "13166", Title: "The Witcher 3: Wild Hunt - Blood and Wine", Kind: KindExpansion, ParentExternalID: "1942"}
	heartsOfStone = SourceGame{Source: "igdb", ExternalID: "12503", Title: "The Witcher 3: Wild Hunt - Hearts of Stone", Kind: KindExpansion, ParentExternalID: "1942"}
	nextGen       = SourceGame{Source: "igdb", ExternalID: "230102", Title: "The Witcher 3: Wild Hunt - Free Next-Gen Update", Kind: KindUpdate, ParentExternalID: "1942"}
	goty          = SourceGame{Source: "igdb", ExternalID: "22439", Title: "The Witcher 3: Wild Hunt - Game of the Year Edition", Kind: KindEdition, ParentExternalID: "1942"}
	complete      = SourceGame{Source: "igdb", ExternalID: "119402", Title: "The Witcher 3: Wild Hunt - Complete Edition", Kind: KindEdition, ParentExternalID: "1942"}
	anniversary   = SourceGame{Source: "igdb", ExternalID: "372654", Title: "The Witcher 3: Wild Hunt - Complete Edition: 10th Anniversary Edition", Kind: KindEdition, ParentExternalID: "119402"}
	witcherBundle = SourceGame{Source: "igdb", ExternalID: "141472", Title: "The Witcher 3: Wild Hunt + Dark Souls III", Kind: KindBundle}
	re2           = SourceGame{Source: "igdb", ExternalID: "880", Title: "Resident Evil 2", Kind: KindGame}
	re2Remake     = SourceGame{Source: "igdb", ExternalID: "19686", Title: "Resident Evil 2", Kind: KindRemake, ParentExternalID: "880"}
)

func withParent(game SourceGame, parent string) SourceGame {
	game.ParentExternalID = parent
	return game
}

func byTitle(games []Game) map[string]Game {
	result := map[string]Game{}
	for _, game := range games {
		result[game.Title] = game
	}
	return result
}

func assertParent(t *testing.T, game Game, parent *Game) {
	t.Helper()
	switch {
	case parent == nil && game.Parent != nil:
		t.Fatalf("%s has parent %+v, want none", game.Title, game.Parent)
	case parent != nil && (game.Parent == nil || game.Parent.ID != parent.ID || game.Parent.Title != parent.Title):
		t.Fatalf("%s has parent %+v, want %s %s", game.Title, game.Parent, parent.ID, parent.Title)
	}
}

func TestUpsertLinksChildrenToTopParentInAnyOrder(t *testing.T) {
	store, db := newTestStore(t)
	// Children precede their parents and the anniversary edition is nested two levels deep.
	games, err := store.Upsert(t.Context(), []SourceGame{anniversary, bloodAndWine, complete, witcher, witcherBundle})
	if err != nil {
		t.Fatal(err)
	}
	stored := byTitle(games)
	parent := stored[witcher.Title]
	assertParent(t, parent, nil)
	assertParent(t, stored[witcherBundle.Title], nil)
	for _, child := range []SourceGame{anniversary, bloodAndWine, complete} {
		assertParent(t, stored[child.Title], &parent)
		if stored[child.Title].Kind != child.Kind {
			t.Fatalf("%s kind = %q, want %q", child.Title, stored[child.Title].Kind, child.Kind)
		}
	}
	assertCount(t, db, 0, `SELECT count(*) FROM catalog.games c JOIN catalog.games p ON p.id = c.parent_id WHERE p.parent_id IS NOT NULL`)

	t.Run("a stored parent links later children", func(t *testing.T) {
		games, err := store.Upsert(t.Context(), []SourceGame{heartsOfStone})
		if err != nil {
			t.Fatal(err)
		}
		assertParent(t, games[0], &parent)
	})
	t.Run("a stored child as parent links to its top", func(t *testing.T) {
		nested := SourceGame{Source: "igdb", ExternalID: "900001", Title: "Complete Edition Soundtrack", Kind: KindPack, ParentExternalID: complete.ExternalID}
		games, err := store.Upsert(t.Context(), []SourceGame{nested})
		if err != nil {
			t.Fatal(err)
		}
		assertParent(t, games[0], &parent)
	})
}

func TestUpsertRejectsChildrenWithoutParentAtomically(t *testing.T) {
	store, db := newTestStore(t)
	if _, err := store.Upsert(t.Context(), []SourceGame{witcher, re2Remake}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid request for a missing parent, got %v", err)
	}
	if _, err := store.Upsert(t.Context(), []SourceGame{withParent(witcher, witcher.ExternalID)}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid request for a self reference, got %v", err)
	}
	unknown := witcher
	unknown.Kind = "sequel"
	if _, err := store.Upsert(t.Context(), []SourceGame{unknown}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid request for an unknown kind, got %v", err)
	}
	assertCount(t, db, 0, `SELECT count(*) FROM catalog.games`)
}

func TestUpsertStoresEveryKind(t *testing.T) {
	store, _ := newTestStore(t)
	batch := []SourceGame{witcher}
	for index, kind := range kinds[1:] {
		batch = append(batch, SourceGame{Source: "igdb", ExternalID: "kind-" + string(kind), Title: string(kind), Kind: kind, ParentExternalID: witcher.ExternalID})
		if index%2 == 0 {
			batch[len(batch)-1].ParentExternalID = ""
		}
	}
	games, err := store.Upsert(t.Context(), batch)
	if err != nil {
		t.Fatal(err)
	}
	for index, game := range games {
		if game.Kind != batch[index].Kind {
			t.Fatalf("%s kind = %q, want %q", game.Title, game.Kind, batch[index].Kind)
		}
	}
}

func TestUpsertReparentsWithoutChangingIdentity(t *testing.T) {
	store, db := newTestStore(t)
	first := byTitle(must(store.Upsert(t.Context(), []SourceGame{witcher, complete, bloodAndWine})))

	t.Run("unchanged relations keep timestamps", func(t *testing.T) {
		again := byTitle(must(store.Upsert(t.Context(), []SourceGame{bloodAndWine, complete, witcher})))
		assertUnchanged(t, first, again)
	})
	t.Run("detached child becomes a parent", func(t *testing.T) {
		games := must(store.Upsert(t.Context(), []SourceGame{withParent(complete, "")}))
		if games[0].ID != first[complete.Title].ID || !games[0].UpdatedAt.After(first[complete.Title].UpdatedAt) {
			t.Fatalf("detaching kept no identity or did not advance updatedAt: %+v", games[0])
		}
		assertParent(t, games[0], nil)
	})
	t.Run("kind change advances the timestamp", func(t *testing.T) {
		changed := bloodAndWine
		changed.Kind = KindDLC
		games := must(store.Upsert(t.Context(), []SourceGame{changed}))
		if games[0].ID != first[bloodAndWine.Title].ID || games[0].Kind != KindDLC || !games[0].UpdatedAt.After(first[bloodAndWine.Title].UpdatedAt) {
			t.Fatalf("kind change: %+v", games[0])
		}
	})
	t.Run("a parent that becomes a child hands its children to the new top", func(t *testing.T) {
		collection := SourceGame{Source: "igdb", ExternalID: "900002", Title: "The Witcher Collection", Kind: KindGame}
		games := byTitle(must(store.Upsert(t.Context(), []SourceGame{collection, withParent(witcher, collection.ExternalID)})))
		top := games[collection.Title]
		assertParent(t, games[witcher.Title], &top)
		if games[witcher.Title].ID != first[witcher.Title].ID {
			t.Fatal("re-parenting changed the parent's UUID")
		}
		titles := slices.Concat(groupedTitles(t, must(store.Detail(t.Context(), top.ID)), &top)...)
		if !slices.Contains(titles, bloodAndWine.Title) || !slices.Contains(titles, witcher.Title) {
			t.Fatalf("collection children = %v", titles)
		}
	})
	t.Run("swapped parent and child", func(t *testing.T) {
		games := byTitle(must(store.Upsert(t.Context(), []SourceGame{withParent(witcher, ""), withParent(SourceGame{Source: "igdb", ExternalID: "900002", Title: "The Witcher Collection", Kind: KindGame}, witcher.ExternalID)})))
		top := games[witcher.Title]
		assertParent(t, top, nil)
		assertParent(t, games["The Witcher Collection"], &top)
	})
	assertCount(t, db, 4, `SELECT count(*) FROM catalog.games`)
	assertCount(t, db, 0, `SELECT count(*) FROM catalog.games c JOIN catalog.games p ON p.id = c.parent_id WHERE p.parent_id IS NOT NULL`)
}

func TestUpsertLeavesCyclicChainsWithoutParent(t *testing.T) {
	store, _ := newTestStore(t)
	games, err := store.Upsert(t.Context(), []SourceGame{withParent(re2, re2Remake.ExternalID), re2Remake})
	if err != nil {
		t.Fatal(err)
	}
	for _, game := range games {
		assertParent(t, game, nil)
	}
}

func TestConcurrentImportsOfRelatedGamesKeepOneHierarchy(t *testing.T) {
	store, db := newTestStore(t)
	batches := [][]SourceGame{
		{bloodAndWine, witcher},
		{witcher, heartsOfStone, goty},
		{anniversary, complete, witcher},
		{nextGen, witcher, bloodAndWine},
	}
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			if _, err := store.Upsert(t.Context(), batches[i%len(batches)]); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	assertCount(t, db, 7, `SELECT count(*) FROM catalog.games`)
	assertCount(t, db, 7, `SELECT count(*) FROM catalog.external_game_ids`)
	assertCount(t, db, 6, `SELECT count(*) FROM catalog.games c JOIN catalog.external_game_ids e ON e.game_id = c.parent_id
		WHERE e.source = 'igdb' AND e.external_id = '1942'`)
}

func TestChildCountsAndDetailGroupChildrenByKind(t *testing.T) {
	store, db := newTestStore(t)
	stored := must(store.Upsert(t.Context(), []SourceGame{witcher, goty, complete, bloodAndWine, heartsOfStone, nextGen, re2, re2Remake}))
	// Original and remake share their title, so they are taken by position.
	original, remake := stored[6], stored[7]
	games := byTitle(stored[:6])
	parent := games[witcher.Title]
	if _, err := db.ExecContext(t.Context(), `UPDATE catalog.games SET is_active = false WHERE id = $1`, games[goty.Title].ID); err != nil {
		t.Fatal(err)
	}
	counts, err := store.ChildCounts(t.Context(), []string{parent.ID, original.ID, remake.ID})
	if err != nil {
		t.Fatal(err)
	}
	want := []KindCount{{KindEdition, 1}, {KindExpansion, 2}, {KindUpdate, 1}}
	if !slices.Equal(counts[parent.ID], want) {
		t.Fatalf("witcher counts = %+v, want %+v", counts[parent.ID], want)
	}
	if !slices.Equal(counts[original.ID], []KindCount{{KindRemake, 1}}) || counts[remake.ID] != nil {
		t.Fatalf("resident evil counts = %+v", counts)
	}

	t.Run("parent detail", func(t *testing.T) {
		detail, err := store.Detail(t.Context(), parent.ID)
		if err != nil {
			t.Fatal(err)
		}
		got := groupedTitles(t, detail, &parent)
		want := [][]string{
			{complete.Title},
			{bloodAndWine.Title, heartsOfStone.Title},
			{nextGen.Title},
		}
		if detail.Game.ID != parent.ID || !slices.Equal(groupKinds(detail), []Kind{KindEdition, KindExpansion, KindUpdate}) || !slices.EqualFunc(got, want, slices.Equal) {
			t.Fatalf("detail children = %v, want %v", got, want)
		}
	})
	t.Run("child detail", func(t *testing.T) {
		detail, err := store.Detail(t.Context(), remake.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertParent(t, detail.Game, &original)
		if detail.Game.Kind != KindRemake || len(detail.Children) != 0 {
			t.Fatalf("child detail = %+v", detail)
		}
	})
	t.Run("unknown and malformed IDs", func(t *testing.T) {
		for _, id := range []string{"10000000-0000-4000-8000-000000000099", "not-a-uuid"} {
			if _, err := store.Detail(t.Context(), id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("Detail(%q) err = %v, want ErrNotFound", id, err)
			}
		}
	})
}

func TestImporterCompletesChainsAndLoadsChildrenWithoutNPlusOne(t *testing.T) {
	store, db := newTestStore(t)
	source := &fakeSource{
		games: []SourceGame{anniversary, re2Remake},
		known: []SourceGame{witcher, complete, bloodAndWine, heartsOfStone, goty, nextGen, re2},
	}
	games, err := NewImporter(source, store).Import(t.Context(), []string{anniversary.ExternalID, re2Remake.ExternalID})
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 || games[0].Title != anniversary.Title || games[1].Kind != KindRemake {
		t.Fatalf("import returned %+v", games)
	}
	// Details, two ancestor levels, then one children request per hierarchy level.
	if want := []string{"games", "games", "games", "children", "children"}; !slices.Equal(source.requests, want) {
		t.Fatalf("provider requests = %v, want %v", source.requests, want)
	}
	assertCount(t, db, 9, `SELECT count(*) FROM catalog.games`)
	assertCount(t, db, 7, `SELECT count(*) FROM catalog.games WHERE parent_id IS NOT NULL`)
}

func TestImporterDetachesGamesWhoseParentIsUnknown(t *testing.T) {
	store, _ := newTestStore(t)
	orphan := SourceGame{Source: "igdb", ExternalID: "77", Title: "Orphaned Remaster", Kind: KindRemaster, ParentExternalID: "404"}
	stray := SourceGame{Source: "igdb", ExternalID: "78", Title: "Edition of another game", Kind: KindEdition, ParentExternalID: "405"}
	source := &fakeSource{games: []SourceGame{orphan}, known: []SourceGame{withParent(stray, "77")}}
	games, err := NewImporter(source, store).Import(t.Context(), []string{"77"})
	if err != nil {
		t.Fatal(err)
	}
	assertParent(t, games[0], nil)
	if games[0].Kind != KindRemaster {
		t.Fatalf("kind = %q", games[0].Kind)
	}
	source = &fakeSource{games: []SourceGame{witcher}, known: []SourceGame{stray}}
	if _, err := NewImporter(source, store).Import(t.Context(), []string{witcher.ExternalID}); err != nil {
		t.Fatalf("children of other parents must be skipped: %v", err)
	}
}

var searchable = []SourceGame{witcher, goty, complete, anniversary, bloodAndWine, heartsOfStone, nextGen, witcherBundle, re2, re2Remake}

func searchHits(t *testing.T, store *Store, matches []SourceGame, categories ...Category) []SearchHit {
	t.Helper()
	source := &fakeSource{games: matches, known: searchable}
	hits, err := NewImporter(source, store).Search(t.Context(), "witcher 3", categories, 20)
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

func TestImporterSearchBundlesHitsUnderParents(t *testing.T) {
	store, _ := newTestStore(t)

	t.Run("without filter every parent appears once with matched children", func(t *testing.T) {
		hits := searchHits(t, store, []SourceGame{witcher, witcherBundle, nextGen, goty, bloodAndWine, heartsOfStone, anniversary})
		if !slices.Equal(hitTitles(hits), []string{witcher.Title, witcherBundle.Title}) {
			t.Fatalf("hits = %+v", hits)
		}
		if want := []string{nextGen.Title, goty.Title, bloodAndWine.Title, heartsOfStone.Title, anniversary.Title}; !slices.Equal(titles(hits[0].MatchedChildren), want) {
			t.Fatalf("matched children = %v, want %v", titles(hits[0].MatchedChildren), want)
		}
		want := []KindCount{{KindEdition, 3}, {KindExpansion, 2}, {KindUpdate, 1}}
		if !slices.Equal(hits[0].ChildCounts, want) || len(hits[1].ChildCounts) != 0 {
			t.Fatalf("child counts = %+v / %+v, want %+v", hits[0].ChildCounts, hits[1].ChildCounts, want)
		}
	})
	t.Run("a child-only match still yields its parent", func(t *testing.T) {
		hits := searchHits(t, store, []SourceGame{bloodAndWine})
		if !slices.Equal(hitTitles(hits), []string{witcher.Title}) || !slices.Equal(titles(hits[0].MatchedChildren), []string{bloodAndWine.Title}) {
			t.Fatalf("hits = %+v", hits)
		}
	})
	t.Run("remake bundles under the original", func(t *testing.T) {
		hits := searchHits(t, store, []SourceGame{re2Remake, re2})
		if len(hits) != 1 || hits[0].Game.Kind != KindGame || hits[0].MatchedChildren[0].Kind != KindRemake {
			t.Fatalf("hits = %+v", hits)
		}
	})
}

func TestImporterSearchFiltersByKind(t *testing.T) {
	store, _ := newTestStore(t)

	t.Run("kind filter yields a flat list with parent references", func(t *testing.T) {
		hits := searchHits(t, store, []SourceGame{bloodAndWine, heartsOfStone}, CategoryExpansion)
		if len(hits) != 2 {
			t.Fatalf("hits = %+v", hits)
		}
		assertChildHits(t, hits, KindExpansion, witcher.Title)
	})
	t.Run("filters are forwarded and applied locally", func(t *testing.T) {
		source := &fakeSource{games: []SourceGame{witcher, nextGen, bloodAndWine}, known: searchable}
		hits := must(NewImporter(source, store).Search(t.Context(), "witcher 3", []Category{CategoryDLC, CategoryDLC}, 20))
		if !slices.Equal(source.filter.Kinds, []Kind{KindDLC, KindPack, KindUpdate}) || source.filter.Parents {
			t.Fatalf("provider filter = %+v", source.filter)
		}
		if !slices.Equal(hitTitles(hits), []string{nextGen.Title}) {
			t.Fatalf("dlc hits = %+v", hits)
		}
	})
	t.Run("game filter yields parents with counts only", func(t *testing.T) {
		hits := searchHits(t, store, []SourceGame{witcher, witcherBundle, bloodAndWine}, CategoryGame)
		if !slices.Equal(hitTitles(hits), []string{witcher.Title, witcherBundle.Title}) || len(hits[0].ChildCounts) == 0 {
			t.Fatalf("game hits = %+v", hits)
		}
	})
}

// assertChildHits checks that every hit is a flat child of kind under parentTitle.
func assertChildHits(t *testing.T, hits []SearchHit, kind Kind, parentTitle string) {
	t.Helper()
	for _, hit := range hits {
		if hit.Game.Kind != kind || hit.Game.Parent == nil || hit.Game.Parent.Title != parentTitle || len(hit.MatchedChildren) != 0 {
			t.Fatalf("%s hit = %+v", kind, hit)
		}
	}
}

func groupKinds(detail Detail) []Kind {
	result := []Kind{}
	for _, group := range detail.Children {
		result = append(result, group.Kind)
	}
	return result
}

func assertUnchanged(t *testing.T, before, after map[string]Game) {
	t.Helper()
	for title, game := range before {
		if after[title].ID != game.ID || !after[title].UpdatedAt.Equal(game.UpdatedAt) {
			t.Fatalf("%s changed identity or timestamp", title)
		}
	}
}

// groupedTitles returns the child titles of each kind group and checks that
// every child references parent.
func groupedTitles(t *testing.T, detail Detail, parent *Game) [][]string {
	t.Helper()
	var groups [][]string
	for _, group := range detail.Children {
		for _, child := range group.Games {
			assertParent(t, child, parent)
		}
		groups = append(groups, titles(group.Games))
	}
	return groups
}

func titles(games []Game) []string {
	result := []string{}
	for _, game := range games {
		result = append(result, game.Title)
	}
	return result
}

func hitTitles(hits []SearchHit) []string {
	result := []string{}
	for _, hit := range hits {
		result = append(result, hit.Game.Title)
	}
	return result
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
