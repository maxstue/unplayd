package igdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
)

const (
	source           = "igdb"
	defaultAPIURL    = "https://api.igdb.com/v4"
	defaultTokenURL  = "https://id.twitch.tv/oauth2/token"
	coverURLPrefix   = "https://images.igdb.com/igdb/image/upload/t_cover_big/"
	gameFields       = "name,cover.image_id,platforms.name"
	maxResultLimit   = 500
	maxAttempts      = 3
	maxRetryAfter    = 5 * time.Second
	maxResponseBytes = 1 << 20
	// IGDB allows four requests per second and eight open requests per client.
	requestInterval = 250 * time.Millisecond
	maxOpenRequests = 8
	// Tokens are renewed this long before Twitch reports their expiry.
	tokenRenewalMargin = time.Minute
)

var (
	// ErrUnavailable marks transient IGDB failures that persisted through all retries.
	ErrUnavailable = errors.New("igdb unavailable")
	// ErrRejected marks requests IGDB refused permanently, including rejected credentials.
	ErrRejected = errors.New("igdb rejected the request")

	imageIDPattern = regexp.MustCompile(`^[a-z0-9]{1,64}$`)
)

// Client calls IGDB with cached app tokens, client-side rate limits and
// bounded retries for transient failures.
type Client struct {
	config     Config
	http       *http.Client
	apiURL     string
	tokenURL   string
	timeout    time.Duration
	retryDelay time.Duration
	open       chan struct{}
	rate       *limiter
	now        func() time.Time

	// tokenLock is a context-aware mutex guarding token and tokenExpiry.
	tokenLock   chan struct{}
	token       string
	tokenExpiry time.Time
}

// NewClient returns a client for enabled IGDB configuration.
func NewClient(config Config) (*Client, error) {
	if !config.Enabled() {
		return nil, errors.New("IGDB integration is not configured")
	}
	return &Client{
		config:     config,
		http:       &http.Client{},
		apiURL:     defaultAPIURL,
		tokenURL:   defaultTokenURL,
		timeout:    5 * time.Second,
		retryDelay: 250 * time.Millisecond,
		open:       make(chan struct{}, maxOpenRequests),
		rate:       &limiter{interval: requestInterval},
		now:        time.Now,
		tokenLock:  make(chan struct{}, 1),
	}, nil
}

// Search returns games whose title matches query, excluding editions of other games.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]catalog.SourceGame, error) {
	query = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(query))
	if query == "" || limit < 1 || limit > maxResultLimit {
		return nil, fmt.Errorf("%w: search requires a query and a limit between 1 and %d", ErrRejected, maxResultLimit)
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(query)
	return c.games(ctx, fmt.Sprintf(`search "%s"; fields %s; where version_parent = null; limit %d;`, escaped, gameFields, limit))
}

// Games returns details for the given IGDB game IDs. Unknown IDs are omitted.
func (c *Client) Games(ctx context.Context, externalIDs []string) ([]catalog.SourceGame, error) {
	if len(externalIDs) < 1 || len(externalIDs) > maxResultLimit {
		return nil, fmt.Errorf("%w: details require 1 to %d IDs", ErrRejected, maxResultLimit)
	}
	for _, id := range externalIDs {
		if value, err := strconv.ParseUint(id, 10, 63); err != nil || value == 0 || strconv.FormatUint(value, 10) != id {
			return nil, fmt.Errorf("%w: IGDB IDs must be positive integers", ErrRejected)
		}
	}
	return c.games(ctx, fmt.Sprintf(`fields %s; where id = (%s); limit %d;`, gameFields, strings.Join(externalIDs, ","), len(externalIDs)))
}

