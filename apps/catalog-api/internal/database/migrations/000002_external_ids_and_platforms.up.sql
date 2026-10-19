BEGIN;
CREATE TABLE catalog.external_game_ids (
    source text NOT NULL CHECK (length(source) > 0),
    external_id text NOT NULL CHECK (length(external_id) > 0),
    game_id uuid NOT NULL REFERENCES catalog.games(id) ON DELETE RESTRICT,
    PRIMARY KEY (source, external_id)
);
CREATE INDEX external_ids_game ON catalog.external_game_ids(game_id);
CREATE TABLE catalog.game_platforms (
    game_id uuid NOT NULL REFERENCES catalog.games(id) ON DELETE CASCADE,
    platform_id uuid NOT NULL REFERENCES catalog.platforms(id) ON DELETE RESTRICT,
    PRIMARY KEY (game_id, platform_id)
);
COMMIT;
