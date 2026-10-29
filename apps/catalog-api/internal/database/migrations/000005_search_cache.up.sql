BEGIN;
-- Remembers when a normalized search was last answered by the provider and
-- which games it returned, so later searches are answered locally, including
-- provider matches whose titles do not contain the query.
CREATE TABLE catalog.search_queries (
    query_key text PRIMARY KEY CHECK (length(query_key) BETWEEN 1 AND 400),
    refreshed_at timestamptz NOT NULL
);
CREATE TABLE catalog.search_query_games (
    query_key text NOT NULL REFERENCES catalog.search_queries(query_key) ON DELETE CASCADE,
    game_id uuid NOT NULL REFERENCES catalog.games(id) ON DELETE RESTRICT,
    PRIMARY KEY (query_key, game_id)
);
CREATE INDEX search_query_games_game ON catalog.search_query_games(game_id);
COMMIT;
