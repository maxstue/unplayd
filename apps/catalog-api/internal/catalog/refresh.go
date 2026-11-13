package catalog

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"math/rand/v2"
	"time"
)

var errLeaseLost = errors.New("refresh lease lost")

// RefreshPolicy bounds durable background work independently of HTTP requests.
type RefreshPolicy struct {
	Poll, Timeout, Lease, Interval, Retry time.Duration
	// Recovery separates retry rounds after MaxAttempts transient failures.
	Recovery time.Duration
	// MaxAttempts bounds a retry round; transient failures remain recoverable.
	MaxAttempts int
}

func DefaultRefreshPolicy() RefreshPolicy {
	return RefreshPolicy{Poll: 5 * time.Second, Timeout: time.Minute, Lease: 90 * time.Second, Interval: 24 * time.Hour, Retry: time.Minute, Recovery: time.Hour, MaxAttempts: 5}
}
func (p RefreshPolicy) valid() bool {
	return p.Poll > 0 && p.Timeout > 0 && p.Lease > p.Timeout && p.Interval > 0 && p.Retry > 0 && p.Recovery >= p.Retry && p.MaxAttempts > 0
}

type refreshJob struct {
	ID, ExternalID, Token string
	Attempts              int
}

func (s *Store) claimRefresh(ctx context.Context, p RefreshPolicy) (refreshJob, error) {
	var j refreshJob
	err := s.db.QueryRowContext(ctx, `WITH candidate AS (
 SELECT j.game_id FROM catalog.refresh_jobs j JOIN catalog.games g ON g.id=j.game_id
 WHERE g.is_active AND ((j.state='pending' AND j.next_run_at<=now()) OR (j.state='running' AND j.lease_until<=now()))
 ORDER BY j.next_run_at, j.game_id FOR UPDATE OF j SKIP LOCKED LIMIT 1
 ) UPDATE catalog.refresh_jobs j SET state='running',
 attempts=CASE WHEN j.attempts >= $2 THEN 1 ELSE j.attempts+1 END,
 lease_token=gen_random_uuid(), lease_until=now()+make_interval(secs=>$1), last_attempt_at=now()
 FROM candidate c WHERE j.game_id=c.game_id
 RETURNING j.game_id, j.lease_token, j.attempts,
 (SELECT external_id FROM catalog.external_game_ids WHERE game_id=j.game_id AND source='igdb' ORDER BY external_id LIMIT 1)`, p.Lease.Seconds(), p.MaxAttempts).Scan(&j.ID, &j.Token, &j.Attempts, &j.ExternalID)
	return j, err
}

// finishRefresh fences both catalog writes and acknowledgement in one transaction.
func (s *Store) finishRefresh(ctx context.Context, j refreshJob, games []SourceGame, delay time.Duration, code string, failed bool) error {
	games, err := normalize(games)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Normal imports lock identities before games; use the same order here.
	if err := lockIdentities(ctx, tx, games); err != nil {
		return err
	}
	var owned bool
	err = tx.QueryRowContext(ctx, `SELECT lease_token=$2::uuid AND lease_until>clock_timestamp() FROM catalog.refresh_jobs WHERE game_id=$1 AND state='running' FOR UPDATE`, j.ID, j.Token).Scan(&owned)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !owned) {
		return errLeaseLost
	}
	if err != nil {
		return err
	}
	if len(games) > 0 {
		if _, err := upsertGames(ctx, tx, games); err != nil {
			return err
		}
	}
	state := "pending"
	if failed {
		state = "failed"
	}
	_, err = tx.ExecContext(ctx, `UPDATE catalog.refresh_jobs SET state=$3, lease_token=NULL, lease_until=NULL,
 next_run_at=now()+make_interval(secs=>$4), last_error_code=NULLIF($5,''),
 last_success_at=CASE WHEN $5='' THEN now() ELSE last_success_at END,
 attempts=CASE WHEN $5='' THEN 0 WHEN $5 IN ('provider_credentials','shutdown') THEN greatest(attempts-1,0) ELSE attempts END WHERE game_id=$1 AND lease_token=$2::uuid`, j.ID, j.Token, state, delay.Seconds(), code)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func jitter(d time.Duration) time.Duration { return d + time.Duration(rand.Float64()*0.1*float64(d)) }

// RunRefresh runs one bounded worker and daily cache maintenance until cancellation.
// rejected classifies permanent provider errors; credentials classifies shared auth failure.
func (s *Store) RunRefresh(ctx context.Context, importer *Importer, p RefreshPolicy, rejected, credentials func(error) bool, logger *slog.Logger) error {
	if !p.valid() {
		return errors.New("invalid refresh policy")
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	nextCleanup := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return nil
		}
		if time.Now().After(nextCleanup) {
			nextCleanup = s.maintainRefresh(ctx, logger)
		}
		delay := p.Poll
		if importer != nil {
			delay = s.runNextRefresh(ctx, importer, p, rejected, credentials, logger)
		}

		timer.Reset(delay)
	}
}

