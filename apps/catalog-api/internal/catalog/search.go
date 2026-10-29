package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lib/pq"
)

const (
	maxBatchSize    = 100
	maxCursorLength = 512
	// Ranks order local search hits: exact title, title prefix, every query
	// word in the title, and games only the provider matched for the query.
	rankExact    = 0
	rankProvider = 3
)

var errInvalidCursor = fmt.Errorf("%w: cursor does not belong to this search", ErrInvalidRequest)

// SearchRequest is a search as received from a caller. Empty PlatformID and
// Cursor mean no platform filter and the first page.
type SearchRequest struct {
	Query      string
	Categories []Category
	PlatformID string
	Limit      int
	Cursor     string
}

// SearchPage is one page of search hits. NextCursor is empty on the last page.
type SearchPage struct {
	Hits       []SearchHit
	NextCursor string
}

// Batch classifies requested catalog IDs. Each ID appears in exactly one list,
// in request order.
type Batch struct {
	Games    []Game
	Missing  []string
	Inactive []string
}

// localQuery is a validated search over stored games.
type localQuery struct {
	// key identifies the provider search: normalized query and categories.
	key        string
	text       string
	filter     SearchFilter
	categories []Category
	platformID string
	limit      int
	after      *cursor
}

func newLocalQuery(request SearchRequest) (localQuery, error) {
	text, err := validateQuery(request.Query)
	if err != nil {
		return localQuery{}, err
	}
	if err := validateLimit(request.Limit); err != nil {
		return localQuery{}, err
	}
	categories := slices.Clone(request.Categories)
	slices.Sort(categories)
	if len(slices.Compact(slices.Clone(categories))) != len(categories) {
		return localQuery{}, fmt.Errorf("%w: kind filters must be unique", ErrInvalidRequest)
	}
	filter, err := newSearchFilter(categories)
	if err != nil {
		return localQuery{}, err
	}
	query := localQuery{text: text, filter: filter, categories: categories, limit: request.Limit}
	if request.PlatformID != "" {
		if query.platformID, err = canonicalID(request.PlatformID); err != nil {
			return localQuery{}, fmt.Errorf("%w: platformId must be a UUID", ErrInvalidRequest)
		}
	}
	names := make([]string, len(categories))
	for index, category := range categories {
		names[index] = string(category)
	}
	query.key = strings.ToLower(text) + "\x1f" + strings.Join(names, ",")
	if request.Cursor != "" {
		if query.after, err = decodeCursor(request.Cursor, query.fingerprint()); err != nil {
			return localQuery{}, err
		}
	}
	return query, nil
}

