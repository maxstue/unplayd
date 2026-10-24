// Package catalog owns catalog games, their external identities and import rules.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	minQueryLength = 2
	maxQueryLength = 200
	maxSearchLimit = 50
	maxImportBatch = 100
)

// ErrInvalidRequest marks caller input that violates the catalog rules.
var ErrInvalidRequest = errors.New("invalid catalog request")

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
	// CoverURL is empty when the provider has no cover.
	CoverURL  string
	Platforms []SourcePlatform
}

type Platform struct {
	ID   string
	Name string
}

// Game is a catalog game identified by its stable internal UUID.
type Game struct {
	ID        string
	Title     string
	CoverURL  *string
	Platforms []Platform
	IsActive  bool
	UpdatedAt time.Time
}

// Source looks up games at an external provider.
type Source interface {
	Search(ctx context.Context, query string, limit int) ([]SourceGame, error)
	Games(ctx context.Context, externalIDs []string) ([]SourceGame, error)
}

// Importer stores provider results under stable catalog identities.
type Importer struct {
	source Source
	store  *Store
}

func NewImporter(source Source, store *Store) *Importer {
	return &Importer{source: source, store: store}
}

// Search queries the provider by title and upserts the matches in provider order.
func (i *Importer) Search(ctx context.Context, query string, limit int) ([]Game, error) {
	query = strings.TrimSpace(query)
	if length := utf8.RuneCountInString(query); length < minQueryLength || length > maxQueryLength {
		return nil, fmt.Errorf("%w: query must contain %d to %d characters", ErrInvalidRequest, minQueryLength, maxQueryLength)
	}
	if limit < 1 || limit > maxSearchLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidRequest, maxSearchLimit)
	}
	found, err := i.source.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	return i.store.Upsert(ctx, found)
}

// Import fetches provider details for the given external IDs and upserts them.
func (i *Importer) Import(ctx context.Context, externalIDs []string) ([]Game, error) {
	if len(externalIDs) < 1 || len(externalIDs) > maxImportBatch {
		return nil, fmt.Errorf("%w: import requires 1 to %d external IDs", ErrInvalidRequest, maxImportBatch)
	}
	found, err := i.source.Games(ctx, externalIDs)
	if err != nil {
		return nil, err
	}
	return i.store.Upsert(ctx, found)
}
