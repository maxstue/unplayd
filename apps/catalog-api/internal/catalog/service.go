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
}

// DefaultSearchPolicy refreshes a search once a day and keeps requests within
// the service clients' 3-second budget.
func DefaultSearchPolicy() SearchPolicy {
	return SearchPolicy{FreshFor: 24 * time.Hour, Wait: 2 * time.Second, RefreshTimeout: 20 * time.Second, Cooldown: 30 * time.Second}
}

var (
	errRefreshPending = errors.New("provider refresh still running")
	errCoolingDown    = errors.New("provider refreshes are suspended after a failure")
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
	coolingUntil  time.Time
	refreshesDone sync.WaitGroup
}

type refreshCall struct {
	done chan struct{}
	err  error
}

// NewService returns a catalog service. A nil importer answers every search
// from stored games only.
func NewService(store *Store, importer *Importer, policy SearchPolicy, logger *slog.Logger) *Service {
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
			return SearchPage{}, fmt.Errorf("%w: %v", ErrUnavailable, providerErr)
		}
		s.logger.WarnContext(ctx, "search answered from stored games without provider refresh", "error", providerErr.Error())
	}
	page := SearchPage{Hits: hits}
	if next != nil {
		page.NextCursor = next.encode()
	}
	return page, nil
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
		return errRefreshPending, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// start joins the running refresh of the same search or starts a new one,
// unless refreshes are suspended after a failure.
func (s *Service) start(ctx context.Context, query localQuery) (*refreshCall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if call, ok := s.inflight[query.key]; ok {
		return call, nil
	}
	if s.now().Before(s.coolingUntil) {
		return nil, errCoolingDown
	}
	call := &refreshCall{done: make(chan struct{})}
	s.inflight[query.key] = call
	s.refreshesDone.Add(1)
	go func() {
		defer s.refreshesDone.Done()
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.policy.RefreshTimeout)
		defer cancel()
		call.err = s.fetch(refreshCtx, query)
		s.mu.Lock()
		delete(s.inflight, query.key)
		if call.err != nil {
			s.coolingUntil = s.now().Add(s.policy.Cooldown)
		}
		s.mu.Unlock()
		if call.err != nil {
			s.logger.WarnContext(refreshCtx, "provider search refresh failed", "error", call.err.Error(), "cooldown", s.policy.Cooldown.String())
		}
		close(call.done)
	}()
	return call, nil
}

// fetch stores the provider's matches for query and records the search.
func (s *Service) fetch(ctx context.Context, query localQuery) error {
	matches, _, err := s.importer.fetch(ctx, query.text, query.filter, maxSearchLimit)
	if err != nil {
		return err
	}
	ids := make([]string, len(matches))
	for index, game := range matches {
		ids[index] = game.ID
	}
	return s.store.recordSearch(ctx, query.key, ids)
}
