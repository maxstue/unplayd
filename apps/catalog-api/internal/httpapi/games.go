package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
)

const (
	defaultSearchLimit = 20
	// maxBatchBody comfortably fits 100 UUIDs with formatting.
	maxBatchBody = 16 << 10
)

type api struct {
	games  Catalog
	logger *slog.Logger
}

type platformDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type parentDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type gameDTO struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Kind      catalog.Kind  `json:"kind"`
	Parent    *parentDTO    `json:"parent"`
	CoverURL  *string       `json:"coverUrl"`
	Platforms []platformDTO `json:"platforms"`
	IsActive  bool          `json:"isActive"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

type kindCountDTO struct {
	Kind  catalog.Kind `json:"kind"`
	Count int          `json:"count"`
}

type childGroupDTO struct {
	Kind  catalog.Kind `json:"kind"`
	Games []gameDTO    `json:"games"`
}

type gameDetailDTO struct {
	Game     gameDTO         `json:"game"`
	Children []childGroupDTO `json:"children"`
}

type searchHitDTO struct {
	Game            gameDTO        `json:"game"`
	ChildCounts     []kindCountDTO `json:"childCounts"`
	MatchedChildren []gameDTO      `json:"matchedChildren"`
}

type gamePageDTO struct {
	Items      []searchHitDTO `json:"items"`
	NextCursor *string        `json:"nextCursor"`
}

type batchRequestDTO struct {
	GameIDs *[]string `json:"gameIds"`
}

type batchResponseDTO struct {
	Items           []gameDTO `json:"items"`
	MissingGameIDs  []string  `json:"missingGameIds"`
	InactiveGameIDs []string  `json:"inactiveGameIds"`
}

func (a api) search(w http.ResponseWriter, r *http.Request) {
	request, message := parseSearch(r.URL.RawQuery)
	if message != "" {
		writeError(w, r, http.StatusBadRequest, "invalid_request", message)
		return
	}
	page, err := a.games.Search(r.Context(), request)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	body := gamePageDTO{Items: make([]searchHitDTO, len(page.Hits))}
	for index, hit := range page.Hits {
		counts := make([]kindCountDTO, len(hit.ChildCounts))
		for i, count := range hit.ChildCounts {
			counts[i] = kindCountDTO(count)
		}
		body.Items[index] = searchHitDTO{Game: newGameDTO(hit.Game), ChildCounts: counts, MatchedChildren: newGameDTOs(hit.MatchedChildren)}
	}
	if page.NextCursor != "" {
		body.NextCursor = &page.NextCursor
	}
	writeJSON(w, http.StatusOK, body)
}

// parseSearch reads the search parameters, or returns a public message for
// malformed ones. Value rules are checked by the catalog.
func parseSearch(rawQuery string) (catalog.SearchRequest, string) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return catalog.SearchRequest{}, "The query string is malformed."
	}
	for _, name := range []string{"query", "platformId", "limit", "cursor"} {
		if len(values[name]) > 1 {
			return catalog.SearchRequest{}, name + " must be given at most once."
		}
	}
	if !values.Has("query") {
		return catalog.SearchRequest{}, "query is required."
	}
	request := catalog.SearchRequest{Query: values.Get("query"), PlatformID: values.Get("platformId"), Cursor: values.Get("cursor"), Limit: defaultSearchLimit}
	if values.Has("platformId") && request.PlatformID == "" {
		return catalog.SearchRequest{}, "platformId must be a UUID."
	}
	if values.Has("cursor") && request.Cursor == "" {
		return catalog.SearchRequest{}, "cursor must not be empty."
	}
	if values.Has("limit") {
		limit, err := strconv.Atoi(values.Get("limit"))
		if err != nil {
			return catalog.SearchRequest{}, "limit must be an integer."
		}
		request.Limit = limit
	}
	for _, kind := range values["kind"] {
		request.Categories = append(request.Categories, catalog.Category(kind))
	}
	return request, ""
}

func (a api) detail(w http.ResponseWriter, r *http.Request) {
	detail, err := a.games.Detail(r.Context(), r.PathValue("gameId"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	body := gameDetailDTO{Game: newGameDTO(detail.Game), Children: make([]childGroupDTO, len(detail.Children))}
	for index, group := range detail.Children {
		body.Children[index] = childGroupDTO{Kind: group.Kind, Games: newGameDTOs(group.Games)}
	}
	writeJSON(w, http.StatusOK, body)
}

func (a api) batch(w http.ResponseWriter, r *http.Request) {
	if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Content-Type must be application/json.")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBatchBody))
	decoder.DisallowUnknownFields()
	var request batchRequestDTO
	if err := decoder.Decode(&request); err != nil || request.GameIDs == nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, r, http.StatusBadRequest, "invalid_request", `The body must be a JSON object {"gameIds": [...]}.`)
		return
	}
	batch, err := a.games.Batch(r.Context(), *request.GameIDs)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, batchResponseDTO{Items: newGameDTOs(batch.Games), MissingGameIDs: batch.Missing, InactiveGameIDs: batch.Inactive})
}

// fail translates catalog errors into the contract's error responses.
func (a api) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, catalog.ErrUnavailable):
		a.logger.WarnContext(r.Context(), "catalog request unavailable", "error", err.Error())
		writeError(w, r, http.StatusServiceUnavailable, "unavailable", "The game provider is unavailable. Try again later.")
	case errors.Is(err, catalog.ErrInvalidRequest):
		message := strings.TrimPrefix(err.Error(), catalog.ErrInvalidRequest.Error()+": ")
		writeError(w, r, http.StatusBadRequest, "invalid_request", strings.ToUpper(message[:1])+message[1:]+".")
	case errors.Is(err, catalog.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "Game not found.")
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		a.logger.InfoContext(r.Context(), "catalog request canceled by the caller")
		writeError(w, r, http.StatusServiceUnavailable, "unavailable", "The request was canceled.")
	default:
		a.logger.ErrorContext(r.Context(), "catalog request failed", "error", err.Error())
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal error.")
	}
}

func newGameDTO(game catalog.Game) gameDTO {
	dto := gameDTO{
		ID: game.ID, Title: game.Title, Kind: game.Kind, CoverURL: game.CoverURL,
		Platforms: make([]platformDTO, len(game.Platforms)), IsActive: game.IsActive, UpdatedAt: game.UpdatedAt,
	}
	if game.Parent != nil {
		dto.Parent = &parentDTO{ID: game.Parent.ID, Title: game.Parent.Title}
	}
	for index, platform := range game.Platforms {
		dto.Platforms[index] = platformDTO(platform)
	}
	return dto
}

func newGameDTOs(games []catalog.Game) []gameDTO {
	dtos := make([]gameDTO, len(games))
	for index, game := range games {
		dtos[index] = newGameDTO(game)
	}
	return dtos
}
