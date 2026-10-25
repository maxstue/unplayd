// Package catalog owns catalog games, their external identities and import rules.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	minQueryLength = 2
	maxQueryLength = 200
	maxSearchLimit = 50
	maxImportBatch = 100
	// maxAncestorDepth and maxDescendantDepth bound the provider requests per
	// import that complete parent chains and load children: one request per
	// hierarchy level. Deeper descendants join once they are imported themselves.
	maxAncestorDepth   = 3
	maxDescendantDepth = 2
)

var (
	// ErrInvalidRequest marks caller input that violates the catalog rules.
	ErrInvalidRequest = errors.New("invalid catalog request")
	// ErrNotFound marks a catalog game that does not exist.
	ErrNotFound = errors.New("catalog game not found")
)

// Kind classifies a catalog game. For children it is also the child type.
type Kind string

const (
	KindGame                Kind = "game"
	KindEdition             Kind = "edition"
	KindDLC                 Kind = "dlc"
	KindExpansion           Kind = "expansion"
	KindStandaloneExpansion Kind = "standalone_expansion"
	KindPack                Kind = "pack"
	KindUpdate              Kind = "update"
	KindBundle              Kind = "bundle"
	KindRemake              Kind = "remake"
	KindRemaster            Kind = "remaster"
	KindExpandedGame        Kind = "expanded_game"
	KindPort                Kind = "port"
	KindMod                 Kind = "mod"
	KindFork                Kind = "fork"
	KindEpisode             Kind = "episode"
	KindSeason              Kind = "season"
)

// kinds lists every kind in presentation order.
var kinds = []Kind{
	KindGame, KindEdition, KindDLC, KindExpansion, KindStandaloneExpansion, KindPack, KindUpdate, KindBundle,
	KindRemake, KindRemaster, KindExpandedGame, KindPort, KindMod, KindFork, KindEpisode, KindSeason,
}

// Category is a search filter that groups related kinds.
type Category string

const (
	// CategoryGame selects parents, whatever their kind.
	CategoryGame      Category = "game"
	CategoryDLC       Category = "dlc"
	CategoryExpansion Category = "expansion"
	CategoryEdition   Category = "edition"
	CategoryRemake    Category = "remake"
	CategoryPort      Category = "port"
	CategoryMod       Category = "mod"
	CategoryEpisode   Category = "episode"
)

var categoryKinds = map[Category][]Kind{
	CategoryDLC:       {KindDLC, KindPack, KindUpdate},
	CategoryExpansion: {KindExpansion, KindStandaloneExpansion},
	CategoryEdition:   {KindEdition, KindBundle, KindExpandedGame},
	CategoryRemake:    {KindRemake, KindRemaster},
	CategoryPort:      {KindPort},
	CategoryMod:       {KindMod, KindFork},
	CategoryEpisode:   {KindEpisode, KindSeason},
}

// SearchFilter narrows a search to parents and/or games of the given kinds.
// Both conditions combine as a union; the zero value matches every game.
type SearchFilter struct {
	Parents bool
	Kinds   []Kind
}

func newSearchFilter(categories []Category) (SearchFilter, error) {
	var filter SearchFilter
	for _, category := range categories {
		if category == CategoryGame {
			filter.Parents = true
			continue
		}
		members, ok := categoryKinds[category]
		if !ok {
			return SearchFilter{}, fmt.Errorf("%w: unknown kind filter %q", ErrInvalidRequest, category)
		}
		for _, kind := range members {
			if !slices.Contains(filter.Kinds, kind) {
				filter.Kinds = append(filter.Kinds, kind)
			}
		}
	}
	return filter, nil
}

func (f SearchFilter) empty() bool { return !f.Parents && len(f.Kinds) == 0 }

func (f SearchFilter) matches(game Game) bool {
	return f.empty() || f.Parents && game.Parent == nil || slices.Contains(f.Kinds, game.Kind)
}

// SourcePlatform is a platform as reported by an external provider.
type SourcePlatform struct {
	ExternalID string
	Name       string
}

