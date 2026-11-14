package catalog

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func dueRefresh(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`UPDATE catalog.refresh_jobs SET next_run_at=now()-interval '1 day'`); err != nil {
		t.Fatal(err)
	}
}
func TestRefreshLeaseRecoveryFencesOldResults(t *testing.T) {
	store, db := newTestStore(t)
	game := upsertOne(t, store, zelda)
	dueRefresh(t, db)
	p := DefaultRefreshPolicy()
	old, err := store.claimRefresh(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.claimRefresh(t.Context(), p); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("duplicate claim: %v", err)
	}
	if _, err := db.Exec(`UPDATE catalog.refresh_jobs SET lease_until=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewStore(db).claimRefresh(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	changed := zelda
	changed.Title = "New title"
	if err := store.finishRefresh(t.Context(), old, []SourceGame{changed}, p.Interval, "", false); !errors.Is(err, errLeaseLost) {
		t.Fatalf("stale owner: %v", err)
	}
	if err := store.finishRefresh(t.Context(), fresh, []SourceGame{zelda}, p.Interval, "", false); err != nil {
		t.Fatal(err)
	}
	detail, err := store.Detail(t.Context(), game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Game.Title != game.Title || !detail.Game.UpdatedAt.Equal(game.UpdatedAt) {
		t.Fatal("unchanged refresh changed data")
	}
	assertCount(t, db, 1, `SELECT count(*) FROM catalog.refresh_jobs WHERE state='pending' AND attempts=0 AND last_success_at IS NOT NULL AND next_run_at>now()`)
}
func TestConcurrentRefreshClaimsHaveOneOwner(t *testing.T) {
	store, db := newTestStore(t)
	upsertOne(t, store, zelda)
	dueRefresh(t, db)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := store.claimRefresh(t.Context(), DefaultRefreshPolicy()); results <- err })
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatalf("owners=%d", wins)
	}
}

type refreshOutcomeCase struct {
	name                            string
	providerErr                     error
	missing, permanent, credentials bool
	want                            string
}

func TestRefreshWorkerOutcomes(t *testing.T) {
	for _, tc := range []refreshOutcomeCase{
		{name: "success", want: "pending"},
		{name: "retry", providerErr: errors.New("offline"), want: "pending"},
		{name: "permanent", providerErr: errors.New("rejected"), permanent: true, want: "failed"},
		{name: "credentials pause", providerErr: errors.New("credentials"), credentials: true, want: "pending"},
		{name: "missing", missing: true, want: "failed"},
	} {
		t.Run(tc.name, tc.run)
	}
}

func (tc refreshOutcomeCase) run(t *testing.T) {
	t.Helper()
	store, db := newTestStore(t)
	upsertOne(t, store, zelda)
	dueRefresh(t, db)
	source := &fakeSource{games: []SourceGame{zelda}, err: tc.providerErr}
	if tc.missing {
		source.games = nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	p := DefaultRefreshPolicy()
	p.Poll = time.Millisecond
	err := store.RunRefresh(ctx, NewImporter(source, store), p, func(error) bool { return tc.permanent }, func(error) bool { return tc.credentials }, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var state string
	var attempts int
	var success sql.NullTime
	if err := db.QueryRow(`SELECT state,attempts,last_success_at FROM catalog.refresh_jobs`).Scan(&state, &attempts, &success); err != nil {
		t.Fatal(err)
	}
	if state != tc.want || success.Valid != (tc.providerErr == nil && !tc.missing) {
		t.Fatalf("state=%s attempts=%d success=%v", state, attempts, success.Valid)
	}
	if tc.credentials && attempts != 0 {
		t.Fatalf("auth consumed attempt: %d", attempts)
	}
	if tc.want == "failed" {
		dueRefresh(t, db)
		if _, err := store.claimRefresh(t.Context(), p); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("permanent failure reclaimed: %v", err)
		}
	}
}
func TestTransientRefreshFailuresStartANewRoundAfterRecovery(t *testing.T) {
	store, db := newTestStore(t)
	upsertOne(t, store, zelda)
	p := DefaultRefreshPolicy()
	source := &fakeSource{games: []SourceGame{zelda}, err: errors.New("offline")}
	importer := NewImporter(source, store)
	logger := slog.New(slog.DiscardHandler)
	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		dueRefresh(t, db)
		store.runNextRefresh(t.Context(), importer, p, nil, nil, logger)
		var actual int
		var delay float64
		if err := db.QueryRow(`SELECT attempts,extract(epoch FROM next_run_at-now()) FROM catalog.refresh_jobs WHERE state='pending' AND last_error_code='provider_unavailable'`).Scan(&actual, &delay); err != nil {
			t.Fatal(err)
		}
		if actual != attempt {
			t.Fatalf("attempt=%d, want %d", actual, attempt)
		}
		if attempt == p.MaxAttempts && delay < p.Recovery.Seconds()-1 {
			t.Fatalf("exhausted retry round waits %fs, want at least %v", delay, p.Recovery)
		}
	}
	if _, err := store.claimRefresh(t.Context(), p); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("refresh reclaimed before recovery: %v", err)
	}
	// The next round must call the provider again and commit its recovered data.
	dueRefresh(t, db)
	source.err = nil
	source.games[0].Title = "Recovered title"
	store.runNextRefresh(t.Context(), importer, p, nil, nil, logger)
	assertCount(t, db, 1, `SELECT count(*) FROM catalog.refresh_jobs WHERE state='pending' AND attempts=0 AND last_error_code IS NULL AND last_success_at IS NOT NULL`)
	assertCount(t, db, 1, `SELECT count(*) FROM catalog.games WHERE title='Recovered title'`)
}

func TestSearchCleanupRetainsGamesAndRecentQueries(t *testing.T) {
	store, db := newTestStore(t)
	game := upsertOne(t, store, zelda)
	for _, key := range []string{"old", "recent"} {
		if err := store.recordSearch(t.Context(), key, []string{game.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE catalog.search_queries SET refreshed_at=now()-interval '31 days' WHERE query_key='old'`); err != nil {
		t.Fatal(err)
	}
	n, err := store.cleanupSearches(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("cleanup %d %v", n, err)
	}
	assertCount(t, db, 1, `SELECT count(*) FROM catalog.games`)
	assertCount(t, db, 1, `SELECT count(*) FROM catalog.search_query_games`)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.RunRefresh(ctx, nil, DefaultRefreshPolicy(), nil, nil, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if err := store.RunRefresh(ctx, nil, RefreshPolicy{}, nil, nil, slog.Default()); err == nil {
		t.Fatal("invalid policy accepted")
	}
}

type blockedRefreshSource struct {
	Source
	started chan struct{}
}

func (s blockedRefreshSource) Games(ctx context.Context, _ []string) ([]SourceGame, error) {
	close(s.started)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestRefreshShutdownRelinquishesClaim(t *testing.T) {
	store, db := newTestStore(t)
	upsertOne(t, store, zelda)
	dueRefresh(t, db)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source := blockedRefreshSource{started: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- store.RunRefresh(ctx, NewImporter(source, store), DefaultRefreshPolicy(), nil, nil, slog.New(slog.DiscardHandler))
	}()
	select {
	case <-source.started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker never fetched")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("shutdown blocked")
	}
	assertCount(t, db, 1, `SELECT count(*) FROM catalog.refresh_jobs WHERE state='pending' AND attempts=0 AND lease_token IS NULL AND last_error_code='shutdown'`)
}
func TestCleanupSkipsConcurrentSearchRenewal(t *testing.T) {
	store, db := newTestStore(t)
	game := upsertOne(t, store, zelda)
	if err := store.recordSearch(t.Context(), "renewing", []string{game.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE catalog.search_queries SET refreshed_at=now()-interval '31 days'`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE catalog.search_queries SET refreshed_at=now()`); err != nil {
		t.Fatal(err)
	}
	n, err := store.cleanupSearches(t.Context())
	if err != nil || n != 0 {
		t.Fatalf("cleanup during renewal: %d %v", n, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	n, err = store.cleanupSearches(t.Context())
	if err != nil || n != 0 {
		t.Fatalf("renewed query deleted: %d %v", n, err)
	}
}

type blockedSearchSource struct {
	Source
	started chan struct{}
}

func (s blockedSearchSource) Search(ctx context.Context, _ string, _ SearchFilter, _ int) ([]SourceGame, error) {
	close(s.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestServiceStopCancelsAndRejectsSearchRefreshes(t *testing.T) {
	source := blockedSearchSource{started: make(chan struct{})}
	service := newTestService(nil, source, DefaultSearchPolicy())
	query, err := newLocalQuery(SearchRequest{Query: "zelda", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	call, err := service.start(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("search refresh did not start")
	}
	service.Stop()
	select {
	case <-call.done:
	case <-time.After(time.Second):
		t.Fatal("search refresh did not stop")
	}
	service.Wait()
	if !errors.Is(call.err, context.Canceled) {
		t.Fatalf("refresh error: %v", call.err)
	}
	if _, err := service.start(t.Context(), query); !errors.Is(err, context.Canceled) {
		t.Fatalf("refresh started after Stop: %v", err)
	}
	service.Stop()
}
