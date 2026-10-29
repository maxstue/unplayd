package catalog

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/database/databasetest"
)

// newTestStore starts PostgreSQL with the shared bootstrap and applies the
// production migrations as the restricted catalog role.
func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db := databasetest.Open(t)
	return NewStore(db), db
}

func assertCount(t *testing.T, db *sql.DB, want int, query string, args ...any) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(t.Context(), query, args...).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s = %d, want %d", query, got, want)
	}
}

func upsertOne(t *testing.T, store *Store, game SourceGame) Game {
	t.Helper()
	games, err := store.Upsert(t.Context(), []SourceGame{game})
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 {
		t.Fatalf("upsert returned %d games, want 1", len(games))
	}
	return games[0]
}

func platformNames(game Game) []string {
	names := make([]string, len(game.Platforms))
	for i, platform := range game.Platforms {
		names[i] = platform.Name
	}
	return names
}

var zelda = SourceGame{
	Source: "igdb", ExternalID: "7346", Title: "The Legend of Zelda: Breath of the Wild", Kind: KindGame,
	CoverURL:  "https://images.igdb.com/igdb/image/upload/t_cover_big/co3p2d.jpg",
	Platforms: []SourcePlatform{{ExternalID: "130", Name: "Nintendo Switch"}, {ExternalID: "41", Name: "Wii U"}},
}

func TestUpsertIsRepeatableAndKeepsCatalogIdentity(t *testing.T) {
	store, db := newTestStore(t)
	first := upsertOne(t, store, zelda)
	assertStoredZelda(t, first)

	t.Run("unchanged metadata keeps UUID and timestamp", func(t *testing.T) {
		again := upsertOne(t, store, zelda)
		if again.ID != first.ID || !again.UpdatedAt.Equal(first.UpdatedAt) {
			t.Fatalf("repeat upsert changed identity or timestamp: %+v vs %+v", again, first)
		}
		assertCount(t, db, 1, `SELECT count(*) FROM catalog.games`)
		assertCount(t, db, 1, `SELECT count(*) FROM catalog.external_game_ids WHERE source = 'igdb' AND external_id = '7346'`)
		assertCount(t, db, 2, `SELECT count(*) FROM catalog.platforms`)
		assertCount(t, db, 2, `SELECT count(*) FROM catalog.game_platforms`)
	})

	for _, tc := range []struct {
		name   string
		change func(*SourceGame)
	}{
		{"title change", func(g *SourceGame) { g.Title = "Zelda: BotW" }},
		{"cover removal", func(g *SourceGame) { g.CoverURL = "" }},
		{"platform removal", func(g *SourceGame) { g.Platforms = g.Platforms[:1] }},
		{"platform addition", func(g *SourceGame) {
			g.Platforms = append(g.Platforms, SourcePlatform{ExternalID: "6", Name: "PC (Microsoft Windows)"})
		}},
	} {
		t.Run(tc.name+" keeps UUID and advances timestamp", func(t *testing.T) {
			changed := zelda
			changed.Platforms = append([]SourcePlatform{}, zelda.Platforms...)
			tc.change(&changed)
			before := upsertOne(t, store, zelda)
			after := upsertOne(t, store, changed)
			if after.ID != first.ID || !after.UpdatedAt.After(before.UpdatedAt) {
				t.Fatalf("metadata change: id %s -> %s, updatedAt %v -> %v", first.ID, after.ID, before.UpdatedAt, after.UpdatedAt)
			}
			assertMatchesSource(t, after, changed)
			assertCount(t, db, 1, `SELECT count(*) FROM catalog.games`)
		})
	}
}

func assertStoredZelda(t *testing.T, game Game) {
	t.Helper()
	if game.Title != zelda.Title || game.CoverURL == nil || *game.CoverURL != zelda.CoverURL || !game.IsActive {
		t.Fatalf("unexpected stored game: %+v", game)
	}
	if names := platformNames(game); len(names) != 2 || names[0] != "Nintendo Switch" || names[1] != "Wii U" {
		t.Fatalf("platforms = %v", names)
	}
	if game.UpdatedAt.Location().String() != "UTC" {
		t.Fatal("updatedAt must be UTC")
	}
}

func assertMatchesSource(t *testing.T, game Game, source SourceGame) {
	t.Helper()
	if game.Title != source.Title || (game.CoverURL == nil) != (source.CoverURL == "") || len(game.Platforms) != len(source.Platforms) {
		t.Fatalf("stored metadata does not match the source: %+v", game)
	}
}

