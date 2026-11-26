package catalog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// SearchPolicy controls when searches consult the provider.
type SearchPolicy struct {
	// FreshFor is how long a provider answer for a search stays current.
	FreshFor time.Duration
	// Wait bounds how long a request waits for a provider refresh before it
	// is answered from stored games. The refresh continues in the background.
	Wait time.Duration
	// RefreshTimeout bounds a provider refresh including storage.
	RefreshTimeout time.Duration
	// Cooldown suspends provider refreshes after a failed refresh.
	Cooldown time.Duration
	// MaxConcurrentRefreshes bounds distinct provider searches, including work
	// continuing after the originating request returns. Zero uses the default.
	MaxConcurrentRefreshes int
}

// DefaultSearchPolicy refreshes a search once a day and keeps requests within
// the service clients' 3-second budget.
func DefaultSearchPolicy() SearchPolicy {
	return SearchPolicy{FreshFor: 24 * time.Hour, Wait: 2 * time.Second, RefreshTimeout: 20 * time.Second, Cooldown: 30 * time.Second, MaxConcurrentRefreshes: 8}
}

var (
	// ErrRefreshPending marks a search with no stored results whose refresh is still running.
	ErrRefreshPending  = errors.New("provider refresh still running")
	errCoolingDown     = errors.New("provider refreshes are suspended after a failure")
	errRefreshCapacity = errors.New("provider refresh capacity reached")
)

// Service answers catalog reads from stored games. Searches that the provider
// has not answered within the freshness period are refreshed first; details
// and batches never contact the provider, so stored games stay readable
// during provider outages.
type Service struct {
	store    *Store
	importer *Importer
	policy   SearchPolicy
	logger   *slog.Logger
	now      func() time.Time

	mu            sync.Mutex
	inflight      map[string]*refreshCall
	active        int
	coolingUntil  time.Time
	refreshesDone sync.WaitGroup
	stopping      bool
}

// refreshCall is one provider refresh shared by every request for the same
// search. waiters, keepAfterTimeout and abandoned are guarded by Service.mu.
type refreshCall struct {
	done   chan struct{}
	err    error
	cancel context.CancelFunc
	// waiters counts the requests currently waiting for the result.
	waiters int
	// keepAfterTimeout lets the refresh finish for a request that will retry.
	keepAfterTimeout bool
	// abandoned records that every waiter aborted and the refresh was cancelled.
	abandoned bool
}

// NewService returns a catalog service. A nil importer answers every search
// from stored games only.
func NewService(store *Store, importer *Importer, policy SearchPolicy, logger *slog.Logger) *Service {
	if policy.MaxConcurrentRefreshes <= 0 {
		policy.MaxConcurrentRefreshes = DefaultSearchPolicy().MaxConcurrentRefreshes
	}
	return &Service{
		store: store, importer: importer, policy: policy, logger: logger, now: time.Now,
		inflight: map[string]*refreshCall{},
	}
}

// Search returns one page of stored games matching the request. Only first
// pages refresh stale searches. When the refresh fails and no stored game
// matches, Search reports ErrUnavailable instead of an empty page.
func (s *Service) Search(ctx context.Context, request SearchRequest) (SearchPage, error) {
	query, err := newLocalQuery(request)
	if err != nil {
		return SearchPage{}, err
	}
	var providerErr error
	if query.after == nil {
		if providerErr, err = s.refresh(ctx, query); err != nil {
			return SearchPage{}, err
		}
	}
	hits, next, err := s.store.search(ctx, query)
	if err != nil {
		return SearchPage{}, err
	}
	if providerErr != nil {
		if len(hits) == 0 {
			return SearchPage{}, fmt.Errorf("%w: %w", ErrUnavailable, providerErr)
		}
		s.logger.WarnContext(ctx, "search answered from stored games without provider refresh", "error", providerErr.Error())
	}
	page := SearchPage{Hits: hits}
	if next != nil {
		page.NextCursor = next.encode()
	}
	return page, nil
}

