BEGIN;
CREATE TABLE catalog.external_platform_ids (
    source text NOT NULL CHECK (length(source) > 0),
    external_id text NOT NULL CHECK (length(external_id) > 0),
    platform_id uuid NOT NULL REFERENCES catalog.platforms(id) ON DELETE RESTRICT,
    PRIMARY KEY (source, external_id)
);
CREATE INDEX external_platform_ids_platform ON catalog.external_platform_ids(platform_id);
COMMIT;
