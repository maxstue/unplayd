package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/lib/pq"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Store persists catalog games in the catalog-owned schema.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

type sourceKey struct{ source, externalID string }

// lockKey namespaces advisory locks so platforms and games never share a key space.
func (k sourceKey) lockKey(kind string) string { return kind + ":" + k.source + ":" + k.externalID }

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Upsert maps each external identity to exactly one catalog game and returns
// the stored games in input order. Existing UUIDs are kept; updated_at only
// advances when title, cover, kind, parent or platforms change. Repeated
// entries for the same identity keep their first occurrence.
//
// Each game is linked to the top of its parent chain, resolved within the
// batch and then among stored games, so the hierarchy keeps a single level.
// A parent must be part of the batch or already stored. Stored children of a
// game that becomes a child move to its new parent.
func (s *Store) Upsert(ctx context.Context, games []SourceGame) ([]Game, error) {
	games, err := normalize(games)
	if err != nil || len(games) == 0 {
		return []Game{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockIdentities(ctx, tx, games); err != nil {
		return nil, err
	}
	platformIDs := map[sourceKey]string{}
	gameIDs := make(map[sourceKey]string, len(games))
	ordered := make([]string, 0, len(games))
	for _, game := range games {
		ids := make([]string, 0, len(game.Platforms))
		for _, platform := range game.Platforms {
			key := sourceKey{game.Source, platform.ExternalID}
			id, ok := platformIDs[key]
			if !ok {
				if id, err = upsertPlatform(ctx, tx, key, platform.Name); err != nil {
					return nil, err
				}
				platformIDs[key] = id
			}
			ids = append(ids, id)
		}
		id, err := upsertGame(ctx, tx, game, ids)
		if err != nil {
			return nil, err
		}
		gameIDs[sourceKey{game.Source, game.ExternalID}] = id
		ordered = append(ordered, id)
	}
	if err := assignParents(ctx, tx, games, gameIDs); err != nil {
		return nil, err
	}
	stored, err := loadGames(ctx, tx, ordered)
	if err != nil {
		return nil, err
	}
	return stored, tx.Commit()
}

func normalize(games []SourceGame) ([]SourceGame, error) {
	seen := map[sourceKey]bool{}
	result := make([]SourceGame, 0, len(games))
	for _, game := range games {
		game.Title = strings.TrimSpace(game.Title)
		if game.Source == "" || game.ExternalID == "" || game.Title == "" {
			return nil, fmt.Errorf("%w: source games require source, external ID and title", ErrInvalidRequest)
		}
		if !slices.Contains(kinds, game.Kind) {
			return nil, fmt.Errorf("%w: unknown game kind %q", ErrInvalidRequest, game.Kind)
		}
		if game.ParentExternalID == game.ExternalID {
			return nil, fmt.Errorf("%w: a game cannot be its own parent", ErrInvalidRequest)
		}
		key := sourceKey{game.Source, game.ExternalID}
		if seen[key] {
			continue
		}
		seen[key] = true
		platforms, err := normalizePlatforms(game.Platforms)
		if err != nil {
			return nil, err
		}
		game.Platforms = platforms
		result = append(result, game)
	}
	return result, nil
}

func normalizePlatforms(platforms []SourcePlatform) ([]SourcePlatform, error) {
	result := make([]SourcePlatform, 0, len(platforms))
	seen := map[string]bool{}
	for _, platform := range platforms {
		platform.Name = strings.TrimSpace(platform.Name)
		if platform.ExternalID == "" || platform.Name == "" {
			return nil, fmt.Errorf("%w: source platforms require external ID and name", ErrInvalidRequest)
		}
		if !seen[platform.ExternalID] {
			seen[platform.ExternalID] = true
			result = append(result, platform)
		}
	}
	return result, nil
}

// lockIdentities serializes concurrent imports of the same external identity,
// including the parents a batch links to. Keys are acquired in one global
// order (platforms, then games, each sorted) so overlapping imports cannot deadlock.
func lockIdentities(ctx context.Context, tx *sql.Tx, games []SourceGame) error {
	var platformKeys, gameKeys []string
	for _, game := range games {
		gameKeys = append(gameKeys, sourceKey{game.Source, game.ExternalID}.lockKey("game"))
		if game.ParentExternalID != "" {
			gameKeys = append(gameKeys, sourceKey{game.Source, game.ParentExternalID}.lockKey("game"))
		}
		for _, platform := range game.Platforms {
			platformKeys = append(platformKeys, sourceKey{game.Source, platform.ExternalID}.lockKey("platform"))
		}
	}
	slices.Sort(platformKeys)
	slices.Sort(gameKeys)
	for _, key := range slices.Concat(slices.Compact(platformKeys), slices.Compact(gameKeys)) {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
			return err
		}
	}
	return nil
}

func upsertPlatform(ctx context.Context, tx *sql.Tx, key sourceKey, name string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT platform_id FROM catalog.external_platform_ids
		WHERE source = $1 AND external_id = $2`, key.source, key.externalID).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err := tx.QueryRowContext(ctx, `INSERT INTO catalog.platforms(name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
			return "", err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO catalog.external_platform_ids(source, external_id, platform_id)
			VALUES ($1, $2, $3)`, key.source, key.externalID, id)
		return id, err
	case err != nil:
		return "", err
	}
	_, err = tx.ExecContext(ctx, `UPDATE catalog.platforms SET name = $2 WHERE id = $1 AND name <> $2`, id, name)
	return id, err
}

// upsertGame stores everything but the parent, which assignParents sets once
// every game of the batch has an ID.
func upsertGame(ctx context.Context, tx *sql.Tx, game SourceGame, platformIDs []string) (string, error) {
	cover := sql.NullString{String: game.CoverURL, Valid: game.CoverURL != ""}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT game_id FROM catalog.external_game_ids
		WHERE source = $1 AND external_id = $2`, game.Source, game.ExternalID).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err := tx.QueryRowContext(ctx, `INSERT INTO catalog.games(title, cover_url, kind) VALUES ($1, $2, $3) RETURNING id`,
			game.Title, cover, game.Kind).Scan(&id); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO catalog.external_game_ids(source, external_id, game_id)
			VALUES ($1, $2, $3)`, game.Source, game.ExternalID, id); err != nil {
			return "", err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO catalog.game_platforms(game_id, platform_id)
			SELECT $1, unnest($2::uuid[])`, id, pq.Array(platformIDs))
		return id, err
	case err != nil:
		return "", err
	}
	var platformsChanged bool
	if err := tx.QueryRowContext(ctx, `WITH removed AS (
			DELETE FROM catalog.game_platforms WHERE game_id = $1 AND NOT (platform_id = ANY($2::uuid[])) RETURNING 1
		), added AS (
			INSERT INTO catalog.game_platforms(game_id, platform_id) SELECT $1, unnest($2::uuid[])
			ON CONFLICT DO NOTHING RETURNING 1
		)
		SELECT EXISTS (SELECT 1 FROM removed) OR EXISTS (SELECT 1 FROM added)`, id, pq.Array(platformIDs)).Scan(&platformsChanged); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `UPDATE catalog.games SET title = $2, cover_url = $3, kind = $5, updated_at = now()
		WHERE id = $1 AND (title IS DISTINCT FROM $2 OR cover_url IS DISTINCT FROM $3 OR kind IS DISTINCT FROM $5 OR $4)`,
		id, game.Title, cover, platformsChanged, game.Kind)
	return id, err
}

