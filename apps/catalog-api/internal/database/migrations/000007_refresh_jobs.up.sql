BEGIN;
CREATE TABLE catalog.refresh_jobs (
    game_id uuid PRIMARY KEY REFERENCES catalog.games(id) ON DELETE RESTRICT,
    next_run_at timestamptz NOT NULL,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'running', 'failed')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    lease_until timestamptz,
    lease_token uuid,
    last_attempt_at timestamptz,
    last_success_at timestamptz,
    last_error_code text,
    CHECK ((state = 'running') = (lease_until IS NOT NULL AND lease_token IS NOT NULL)),
    CHECK (state = 'running' OR (lease_until IS NULL AND lease_token IS NULL))
);
CREATE INDEX refresh_jobs_due ON catalog.refresh_jobs(next_run_at) WHERE state = 'pending';
CREATE INDEX refresh_jobs_leases ON catalog.refresh_jobs(lease_until) WHERE state = 'running';
CREATE INDEX search_queries_refreshed ON catalog.search_queries(refreshed_at);
INSERT INTO catalog.refresh_jobs(game_id, next_run_at)
SELECT g.id, now() + random() * interval '24 hours'
FROM catalog.games g WHERE g.is_active AND EXISTS (
 SELECT 1 FROM catalog.external_game_ids e WHERE e.game_id=g.id AND e.source='igdb'
);
COMMIT;