// maintainRefresh schedules cleanup retries separately from provider work.
func (s *Store) maintainRefresh(ctx context.Context, logger *slog.Logger) time.Time {
	cleanupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	count, err := s.cleanupSearches(cleanupCtx)
	cancel()
	next := 24 * time.Hour
	if err != nil {
		logger.Warn("search cache cleanup failed")
		next = time.Minute
	} else {
		logger.Info("search cache cleanup", "removed", count)
	}
	s.logRefreshStats(ctx, logger)
	return time.Now().Add(next)
}

type refreshOutcome struct {
	code        string
	terminal    bool
	next, pause time.Duration
}

func (p RefreshPolicy) outcome(ctx context.Context, j refreshJob, err error, rejected, credentials func(error) bool) refreshOutcome {
	if err == nil {
		return refreshOutcome{next: jitter(p.Interval)}
	}
	result := refreshOutcome{code: "provider_unavailable",
		next: jitter(min(p.Retry*time.Duration(1<<min(j.Attempts-1, 10)), time.Hour))}
	if j.Attempts >= p.MaxAttempts {
		result.next = jitter(p.Recovery)
	}
	if errors.Is(err, ErrNotFound) {
		result.code = "provider_missing"
		result.terminal = true
	}
	if rejected != nil && rejected(err) {
		result.code = "provider_rejected"
		result.terminal = true
	}
	if credentials != nil && credentials(err) {
		result = refreshOutcome{code: "provider_credentials", next: time.Minute, pause: time.Minute}
	}
	if ctx.Err() != nil {
		result.code = "shutdown"
		result.terminal = false
		result.next = 0
	}
	return result
}

func (s *Store) runNextRefresh(ctx context.Context, importer *Importer, p RefreshPolicy, rejected, credentials func(error) bool, logger *slog.Logger) time.Duration {
	j, err := s.claimRefresh(ctx, p)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			logger.Warn("refresh claim failed")
		}
		return p.Poll
	}
	started := time.Now()
	jobCtx, cancel := context.WithTimeout(ctx, p.Timeout)
	games, fetchErr := importer.fetchRefresh(jobCtx, j)
	cancel()
	outcome := p.outcome(ctx, j, fetchErr, rejected, credentials)
	if fetchErr != nil {
		games = nil
	}
	// Completion has its own bounded context so shutdown can relinquish ownership.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	err = s.finishRefresh(finishCtx, j, games, outcome.next, outcome.code, outcome.terminal)
	finishCancel()
	if err != nil {
		logger.Warn("refresh completion failed", "gameId", j.ID, "leaseLost", errors.Is(err, errLeaseLost))
	} else {
		logger.Info("catalog refresh", "gameId", j.ID, "attempt", j.Attempts, "result", outcome.code, "failed", outcome.terminal, "duration", time.Since(started), "nextRunAt", time.Now().Add(outcome.next), "claimId", j.Token)
	}
	return outcome.pause
}

// fetchRefresh performs no writes: only the current lease owner may commit results.
func (i *Importer) fetchRefresh(ctx context.Context, j refreshJob) ([]SourceGame, error) {
	found, err := i.source.Games(ctx, []string{j.ExternalID})
	if err != nil {
		return nil, err
	}
	present := false
	for _, game := range found {
		if game.Source == "igdb" && game.ExternalID == j.ExternalID {
			present = true
		}
	}
	if !present {
		return nil, ErrNotFound
	}
	batch, err := i.withRelatives(ctx, found)
	return batch.games, err
}

func (s *Store) cleanupSearches(ctx context.Context) (int64, error) {
	var total int64
	for {
		result, err := s.db.ExecContext(ctx, `DELETE FROM catalog.search_queries WHERE query_key IN (
 SELECT query_key FROM catalog.search_queries WHERE refreshed_at < now()-interval '30 days'
 ORDER BY refreshed_at FOR UPDATE SKIP LOCKED LIMIT 500)`)
		if err != nil {
			return total, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < 500 {
			return total, nil
		}
	}
}
func (s *Store) logRefreshStats(ctx context.Context, logger *slog.Logger) {
	var pending, running, failed int64
	var oldest sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE state='pending' AND next_run_at<=now()),count(*) FILTER(WHERE state='running'),count(*) FILTER(WHERE state='failed'),min(next_run_at) FILTER(WHERE state='pending') FROM catalog.refresh_jobs`).Scan(&pending, &running, &failed, &oldest)
	if err == nil {
		logger.Info("refresh queue", "due", pending, "running", running, "failed", failed, "oldestDueAt", oldest.Time)
	}
}
