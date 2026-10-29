BEGIN;
-- Existing games predate kinds; re-imports classify them.
ALTER TABLE catalog.games
    ADD COLUMN kind text NOT NULL DEFAULT 'game' CHECK (kind IN (
        'game', 'edition', 'dlc', 'expansion', 'standalone_expansion', 'pack', 'update', 'bundle',
        'remake', 'remaster', 'expanded_game', 'port', 'mod', 'fork', 'episode', 'season'
    )),
    ADD COLUMN parent_id uuid REFERENCES catalog.games(id) ON DELETE RESTRICT,
    ADD CONSTRAINT games_not_own_parent CHECK (parent_id <> id);
ALTER TABLE catalog.games ALTER COLUMN kind DROP DEFAULT;
CREATE INDEX games_parent_kind ON catalog.games(parent_id, kind) WHERE parent_id IS NOT NULL;
-- Keeps the hierarchy at one level: a parent has no parent and a child has no
-- children, which also rules out cycles. The check runs at commit so a
-- transaction may re-parent in any order; locking the parent row makes
-- concurrent transactions see each other's committed hierarchy changes.
CREATE FUNCTION catalog.check_game_hierarchy() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    current_parent uuid;
BEGIN
    SELECT parent_id INTO current_parent FROM catalog.games WHERE id = NEW.id;
    IF current_parent IS NULL THEN
        RETURN NULL;
    END IF;
    PERFORM 1 FROM catalog.games WHERE id = current_parent AND parent_id IS NULL FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Catalog game % cannot have a parent that has a parent itself', NEW.id
            USING ERRCODE = 'check_violation';
    END IF;
    IF EXISTS (SELECT 1 FROM catalog.games WHERE parent_id = NEW.id) THEN
        RAISE EXCEPTION 'Catalog game % cannot have a parent while it has children', NEW.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER games_single_level AFTER INSERT OR UPDATE OF parent_id ON catalog.games
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION catalog.check_game_hierarchy();
COMMIT;
