package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/lib/pq"
)

// Store persists catalog games in the catalog-owned schema.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

type sourceKey struct{ source, externalID string }

// lockKey namespaces advisory locks so platforms and games never share a key space.
func (k sourceKey) lockKey(kind string) string { return kind + ":" + k.source + ":" + k.externalID }

// Upsert maps each external identity to exactly one catalog game and returns
// the stored games in input order. Existing UUIDs are kept; updated_at only
// advances when title, cover or platforms change. Repeated entries for the
// same identity keep their first occurrence.
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
	gameIDs := make([]string, 0, len(games))
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
		gameIDs = append(gameIDs, id)
	}
	stored, err := loadGames(ctx, tx, gameIDs)
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
		key := sourceKey{game.Source, game.ExternalID}
		if seen[key] {
			continue
		}
		seen[key] = true
		platforms := make([]SourcePlatform, 0, len(game.Platforms))
		seenPlatforms := map[string]bool{}
		for _, platform := range game.Platforms {
			platform.Name = strings.TrimSpace(platform.Name)
			if platform.ExternalID == "" || platform.Name == "" {
				return nil, fmt.Errorf("%w: source platforms require external ID and name", ErrInvalidRequest)
			}
			if !seenPlatforms[platform.ExternalID] {
				seenPlatforms[platform.ExternalID] = true
				platforms = append(platforms, platform)
			}
		}
		game.Platforms = platforms
		result = append(result, game)
	}
	return result, nil
}

// lockIdentities serializes concurrent imports of the same external identity.
// Keys are acquired in one global order (platforms, then games, each sorted)
// so overlapping imports cannot deadlock.
func lockIdentities(ctx context.Context, tx *sql.Tx, games []SourceGame) error {
	var platformKeys, gameKeys []string
	for _, game := range games {
		gameKeys = append(gameKeys, sourceKey{game.Source, game.ExternalID}.lockKey("game"))
		for _, platform := range game.Platforms {
			platformKeys = append(platformKeys, sourceKey{game.Source, platform.ExternalID}.lockKey("platform"))
		}
	}
	slices.Sort(platformKeys)
	slices.Sort(gameKeys)
	for _, key := range slices.Concat(slices.Compact(platformKeys), gameKeys) {
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

func upsertGame(ctx context.Context, tx *sql.Tx, game SourceGame, platformIDs []string) (string, error) {
	cover := sql.NullString{String: game.CoverURL, Valid: game.CoverURL != ""}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT game_id FROM catalog.external_game_ids
		WHERE source = $1 AND external_id = $2`, game.Source, game.ExternalID).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err := tx.QueryRowContext(ctx, `INSERT INTO catalog.games(title, cover_url) VALUES ($1, $2) RETURNING id`,
			game.Title, cover).Scan(&id); err != nil {
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
	_, err = tx.ExecContext(ctx, `UPDATE catalog.games SET title = $2, cover_url = $3, updated_at = now()
		WHERE id = $1 AND (title IS DISTINCT FROM $2 OR cover_url IS DISTINCT FROM $3 OR $4)`,
		id, game.Title, cover, platformsChanged)
	return id, err
}

func loadGames(ctx context.Context, tx *sql.Tx, ids []string) ([]Game, error) {
	rows, err := tx.QueryContext(ctx, `SELECT g.id, g.title, g.cover_url, g.is_active, g.updated_at, p.id, p.name
		FROM catalog.games g
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
		var cover, platformID, platformName sql.NullString
		if err := rows.Scan(&game.ID, &game.Title, &cover, &game.IsActive, &game.UpdatedAt, &platformID, &platformName); err != nil {
			return nil, err
		}
		existing, ok := byID[game.ID]
		if !ok {
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
