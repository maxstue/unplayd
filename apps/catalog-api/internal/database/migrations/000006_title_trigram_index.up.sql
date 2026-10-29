BEGIN;
-- Serves the title search, which matches words anywhere in a title. pg_trgm is
-- created by the database bootstrap because the catalog role may not create
-- extensions.
CREATE INDEX games_active_title_trgm ON catalog.games USING gin (title catalog.gin_trgm_ops) WHERE is_active;
COMMIT;