// assignParents links every batch game to the top of its parent chain. Stored
// children of games that become children move to the same top first, so the
// explicit assignments of batch games always win.
func assignParents(ctx context.Context, tx *sql.Tx, games []SourceGame, ids map[sourceKey]string) error {
	resolver := hierarchy{tx: tx, batch: map[sourceKey]SourceGame{}, ids: ids, keys: map[string]sourceKey{}}
	for _, game := range games {
		key := sourceKey{game.Source, game.ExternalID}
		resolver.batch[key] = game
		resolver.keys[ids[key]] = key
	}
	gameIDs := make([]string, 0, len(games))
	parentIDs := make([]sql.NullString, 0, len(games))
	for _, game := range games {
		parent, err := resolver.top(ctx, game)
		if err != nil {
			return err
		}
		gameIDs = append(gameIDs, ids[sourceKey{game.Source, game.ExternalID}])
		parentIDs = append(parentIDs, parent)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE catalog.games g SET parent_id = m.parent_id, updated_at = now()
		FROM unnest($1::uuid[], $2::uuid[]) AS m(id, parent_id)
		WHERE m.parent_id IS NOT NULL AND g.parent_id = m.id AND g.id <> m.parent_id AND NOT (g.id = ANY($1::uuid[]))`,
		pq.Array(gameIDs), pq.Array(parentIDs)); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE catalog.games g SET parent_id = m.parent_id, updated_at = now()
		FROM unnest($1::uuid[], $2::uuid[]) AS m(id, parent_id)
		WHERE g.id = m.id AND g.parent_id IS DISTINCT FROM m.parent_id`,
		pq.Array(gameIDs), pq.Array(parentIDs))
	return err
}

// hierarchy resolves parent chains over the batch, falling back to stored
// games for parents outside it. Batch data wins over stored data.
type hierarchy struct {
	tx    *sql.Tx
	batch map[sourceKey]SourceGame
	ids   map[sourceKey]string
	keys  map[string]sourceKey
}

// top returns the catalog ID of the first ancestor without a parent, or NULL
// when game has no parent. Cyclic chains leave the game without a parent.
func (h hierarchy) top(ctx context.Context, game SourceGame) (sql.NullString, error) {
	self := h.ids[sourceKey{game.Source, game.ExternalID}]
	current := self
	seen := map[string]bool{self: true}
	for {
		next, err := h.parent(ctx, current)
		if err != nil {
			return sql.NullString{}, err
		}
		if next == "" {
			return sql.NullString{String: current, Valid: current != self}, nil
		}
		if seen[next] {
			return sql.NullString{}, nil
		}
		seen[next] = true
		current = next
	}
}