func TestUpsertReusesPlatformsAndUpdatesTheirNames(t *testing.T) {
	store, db := newTestStore(t)
	witcher := SourceGame{Source: "igdb", ExternalID: "1942", Title: "The Witcher 3", Kind: KindGame, Platforms: []SourcePlatform{{ExternalID: "6", Name: "PC"}}}
	portal := SourceGame{Source: "igdb", ExternalID: "72", Title: "Portal 2", Kind: KindGame, Platforms: []SourcePlatform{{ExternalID: "6", Name: "PC (Microsoft Windows)"}}}
	first := upsertOne(t, store, witcher)
	second := upsertOne(t, store, portal)
	if first.Platforms[0].ID != second.Platforms[0].ID {
		t.Fatal("the same external platform produced two catalog platforms")
	}
	if second.Platforms[0].Name != "PC (Microsoft Windows)" {
		t.Fatalf("platform name = %q, want latest source name", second.Platforms[0].Name)
	}
	assertCount(t, db, 1, `SELECT count(*) FROM catalog.platforms`)
	assertCount(t, db, 1, `SELECT count(*) FROM catalog.external_platform_ids WHERE source = 'igdb' AND external_id = '6'`)
}

func TestUpsertNormalizesBatches(t *testing.T) {
	store, db := newTestStore(t)
	duplicate := zelda
	duplicate.Title = "Ignored duplicate"
	other := SourceGame{Source: "steam", ExternalID: "7346", Title: "  Same ID from another source  ", Kind: KindGame,
		Platforms: []SourcePlatform{{ExternalID: "1", Name: "PC"}, {ExternalID: "1", Name: "PC"}}}
	games, err := store.Upsert(t.Context(), []SourceGame{zelda, duplicate, other})
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 || games[0].Title != zelda.Title || games[1].Title != "Same ID from another source" || games[0].ID == games[1].ID {
		t.Fatalf("unexpected batch result: %+v", games)
	}
	if len(games[1].Platforms) != 1 || games[1].CoverURL != nil {
		t.Fatalf("unexpected normalized game: %+v", games[1])
	}
	assertCount(t, db, 2, `SELECT count(*) FROM catalog.games`)
	if empty, err := store.Upsert(t.Context(), nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty upsert = %v, %v", empty, err)
	}
}

func TestUpsertRejectsInvalidSourceGamesAtomically(t *testing.T) {
	store, db := newTestStore(t)
	for _, tc := range []struct {
		name string
		game SourceGame
	}{
		{"missing source", SourceGame{ExternalID: "1", Title: "Game"}},
		{"missing external ID", SourceGame{Source: "igdb", Title: "Game"}},
		{"blank title", SourceGame{Source: "igdb", ExternalID: "1", Title: "  "}},
		{"platform without ID", SourceGame{Source: "igdb", ExternalID: "1", Title: "Game", Kind: KindGame, Platforms: []SourcePlatform{{Name: "PC"}}}},
		{"platform without name", SourceGame{Source: "igdb", ExternalID: "1", Title: "Game", Kind: KindGame, Platforms: []SourcePlatform{{ExternalID: "6"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.Upsert(t.Context(), []SourceGame{zelda, tc.game}); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected invalid request, got %v", err)
			}
		})
	}
	assertCount(t, db, 0, `SELECT count(*) FROM catalog.games`)
}

func TestConcurrentUpsertsCreateOneCatalogEntry(t *testing.T) {
	store, db := newTestStore(t)
	reversed := SourceGame{Source: "igdb", ExternalID: "1942", Title: "The Witcher 3", Kind: KindGame,
		Platforms: []SourcePlatform{{ExternalID: "41", Name: "Wii U"}, {ExternalID: "130", Name: "Nintendo Switch"}}}
	ids := make(chan string, 16)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			batch := []SourceGame{zelda, reversed}
			if i%2 == 1 {
				batch = []SourceGame{reversed, zelda}
			}
			games, err := store.Upsert(t.Context(), batch)
			if err != nil {
				t.Error(err)
				return
			}
			for _, game := range games {
				if game.Title == zelda.Title {
					ids <- game.ID
				}
			}
		})
	}
	wg.Wait()
	close(ids)
	first := <-ids
	for id := range ids {
		if id != first {
			t.Fatal("concurrent imports produced different catalog identities")
		}
	}
	assertCount(t, db, 2, `SELECT count(*) FROM catalog.games`)
	assertCount(t, db, 2, `SELECT count(*) FROM catalog.platforms`)
	assertCount(t, db, 4, `SELECT count(*) FROM catalog.game_platforms`)
}

func TestUpsertPreservesDeactivation(t *testing.T) {
	store, db := newTestStore(t)
	game := upsertOne(t, store, zelda)
	if _, err := db.ExecContext(t.Context(), `UPDATE catalog.games SET is_active = false WHERE id = $1`, game.ID); err != nil {
		t.Fatal(err)
	}
	if again := upsertOne(t, store, zelda); again.ID != game.ID || again.IsActive {
		t.Fatalf("re-import changed identity or reactivated the game: %+v", again)
	}
}

func TestUpsertHonorsCanceledContext(t *testing.T) {
	store, db := newTestStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Upsert(ctx, []SourceGame{zelda}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	assertCount(t, db, 0, `SELECT count(*) FROM catalog.games`)
}