// SourceGame is provider metadata keyed by the provider's own identity.
type SourceGame struct {
	Source     string
	ExternalID string
	Title      string
	Kind       Kind
	// ParentExternalID is the provider's direct parent of this game, or empty.
	// The catalog links the game to the top of that parent chain.
	ParentExternalID string
	// CoverURL is empty when the provider has no cover.
	CoverURL  string
	Platforms []SourcePlatform
}

type Platform struct {
	ID   string
	Name string
}

// ParentRef identifies the parent of a child game.
type ParentRef struct {
	ID    string
	Title string
}

// Game is a catalog game identified by its stable internal UUID.
type Game struct {
	ID       string
	Title    string
	Kind     Kind
	Parent   *ParentRef
	CoverURL *string
	// Platforms are sorted by name.
	Platforms []Platform
	IsActive  bool
	UpdatedAt time.Time
}

// KindCount is the number of active children of one kind.
type KindCount struct {
	Kind  Kind
	Count int
}

// ChildGroup holds the active children of one kind, sorted by title.
type ChildGroup struct {
	Kind  Kind
	Games []Game
}

// Detail is a game with its children grouped by kind in presentation order.
type Detail struct {
	Game     Game
	Children []ChildGroup
}

// SearchHit is one search result. Parents carry their child counts; without a
// filter they also carry the children that matched the query.
type SearchHit struct {
	Game            Game
	ChildCounts     []KindCount
	MatchedChildren []Game
}

// Source looks up games at an external provider.
type Source interface {
	Search(ctx context.Context, query string, filter SearchFilter, limit int) ([]SourceGame, error)
	Games(ctx context.Context, externalIDs []string) ([]SourceGame, error)
	// Children returns games whose direct parent is one of parentIDs.
	Children(ctx context.Context, parentIDs []string) ([]SourceGame, error)
}

// Importer stores provider results under stable catalog identities.
type Importer struct {
	source Source
	store  *Store
}

func NewImporter(source Source, store *Store) *Importer {
	return &Importer{source: source, store: store}
}

// Search queries the provider by title and upserts the matches with their
// relatives. Without categories every parent appears once, carrying the
// matched children; with categories the matches form a flat list.
func (i *Importer) Search(ctx context.Context, query string, categories []Category, limit int) ([]SearchHit, error) {
	query = strings.TrimSpace(query)
	if length := utf8.RuneCountInString(query); length < minQueryLength || length > maxQueryLength {
		return nil, fmt.Errorf("%w: query must contain %d to %d characters", ErrInvalidRequest, minQueryLength, maxQueryLength)
	}
	if limit < 1 || limit > maxSearchLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidRequest, maxSearchLimit)
	}
	filter, err := newSearchFilter(categories)
	if err != nil {
		return nil, err
	}
	found, err := i.source.Search(ctx, query, filter, limit)
	if err != nil {
		return nil, err
	}
	matches, stored, err := i.save(ctx, found)
	if err != nil {
		return nil, err
	}
	hits := bundle(matches, stored, filter)
	var parentIDs []string
	for _, hit := range hits {
		if hit.Game.Parent == nil {
			parentIDs = append(parentIDs, hit.Game.ID)
		}
	}
	counts, err := i.store.ChildCounts(ctx, parentIDs)
	if err != nil {
		return nil, err
	}
	for index := range hits {
		if hit := &hits[index]; hit.Game.Parent == nil {
			hit.ChildCounts = counts[hit.Game.ID]
		}
	}
	return hits, nil
}

// bundle turns matches into hits: with a filter as a flat list of matching
// games, otherwise one hit per parent in order of its first match.
func bundle(matches, stored []Game, filter SearchFilter) []SearchHit {
	hits := []SearchHit{}
	if !filter.empty() {
		for _, game := range matches {
			if filter.matches(game) {
				hits = append(hits, SearchHit{Game: game, ChildCounts: []KindCount{}, MatchedChildren: []Game{}})
			}
		}
		return hits
	}
	byID := make(map[string]Game, len(stored))
	for _, game := range stored {
		byID[game.ID] = game
	}
	index := map[string]int{}
	for _, game := range matches {
		parent, isChild := game, false
		if game.Parent != nil {
			parent, isChild = byID[game.Parent.ID]
		}
		if !isChild {
			parent = game
		}
		position, ok := index[parent.ID]
		if !ok {
			position = len(hits)
			index[parent.ID] = position
			hits = append(hits, SearchHit{Game: parent, ChildCounts: []KindCount{}, MatchedChildren: []Game{}})
		}
		if isChild {
			hits[position].MatchedChildren = append(hits[position].MatchedChildren, game)
		}
	}
	return hits
}