// parent returns the catalog ID of the direct parent of id, or "" for none.
func (h hierarchy) parent(ctx context.Context, id string) (string, error) {
	key, inBatch := h.keys[id]
	if !inBatch {
		var parent sql.NullString
		err := h.tx.QueryRowContext(ctx, `SELECT parent_id FROM catalog.games WHERE id = $1`, id).Scan(&parent)
		return parent.String, err
	}
	game := h.batch[key]
	if game.ParentExternalID == "" {
		return "", nil
	}
	parentKey := sourceKey{game.Source, game.ParentExternalID}
	if parent, ok := h.ids[parentKey]; ok {
		return parent, nil
	}
	var parent string
	err := h.tx.QueryRowContext(ctx, `SELECT game_id FROM catalog.external_game_ids
		WHERE source = $1 AND external_id = $2`, parentKey.source, parentKey.externalID).Scan(&parent)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: parent %s of %s %s is neither in the batch nor stored",
			ErrInvalidRequest, game.ParentExternalID, game.Source, game.ExternalID)
	}
	return parent, err
}

// ChildCounts returns the number of active children per kind for each of the
// given parents, in presentation order. Parents without children are omitted.
func (s *Store) ChildCounts(ctx context.Context, parentIDs []string) (map[string][]KindCount, error) {
	counts := map[string][]KindCount{}
	if len(parentIDs) == 0 {
		return counts, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT parent_id, kind, count(*) FROM catalog.games
		WHERE parent_id = ANY($1::uuid[]) AND is_active
		GROUP BY parent_id, kind ORDER BY parent_id, array_position($2::text[], kind)`,
		pq.Array(parentIDs), pq.Array(kinds))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var parentID string
		var count KindCount
		if err := rows.Scan(&parentID, &count.Kind, &count.Count); err != nil {
			return nil, err
		}
		counts[parentID] = append(counts[parentID], count)
	}
	return counts, rows.Err()
}

// Detail returns a game with its active children grouped by kind. Children
// reference their parent and have no children themselves.
func (s *Store) Detail(ctx context.Context, id string) (Detail, error) {
	if !uuidPattern.MatchString(id) {
		return Detail{}, ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return Detail{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM catalog.games WHERE parent_id = $1 AND is_active
		ORDER BY array_position($2::text[], kind), lower(title), id`, id, pq.Array(kinds))
	if err != nil {
		return Detail{}, err
	}
	ids := []string{id}
	for rows.Next() {
		var child string
		if err := rows.Scan(&child); err != nil {
			rows.Close()
			return Detail{}, err
		}
		ids = append(ids, child)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Detail{}, err
	}
	games, err := loadGames(ctx, tx, ids)
	if err != nil {
		return Detail{}, err
	}
	if len(games) == 0 || games[0].ID != id {
		return Detail{}, ErrNotFound
	}
	detail := Detail{Game: games[0], Children: []ChildGroup{}}
	for _, child := range games[1:] {
		if last := len(detail.Children) - 1; last >= 0 && detail.Children[last].Kind == child.Kind {
			detail.Children[last].Games = append(detail.Children[last].Games, child)
		} else {
			detail.Children = append(detail.Children, ChildGroup{Kind: child.Kind, Games: []Game{child}})
		}
	}
	return detail, tx.Commit()
}

func loadGames(ctx context.Context, db querier, ids []string) ([]Game, error) {
	rows, err := db.QueryContext(ctx, `SELECT g.id, g.title, g.kind, g.parent_id, parent.title, g.cover_url,
			g.is_active, g.updated_at, p.id, p.name
		FROM catalog.games g
		LEFT JOIN catalog.games parent ON parent.id = g.parent_id
		LEFT JOIN catalog.game_platforms gp ON gp.game_id = g.id
		LEFT JOIN catalog.platforms p ON p.id = gp.platform_id
		WHERE g.id = ANY($1::uuid[])
		ORDER BY g.id, lower(p.name), p.id`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]*Game{}
	for rows.Next() {
		var game Game
		var parentID, parentTitle, cover, platformID, platformName sql.NullString
		if err := rows.Scan(&game.ID, &game.Title, &game.Kind, &parentID, &parentTitle, &cover,
			&game.IsActive, &game.UpdatedAt, &platformID, &platformName); err != nil {
			return nil, err
		}
		existing, ok := byID[game.ID]
		if !ok {
			if parentID.Valid {
				game.Parent = &ParentRef{ID: parentID.String, Title: parentTitle.String}
			}
			if cover.Valid {
				game.CoverURL = &cover.String
			}
			game.UpdatedAt = game.UpdatedAt.UTC()
			game.Platforms = []Platform{}
			existing = &game
			byID[game.ID] = existing
		}
		if platformID.Valid {
			existing.Platforms = append(existing.Platforms, Platform{ID: platformID.String, Name: platformName.String})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]Game, 0, len(ids))
	for _, id := range ids {
		if game, ok := byID[id]; ok {
			result = append(result, *game)
		}
	}
	return result, nil
}