type igdbGame struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Cover *struct {
		ImageID string `json:"image_id"`
	} `json:"cover"`
	Platforms []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"platforms"`
}

func (c *Client) games(ctx context.Context, query string) ([]catalog.SourceGame, error) {
	body, err := c.post(ctx, c.apiURL+"/games", query)
	if err != nil {
		return nil, err
	}
	var found []igdbGame
	if err := json.Unmarshal(body, &found); err != nil {
		return nil, fmt.Errorf("%w: malformed games response", ErrRejected)
	}
	games := make([]catalog.SourceGame, 0, len(found))
	for _, game := range found {
		if mapped, ok := mapGame(game); ok {
			games = append(games, mapped)
		}
	}
	return games, nil
}

// mapGame skips entries the catalog cannot store and drops invalid optional data.
func mapGame(game igdbGame) (catalog.SourceGame, bool) {
	title := strings.TrimSpace(game.Name)
	if game.ID <= 0 || title == "" {
		return catalog.SourceGame{}, false
	}
	mapped := catalog.SourceGame{
		Source: source, ExternalID: strconv.FormatInt(game.ID, 10), Title: title,
		Platforms: []catalog.SourcePlatform{},
	}
	if game.Cover != nil && imageIDPattern.MatchString(game.Cover.ImageID) {
		mapped.CoverURL = coverURLPrefix + game.Cover.ImageID + ".jpg"
	}
	for _, platform := range game.Platforms {
		if name := strings.TrimSpace(platform.Name); platform.ID > 0 && name != "" {
			mapped.Platforms = append(mapped.Platforms, catalog.SourcePlatform{ExternalID: strconv.FormatInt(platform.ID, 10), Name: name})
		}
	}
	return mapped, true
}

// post sends an Apicalypse query, retrying transient failures and renewing a
// rejected token once. Cancellation of ctx stops immediately.
func (c *Client) post(ctx context.Context, endpoint, query string) ([]byte, error) {
	renewed := false
	for attempt := 1; ; attempt++ {
		token, err := c.accessToken(ctx)
		var body []byte
		var status int
		var retryAfter time.Duration
		if err == nil {
			status, body, retryAfter, err = c.send(ctx, endpoint, query, token)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		switch {
		case errors.Is(err, ErrRejected):
			return nil, err
		case err == nil && status == http.StatusOK:
			return body, nil
		case err == nil && status == http.StatusUnauthorized && !renewed:
			c.invalidate(token)
			renewed = true
			attempt--
			continue
		case err == nil && status != http.StatusTooManyRequests && status < http.StatusInternalServerError:
			return nil, fmt.Errorf("%w: status %d", ErrRejected, status)
		}
		if attempt == maxAttempts {
			if err == nil {
				err = fmt.Errorf("status %d", status)
			}
			return nil, fmt.Errorf("%w after %d attempts: %v", ErrUnavailable, attempt, err)
		}
		if err := sleep(ctx, c.backoff(attempt, retryAfter)); err != nil {
			return nil, err
		}
	}
}

func (c *Client) send(ctx context.Context, endpoint, query, token string) (int, []byte, time.Duration, error) {
	if err := acquire(ctx, c.open); err != nil {
		return 0, nil, 0, err
	}
	defer func() { <-c.open }()
	if err := c.rate.wait(ctx); err != nil {
		return 0, nil, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(query))
	if err != nil {
		return 0, nil, 0, fmt.Errorf("%w: invalid request", ErrRejected)
	}
	request.Header.Set("Client-ID", c.config.clientID)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "text/plain")
	status, body, header, err := c.do(request)
	if err != nil {
		return 0, nil, 0, err
	}
	return status, body, parseRetryAfter(header.Get("Retry-After")), nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// accessToken returns a cached app token or obtains one via the client credentials grant.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	if err := acquire(ctx, c.tokenLock); err != nil {
		return "", err
	}
	defer func() { <-c.tokenLock }()
	if c.token != "" && c.now().Before(c.tokenExpiry) {
		return c.token, nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// Credentials travel in the form body so they never appear in URLs or URL errors.
	form := url.Values{"client_id": {c.config.clientID}, "client_secret": {c.config.clientSecret}, "grant_type": {"client_credentials"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("%w: invalid token request", ErrRejected)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	status, body, _, err := c.do(request)
	switch {
	case err != nil:
		return "", fmt.Errorf("token request: %w", err)
	case status == http.StatusTooManyRequests || status >= http.StatusInternalServerError:
		return "", fmt.Errorf("token request status %d", status)
	case status != http.StatusOK:
		return "", fmt.Errorf("%w: token request status %d; check IGDB credentials", ErrRejected, status)
	}
	var token tokenResponse
	if err := json.Unmarshal(body, &token); err != nil || !validCredential(token.AccessToken) || token.ExpiresIn <= 0 {
		return "", fmt.Errorf("%w: malformed token response", ErrRejected)
	}
	lifetime := time.Duration(token.ExpiresIn) * time.Second
	if lifetime > 2*tokenRenewalMargin {
		lifetime -= tokenRenewalMargin
	} else {
		lifetime /= 2
	}
	c.token, c.tokenExpiry = token.AccessToken, c.now().Add(lifetime)
	return c.token, nil
}

func (c *Client) invalidate(token string) {
	c.tokenLock <- struct{}{}
	defer func() { <-c.tokenLock }()
	if c.token == token {
		c.token = ""
	}
}

// do executes request and reads a bounded body. Transport errors are reported
// without the request URL or headers.
func (c *Client) do(request *http.Request) (int, []byte, http.Header, error) {
	response, err := c.http.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, nil, nil, errors.New("request timed out")
		}
		return 0, nil, nil, errors.New("request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return 0, nil, nil, errors.New("reading response failed")
	}
	if len(body) > maxResponseBytes {
		return 0, nil, nil, fmt.Errorf("%w: response exceeds %d bytes", ErrRejected, maxResponseBytes)
	}
	return response.StatusCode, body, response.Header, nil
}

func (c *Client) backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return min(retryAfter, maxRetryAfter)
	}
	delay := c.retryDelay << (attempt - 1)
	return delay + rand.N(delay/2+1)
}

func parseRetryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0
	}
	return min(time.Duration(seconds)*time.Second, maxRetryAfter)
}

func acquire(ctx context.Context, slots chan struct{}) error {
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// limiter spaces requests at a fixed interval across all callers.
type limiter struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	slot := l.next
	if slot.Before(now) {
		slot = now
	}
	l.next = slot.Add(l.interval)
	l.mu.Unlock()
	return sleep(ctx, slot.Sub(now))
}