// Import fetches provider details for the given external IDs and upserts them
// with their relatives. The result holds the requested games found at the provider.
func (i *Importer) Import(ctx context.Context, externalIDs []string) ([]Game, error) {
	if len(externalIDs) < 1 || len(externalIDs) > maxImportBatch {
		return nil, fmt.Errorf("%w: import requires 1 to %d external IDs", ErrInvalidRequest, maxImportBatch)
	}
	found, err := i.source.Games(ctx, externalIDs)
	if err != nil {
		return nil, err
	}
	matches, _, err := i.save(ctx, found)
	return matches, err
}

// save upserts found together with its relatives in one transaction and
// returns the stored matches in input order plus every stored game.
func (i *Importer) save(ctx context.Context, found []SourceGame) ([]Game, []Game, error) {
	batch, err := i.withRelatives(ctx, found)
	if err != nil {
		return nil, nil, err
	}
	stored, err := i.store.Upsert(ctx, batch.games)
	if err != nil {
		return nil, nil, err
	}
	return stored[:batch.matches], stored, nil
}

// sourceBatch is a duplicate-free list of source games; the first matches
// entries are the requested games.
type sourceBatch struct {
	games   []SourceGame
	index   map[sourceKey]int
	matches int
}

func (b *sourceBatch) add(game SourceGame) bool {
	key := sourceKey{game.Source, game.ExternalID}
	if _, ok := b.index[key]; ok {
		return false
	}
	b.index[key] = len(b.games)
	b.games = append(b.games, game)
	return true
}

func (b *sourceBatch) hasParent(game SourceGame) bool {
	_, ok := b.index[sourceKey{game.Source, game.ParentExternalID}]
	return ok
}

// missingParents lists the parents referenced by the batch but not part of it.
func (b *sourceBatch) missingParents() []string {
	var missing []string
	for _, game := range b.games {
		if game.ParentExternalID != "" && !b.hasParent(game) && !slices.Contains(missing, game.ParentExternalID) {
			missing = append(missing, game.ParentExternalID)
		}
	}
	return missing
}

// withRelatives completes found with the parent chains of its games and the
// children of every top-level game, using one provider request per level.
// Games whose parents the provider no longer knows become parents themselves.
// Children beyond the provider's per-request limit are left to later refreshes.
func (i *Importer) withRelatives(ctx context.Context, found []SourceGame) (sourceBatch, error) {
	batch := sourceBatch{index: map[sourceKey]int{}}
	for _, game := range found {
		batch.add(game)
	}
	batch.matches = len(batch.games)
	for range maxAncestorDepth {
		missing := batch.missingParents()
		if len(missing) == 0 {
			break
		}
		parents, err := i.source.Games(ctx, missing)
		if err != nil {
			return sourceBatch{}, err
		}
		for _, parent := range parents {
			batch.add(parent)
		}
	}
	var parentIDs []string
	for index, game := range batch.games {
		if game.ParentExternalID != "" && !batch.hasParent(game) {
			batch.games[index].ParentExternalID = ""
		}
		if batch.games[index].ParentExternalID == "" {
			parentIDs = append(parentIDs, game.ExternalID)
		}
	}
	queried := map[string]bool{}
	for depth := 0; depth < maxDescendantDepth && len(parentIDs) > 0; depth++ {
		children, err := i.source.Children(ctx, parentIDs)
		if err != nil {
			return sourceBatch{}, err
		}
		for _, id := range parentIDs {
			queried[id] = true
		}
		parentIDs = nil
		for _, child := range children {
			// A child that names a parent outside the batch belongs elsewhere.
			if !batch.hasParent(child) {
				continue
			}
			batch.add(child)
			if !queried[child.ExternalID] && !slices.Contains(parentIDs, child.ExternalID) {
				parentIDs = append(parentIDs, child.ExternalID)
			}
		}
	}
	return batch, nil
}
