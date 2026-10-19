BEGIN;
CREATE TABLE catalog.games (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title text NOT NULL CHECK (length(title) > 0),
    cover_url text,
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX games_active_title ON catalog.games(lower(title), id) WHERE is_active;
CREATE TABLE catalog.platforms (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (length(name) > 0)
);
CREATE FUNCTION catalog.prevent_game_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Catalog games must be deactivated rather than deleted';
END;
$$;
CREATE TRIGGER games_no_hard_delete BEFORE DELETE ON catalog.games
FOR EACH ROW EXECUTE FUNCTION catalog.prevent_game_delete();
COMMIT;