// Suggestions returns up to five matches from the stored catalog.
func (s *Service) Suggestions(ctx context.Context, text string) ([]SearchHit, error) {
	query, err := newLocalQuery(SearchRequest{Query: text, Limit: 5})
	if err != nil {
		return nil, err
	}
	hits, _, err := s.store.search(ctx, query)
	return hits, err
}

// Detail returns a stored game, including inactive ones, with its active children.
func (s *Service) Detail(ctx context.Context, id string) (Detail, error) {
	id, err := canonicalID(id)
	if err != nil {
		return Detail{}, err
	}
	return s.store.Detail(ctx, id)
}

// Batch classifies stored games by ID.
func (s *Service) Batch(ctx context.Context, ids []string) (Batch, error) {
	return s.store.Batch(ctx, ids)
}

// Stop prevents new refreshes and cancels outstanding provider work.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopping = true
	for _, call := range s.inflight {
		call.cancel()
	}
}

// Wait blocks until background refreshes have finished.
func (s *Service) Wait() { s.refreshesDone.Wait() }

// refresh brings a stale search up to date. It returns providerErr when the
// provider could not answer in time and err for storage failures.
func (s *Service) refresh(ctx context.Context, query localQuery) (providerErr, err error) {
	if s.importer == nil {
		return nil, nil
	}
	fresh, err := s.store.searchIsFresh(ctx, query.key, s.policy.FreshFor)
	if err != nil || fresh {
		return nil, err
	}
	call, err := s.start(ctx, query)
	if err != nil {
		return err, nil
	}
	timer := time.NewTimer(s.policy.Wait)
	defer timer.Stop()
	select {
	case <-call.done:
		return call.err, nil
	case <-timer.C:
		s.leave(query.key, call, false)
		return ErrRefreshPending, nil
	case <-ctx.Done():
		s.leave(query.key, call, true)
		return nil, ctx.Err()
	}
}

// leave ends one request's wait for call. A refresh whose waiters all aborted
// before any of them timed out is cancelled to free capacity for other searches.
func (s *Service) leave(key string, call *refreshCall, aborted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	call.waiters--
	if !aborted {
		call.keepAfterTimeout = true
		return
	}
	if call.waiters > 0 || call.keepAfterTimeout || s.inflight[key] != call {
		return
	}
	call.abandoned = true
	delete(s.inflight, key)
	call.cancel()
}

// start joins the running refresh of the same search or starts a new one,
// unless refreshes are suspended after a failure or admission capacity is full.
func (s *Service) start(ctx context.Context, query localQuery) (*refreshCall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return nil, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if call, ok := s.inflight[query.key]; ok {
		call.waiters++
		return call, nil
	}
	if s.now().Before(s.coolingUntil) {
		return nil, errCoolingDown
	}
	if s.active >= s.policy.MaxConcurrentRefreshes {
		return nil, errRefreshCapacity
	}
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.policy.RefreshTimeout)
	call := &refreshCall{done: make(chan struct{}), cancel: cancel, waiters: 1}
	s.inflight[query.key] = call
	s.active++
	s.refreshesDone.Add(1)
	go func() {
		defer s.refreshesDone.Done()
		defer cancel()
		call.err = s.fetch(refreshCtx, query)
		s.mu.Lock()
		s.active--
		if s.inflight[query.key] == call {
			delete(s.inflight, query.key)
		}
		// A cancelled abandoned refresh says nothing about the provider's health.
		failed := call.err != nil && !call.abandoned
		if failed {
			s.coolingUntil = s.now().Add(s.policy.Cooldown)
		}
		s.mu.Unlock()
		if failed {
			s.logger.WarnContext(refreshCtx, "provider search refresh failed", "error", call.err.Error(), "cooldown", s.policy.Cooldown.String())
		}
		close(call.done)
	}()
	return call, nil
}

// fetch stores the provider's matches for query and records the search.
func (s *Service) fetch(ctx context.Context, query localQuery) error {
	matches, err := s.importer.fetch(ctx, query.text, query.filter, maxSearchLimit)
	if err != nil {
		return err
	}
	ids := make([]string, len(matches))
	for index, game := range matches {
		ids[index] = game.ID
	}
	return s.store.recordSearch(ctx, query.key, ids)
}