// fingerprint binds cursors to the search they were issued for.
func (q localQuery) fingerprint() string {
	sum := sha256.Sum256([]byte(q.key + "\x1f" + q.platformID))
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

// patterns returns ILIKE patterns that each require one query word.
func (q localQuery) patterns() []string {
	words := strings.Fields(q.text)
	patterns := make([]string, len(words))
	for index, word := range words {
		patterns[index] = "%" + escapeLike(word) + "%"
	}
	return patterns
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

// cursor is the position of the last hit of a page.
type cursor struct {
	Rank        int    `json:"r"`
	ID          string `json:"i"`
	Fingerprint string `json:"f"`
}

func (c cursor) encode() string {
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(value, fingerprint string) (*cursor, error) {
	if len(value) > maxCursorLength {
		return nil, errInvalidCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, errInvalidCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var position cursor
	if err := decoder.Decode(&position); err != nil || decoder.More() {
		return nil, errInvalidCursor
	}
	if position.Fingerprint != fingerprint || position.Rank < rankExact || position.Rank > rankProvider || !uuidPattern.MatchString(position.ID) {
		return nil, errInvalidCursor
	}
	return &position, nil
}

// canonicalID returns id in the lowercase form PostgreSQL returns UUIDs in.
func canonicalID(id string) (string, error) {
	if !uuidPattern.MatchString(id) {
		return "", fmt.Errorf("%w: game IDs must be UUIDs", ErrInvalidRequest)
	}
	return strings.ToLower(id), nil
}

// searchIsFresh reports whether the provider answered the search within maxAge.
func (s *Store) searchIsFresh(ctx context.Context, key string, maxAge time.Duration) (bool, error) {
	var fresh bool
	err := s.db.QueryRowContext(ctx, `SELECT refreshed_at > now() - make_interval(secs => $2)
		FROM catalog.search_queries WHERE query_key = $1`, key, maxAge.Seconds()).Scan(&fresh)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return fresh, err
}

// recordSearch stores the games the provider returned for a search.
func (s *Store) recordSearch(ctx context.Context, key string, gameIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO catalog.search_queries(query_key, refreshed_at) VALUES ($1, now())
		ON CONFLICT (query_key) DO UPDATE SET refreshed_at = excluded.refreshed_at`, key); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM catalog.search_query_games WHERE query_key = $1`, key); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO catalog.search_query_games(query_key, game_id)
		SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING`, key, pq.Array(gameIDs)); err != nil {
		return err
	}
	return tx.Commit()
}

// searchSQL finds the hits of a page. Candidates are active games whose title
// contains the longest query word, which the trigram index answers, and every
// query word, plus the games the provider returned for the search.
const searchSQL = `WITH candidates AS (
		SELECT id FROM catalog.games
		WHERE is_active AND title ILIKE $12::text AND title ILIKE ALL($2::text[])
		UNION
		SELECT game_id FROM catalog.search_query_games WHERE query_key = $1::text
	), matches AS (
		SELECT g.id, lower(g.title) AS sort_title,
			CASE WHEN $8::boolean THEN g.id ELSE coalesce(g.parent_id, g.id) END AS hit_id,
			CASE WHEN lower(g.title) = lower($3::text) THEN 0
				WHEN g.title ILIKE $4::text THEN 1
				WHEN g.title ILIKE ALL($2::text[]) THEN 2
				ELSE 3 END AS rank
		FROM candidates c JOIN catalog.games g ON g.id = c.id
		WHERE g.is_active
			AND ($5::uuid IS NULL OR EXISTS (SELECT 1 FROM catalog.game_platforms gp
				WHERE gp.game_id = g.id AND gp.platform_id = $5::uuid))
			AND (NOT $8::boolean OR ($6::boolean AND g.parent_id IS NULL) OR g.kind = ANY($7::text[]))
	), hits AS (
		SELECT m.hit_id, min(m.rank) AS rank, lower(h.title) AS sort_title,
			coalesce(array_agg(m.id ORDER BY m.rank, m.sort_title, m.id) FILTER (WHERE m.id <> m.hit_id), '{}') AS children
		FROM matches m JOIN catalog.games h ON h.id = m.hit_id AND h.is_active
		GROUP BY m.hit_id, h.title
	)
	SELECT hit_id, rank, children FROM hits
	WHERE $9::uuid IS NULL
		OR (rank, sort_title, hit_id) > ($10::int, (SELECT lower(title) FROM catalog.games WHERE id = $9::uuid), $9::uuid)
	ORDER BY rank, sort_title, hit_id
	LIMIT $11`

func (q localQuery) searchArgs() []any {
	var afterID sql.NullString
	afterRank := 0
	if q.after != nil {
		afterID, afterRank = sql.NullString{String: q.after.ID, Valid: true}, q.after.Rank
	}
	patterns := q.patterns()
	longest := slices.MaxFunc(patterns, func(a, b string) int { return len(a) - len(b) })
	return []any{
		q.key, pq.Array(patterns), q.text, escapeLike(q.text) + "%",
		sql.NullString{String: q.platformID, Valid: q.platformID != ""},
		q.filter.Parents, pq.Array(q.filter.Kinds), !q.filter.empty(),
		afterID, afterRank, q.limit + 1, longest,
	}
}

// search answers q from stored active games. Games match when their title
// contains every query word or the provider returned them for the same search.
// Without a kind filter every match counts for its parent, which becomes the
// hit; with a filter the matching games themselves are the hits. Hits are
// ordered by their best rank, title and ID; next is nil on the last page.
func (s *Store) search(ctx context.Context, q localQuery) ([]SearchHit, *cursor, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, searchSQL, q.searchArgs()...)
	if err != nil {
		return nil, nil, err
	}
	type row struct {
		id       string
		rank     int
		children []string
	}
	var found []row
	for rows.Next() {
		var hit row
		if err := rows.Scan(&hit.id, &hit.rank, pq.Array(&hit.children)); err != nil {
			rows.Close()
			return nil, nil, err
		}
		found = append(found, hit)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *cursor
	if len(found) > q.limit {
		found = found[:q.limit]
		last := found[len(found)-1]
		next = &cursor{Rank: last.rank, ID: last.id, Fingerprint: q.fingerprint()}
	}
	var ids, parentIDs []string
	for _, hit := range found {
		ids = append(ids, hit.id)
		ids = append(ids, hit.children...)
	}
	games, err := loadGames(ctx, tx, ids)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[string]Game, len(games))
	for _, game := range games {
		byID[game.ID] = game
		if game.Parent == nil {
			parentIDs = append(parentIDs, game.ID)
		}
	}
	counts, err := childCounts(ctx, tx, parentIDs)
	if err != nil {
		return nil, nil, err
	}
	hits := make([]SearchHit, 0, len(found))
	for _, row := range found {
		hit := newHit(byID[row.id])
		if hit.Game.Parent == nil && counts[row.id] != nil {
			hit.ChildCounts = counts[row.id]
		}
		for _, child := range row.children {
			hit.MatchedChildren = append(hit.MatchedChildren, byID[child])
		}
		hits = append(hits, hit)
	}
	return hits, next, tx.Commit()
}

// Batch resolves 1 to 100 unique catalog IDs with one query. IDs are matched
// case-insensitively and returned in canonical lowercase form.
func (s *Store) Batch(ctx context.Context, ids []string) (Batch, error) {
	if len(ids) < 1 || len(ids) > maxBatchSize {
		return Batch{}, fmt.Errorf("%w: a batch requires 1 to %d game IDs", ErrInvalidRequest, maxBatchSize)
	}
	canonical := make([]string, len(ids))
	seen := make(map[string]bool, len(ids))
	for index, id := range ids {
		id, err := canonicalID(id)
		if err != nil {
			return Batch{}, err
		}
		if seen[id] {
			return Batch{}, fmt.Errorf("%w: game ID %s is requested more than once", ErrInvalidRequest, id)
		}
		seen[id] = true
		canonical[index] = id
	}
	games, err := loadGames(ctx, s.db, canonical)
	if err != nil {
		return Batch{}, err
	}
	byID := make(map[string]Game, len(games))
	for _, game := range games {
		byID[game.ID] = game
	}
	batch := Batch{Games: []Game{}, Missing: []string{}, Inactive: []string{}}
	for _, id := range canonical {
		game, ok := byID[id]
		switch {
		case !ok:
			batch.Missing = append(batch.Missing, id)
		case !game.IsActive:
			batch.Inactive = append(batch.Inactive, id)
		default:
			batch.Games = append(batch.Games, game)
		}
	}
	return batch, nil
}
