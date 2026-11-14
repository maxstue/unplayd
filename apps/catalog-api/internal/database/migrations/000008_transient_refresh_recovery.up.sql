BEGIN;
-- Repair jobs made terminal by the previous transient retry limit. Permanent
-- provider rejections and missing games retain their terminal state.
UPDATE catalog.refresh_jobs
SET state = 'pending', attempts = 0,
    next_run_at = greatest(next_run_at, now() + interval '1 hour')
WHERE state = 'failed' AND last_error_code = 'provider_unavailable';
COMMIT;
