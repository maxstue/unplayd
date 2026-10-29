package igdb

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
)

type fakeIGDB struct {
	t            *testing.T
	clientID     string
	clientSecret string
	tokenCalls   atomic.Int32
	gameCalls    atomic.Int32
	// tokenStatus overrides the token endpoint response when non-zero.
	tokenStatus int
	tokenBody   string
	games       http.HandlerFunc
}

func newFakeIGDB(t *testing.T, games http.HandlerFunc) (*fakeIGDB, *Client) {
	t.Helper()
	fake := &fakeIGDB{t: t, clientID: rand.Text(), clientSecret: rand.Text(), games: games}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	config, err := Load(credentials(fake.clientID, fake.clientSecret))
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	client.http = server.Client()
	client.apiURL = server.URL + "/v4"
	client.tokenURL = server.URL + "/oauth2/token"
	client.retryDelay = time.Millisecond
	client.rate = &limiter{}
	return fake, client
}

// issuedToken is the token handed out by the n-th successful token request.
func issuedToken(n int32) string { return fmt.Sprintf("token-%d", n) }

func (f *fakeIGDB) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/oauth2/token":
		calls := f.tokenCalls.Add(1)
		if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			f.t.Errorf("unexpected token request: %s %s", r.Method, r.URL)
		}
		if err := r.ParseForm(); err != nil || r.PostForm.Get("client_id") != f.clientID ||
			r.PostForm.Get("client_secret") != f.clientSecret || r.PostForm.Get("grant_type") != "client_credentials" {
			f.t.Error("token request did not carry client credentials in the form body")
		}
		if f.tokenStatus != 0 {
			w.WriteHeader(f.tokenStatus)
			_, _ = io.WriteString(w, f.tokenBody)
			return
		}
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"expires_in":3600,"token_type":"bearer"}`, issuedToken(calls))
	case "/v4/games":
		f.gameCalls.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Client-ID") != f.clientID || r.Header.Get("Content-Type") != "text/plain" {
			f.t.Errorf("unexpected IGDB request headers: %s %v", r.Method, r.Header)
		}
		f.games(w, r)
	default:
		f.t.Errorf("unexpected path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// sequence answers successive calls with the given handlers and repeats the last one.
func sequence(handlers ...http.HandlerFunc) http.HandlerFunc {
	var calls atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		handlers[min(int(calls.Add(1)), len(handlers))-1](w, r)
	}
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Error(err)
	}
	return string(body)
}

func assertNoSecrets(t *testing.T, err error, fake *fakeIGDB) {
	t.Helper()
	for _, value := range []string{fake.clientID, fake.clientSecret, issuedToken(1), "127.0.0.1"} {
		if strings.Contains(err.Error(), value) {
			t.Errorf("error exposes sensitive or internal value: %v", err)
		}
	}
}

func TestSearchMapsGamesWithCoversAndPlatforms(t *testing.T) {
	_, client := newFakeIGDB(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+issuedToken(1) {
			t.Error("IGDB request did not use the issued app token")
		}
		want := `search "Zelda \"Breath\" \\"; fields name,cover.image_id,platforms.name,game_type,parent_game,version_parent; limit 10;`
		if body := readBody(t, r); body != want {
			t.Errorf("query = %q, want %q", body, want)
		}
		_, _ = io.WriteString(w, `[
			{"id": 7346, "name": " The Legend of Zelda: Breath of the Wild ", "cover": {"image_id": "co3p2d"},
			 "platforms": [{"id": 130, "name": "Nintendo Switch"}, {"id": 41, "name": "Wii U"}, {"id": 0, "name": "Broken"}, {"id": 5, "name": " "}]},
			{"id": 1, "name": "Unsafe cover", "cover": {"image_id": "../../evil"}},
			{"id": 2, "name": "No cover"},
			{"id": 3, "name": "   "},
			{"id": 0, "name": "Missing identity"}
		]`)
	})
	games, err := client.Search(t.Context(), " Zelda \"Breath\" \\\x00 ", catalog.SearchFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []catalog.SourceGame{
		{Source: "igdb", ExternalID: "7346", Title: "The Legend of Zelda: Breath of the Wild", Kind: catalog.KindGame,
			CoverURL:  "https://images.igdb.com/igdb/image/upload/t_cover_big/co3p2d.jpg",
			Platforms: []catalog.SourcePlatform{{ExternalID: "130", Name: "Nintendo Switch"}, {ExternalID: "41", Name: "Wii U"}}},
		{Source: "igdb", ExternalID: "1", Title: "Unsafe cover", Kind: catalog.KindGame, Platforms: []catalog.SourcePlatform{}},
		{Source: "igdb", ExternalID: "2", Title: "No cover", Kind: catalog.KindGame, Platforms: []catalog.SourcePlatform{}},
	}
	if !reflect.DeepEqual(games, want) {
		t.Fatalf("mapped games = %+v, want %+v", games, want)
	}
}

func TestGamesRequestsDetailsByExternalID(t *testing.T) {
	_, client := newFakeIGDB(t, func(w http.ResponseWriter, r *http.Request) {
		want := `fields name,cover.image_id,platforms.name,game_type,parent_game,version_parent; where id = (1942,7346); limit 2;`
		if body := readBody(t, r); body != want {
			t.Errorf("query = %q, want %q", body, want)
		}
		_, _ = io.WriteString(w, `[{"id": 1942, "name": "The Witcher 3: Wild Hunt"}]`)
	})
	games, err := client.Games(t.Context(), []string{"1942", "7346"})
	if err != nil || len(games) != 1 || games[0].ExternalID != "1942" {
		t.Fatalf("unexpected details: %+v, %v", games, err)
	}
}

func TestGamesMapKindsAndParents(t *testing.T) {
	// Shapes taken from live IGDB responses for The Witcher 3 and Resident Evil 2.
	_, client := newFakeIGDB(t, respond(http.StatusOK, `[
		{"id": 1942, "name": "The Witcher 3: Wild Hunt", "game_type": 0},
		{"id": 22439, "name": "The Witcher 3: Wild Hunt - Game of the Year Edition", "version_parent": 1942, "game_type": 3},
		{"id": 13166, "name": "The Witcher 3: Wild Hunt - Blood and Wine", "parent_game": 1942, "game_type": 2},
		{"id": 230102, "name": "The Witcher 3: Wild Hunt - Free Next-Gen Update", "parent_game": 1942, "game_type": 14},
		{"id": 141472, "name": "The Witcher 3: Wild Hunt + Dark Souls III", "game_type": 3},
		{"id": 19686, "name": "Resident Evil 2", "parent_game": 880, "game_type": 8},
		{"id": 146147, "name": "Collector's Edition", "parent_game": 122611, "version_parent": 146146, "game_type": 9},
		{"id": 5, "name": "Unknown type", "game_type": 99, "parent_game": 5},
		{"id": 6, "name": "No type"}
	]`))
	games, err := client.Games(t.Context(), []string{"1942"})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind   catalog.Kind
		parent string
	}{
		{catalog.KindGame, ""},
		{catalog.KindEdition, "1942"},
		{catalog.KindExpansion, "1942"},
		{catalog.KindUpdate, "1942"},
		{catalog.KindBundle, ""},
		{catalog.KindRemake, "880"},
		{catalog.KindEdition, "146146"},
		{catalog.KindGame, ""},
		{catalog.KindGame, ""},
	}
	if len(games) != len(want) {
		t.Fatalf("mapped %d games, want %d", len(games), len(want))
	}
	for i, game := range games {
		if game.Kind != want[i].kind || game.ParentExternalID != want[i].parent {
			t.Errorf("%s: kind %q parent %q, want %q %q", game.Title, game.Kind, game.ParentExternalID, want[i].kind, want[i].parent)
		}
	}
}

func TestSearchTranslatesKindFilters(t *testing.T) {
	for _, tc := range []struct {
		name   string
		filter catalog.SearchFilter
		where  string
	}{
		{"no filter", catalog.SearchFilter{}, ""},
		{"parents", catalog.SearchFilter{Parents: true}, " where (version_parent = null & parent_game = null);"},
		{"expansions", catalog.SearchFilter{Kinds: []catalog.Kind{catalog.KindExpansion, catalog.KindStandaloneExpansion}},
			" where (version_parent = null & game_type = (2,4));"},
		{"editions and parents", catalog.SearchFilter{Parents: true, Kinds: []catalog.Kind{catalog.KindEdition, catalog.KindBundle}},
			" where (version_parent = null & parent_game = null) | version_parent != null | (version_parent = null & game_type = (3));"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, client := newFakeIGDB(t, func(w http.ResponseWriter, r *http.Request) {
				want := `search "witcher 3"; fields ` + gameFields + `;` + tc.where + ` limit 5;`
				if body := readBody(t, r); body != want {
					t.Errorf("query = %q, want %q", body, want)
				}
				_, _ = io.WriteString(w, `[]`)
			})
			if _, err := client.Search(t.Context(), "witcher 3", tc.filter, 5); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestChildrenRequestsAllChildrenOfParentsInOneQuery(t *testing.T) {
	fake, client := newFakeIGDB(t, func(w http.ResponseWriter, r *http.Request) {
		want := `fields ` + gameFields + `; where parent_game = (1942,880) | version_parent = (1942,880); sort id asc; limit 500;`
		if body := readBody(t, r); body != want {
			t.Errorf("query = %q, want %q", body, want)
		}
		_, _ = io.WriteString(w, `[{"id": 12503, "name": "Hearts of Stone", "parent_game": 1942, "game_type": 2}]`)
	})
	children, err := client.Children(t.Context(), []string{"1942", "880"})
	if err != nil || len(children) != 1 || children[0].ParentExternalID != "1942" || children[0].Kind != catalog.KindExpansion {
		t.Fatalf("children = %+v, %v", children, err)
	}
	if fake.gameCalls.Load() != 1 {
		t.Fatalf("game calls = %d, want 1", fake.gameCalls.Load())
	}
}

func TestInvalidRequestsAreRejectedWithoutCallingIGDB(t *testing.T) {
	fake, client := newFakeIGDB(t, respond(http.StatusOK, `[]`))
	tooMany := make([]string, maxResultLimit+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprint(i + 1)
	}
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"empty query", func() error { _, err := client.Search(t.Context(), " \n\t", catalog.SearchFilter{}, 10); return err }},
		{"zero limit", func() error { _, err := client.Search(t.Context(), "zelda", catalog.SearchFilter{}, 0); return err }},
		{"limit above IGDB maximum", func() error {
			_, err := client.Search(t.Context(), "zelda", catalog.SearchFilter{}, maxResultLimit+1)
			return err
		}},
		{"unsupported kind", func() error {
			_, err := client.Search(t.Context(), "zelda", catalog.SearchFilter{Kinds: []catalog.Kind{"sequel"}}, 10)
			return err
		}},
		{"no IDs", func() error { _, err := client.Games(t.Context(), nil); return err }},
		{"no parent IDs", func() error { _, err := client.Children(t.Context(), nil); return err }},
		{"too many parent IDs", func() error { _, err := client.Children(t.Context(), tooMany); return err }},
		{"parent ID injection", func() error { _, err := client.Children(t.Context(), []string{"1) | id > (0"}); return err }},
		{"too many IDs", func() error { _, err := client.Games(t.Context(), tooMany); return err }},
		{"zero ID", func() error { _, err := client.Games(t.Context(), []string{"0"}); return err }},
		{"negative ID", func() error { _, err := client.Games(t.Context(), []string{"-1"}); return err }},
		{"leading zero", func() error { _, err := client.Games(t.Context(), []string{"01"}); return err }},
		{"query injection", func() error { _, err := client.Games(t.Context(), []string{"1); fields *"}); return err }},
		{"empty ID", func() error { _, err := client.Games(t.Context(), []string{""}); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrRejected) {
				t.Fatalf("expected rejected request, got %v", err)
			}
		})
	}
	if fake.tokenCalls.Load() != 0 || fake.gameCalls.Load() != 0 {
		t.Fatal("invalid requests reached IGDB")
	}
}

func TestAccessTokenIsCachedUntilShortlyBeforeExpiry(t *testing.T) {
	fake, client := newFakeIGDB(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `[{"id": 1, "name": %q}]`, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	})
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }
	search := func() string {
		t.Helper()
		games, err := client.Search(t.Context(), "zelda", catalog.SearchFilter{}, 1)
		if err != nil {
			t.Fatal(err)
		}
		return games[0].Title
	}
	for range 2 {
		if search() != issuedToken(1) {
			t.Fatal("cached token was not reused")
		}
	}
	now = now.Add(time.Hour - tokenRenewalMargin - time.Second)
	if search() != issuedToken(1) {
		t.Fatal("token renewed too early")
	}
	now = now.Add(time.Second)
	if search() != issuedToken(2) || fake.tokenCalls.Load() != 2 {
		t.Fatalf("token was not renewed before expiry; token calls = %d", fake.tokenCalls.Load())
	}
}

func TestRejectedTokenIsRenewedOnce(t *testing.T) {
	t.Run("renewed token succeeds", func(t *testing.T) {
		fake, client := newFakeIGDB(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "Bearer "+issuedToken(1) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `[]`)
		})
		if _, err := client.Search(t.Context(), "zelda", catalog.SearchFilter{}, 1); err != nil {
			t.Fatal(err)
		}
		if fake.tokenCalls.Load() != 2 || fake.gameCalls.Load() != 2 {
			t.Fatalf("token calls = %d, game calls = %d", fake.tokenCalls.Load(), fake.gameCalls.Load())
		}
	})
	t.Run("persistent rejection stops after one renewal", func(t *testing.T) {
		fake, client := newFakeIGDB(t, respond(http.StatusUnauthorized, ""))
		_, err := client.Search(t.Context(), "zelda", catalog.SearchFilter{}, 1)
		if !errors.Is(err, ErrRejected) || fake.tokenCalls.Load() != 2 || fake.gameCalls.Load() != 2 {
			t.Fatalf("err = %v, token calls = %d, game calls = %d", err, fake.tokenCalls.Load(), fake.gameCalls.Load())
		}
		assertNoSecrets(t, err, fake)
	})
}

func TestTransientFailuresAreRetriedWithinBounds(t *testing.T) {
	ok := respond(http.StatusOK, `[]`)
	for _, tc := range []struct {
		name     string
		handler  http.HandlerFunc
		calls    int32
		expected error
	}{
		{"service unavailable then success", sequence(respond(http.StatusServiceUnavailable, ""), ok), 2, nil},
		{"rate limited then success", sequence(respond(http.StatusTooManyRequests, ""), ok), 2, nil},
		{"two server errors then success", sequence(respond(http.StatusInternalServerError, ""), respond(http.StatusBadGateway, ""), ok), 3, nil},
		{"persistent server errors", respond(http.StatusServiceUnavailable, ""), maxAttempts, ErrUnavailable},
		{"persistent rate limit", respond(http.StatusTooManyRequests, ""), maxAttempts, ErrUnavailable},
		{"bad request is not retried", respond(http.StatusBadRequest, `{"title":"Syntax Error"}`), 1, ErrRejected},
		{"forbidden is not retried", respond(http.StatusForbidden, ""), 1, ErrRejected},
		{"malformed JSON is not retried", respond(http.StatusOK, `{"not":"a list"}`), 1, ErrRejected},
		{"oversized response is not retried", respond(http.StatusOK, "["+strings.Repeat(" ", maxResponseBytes)+"]"), 1, ErrRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, client := newFakeIGDB(t, tc.handler)
			_, err := client.Search(t.Context(), "zelda", catalog.SearchFilter{}, 1)
			if tc.expected == nil && err != nil || tc.expected != nil && !errors.Is(err, tc.expected) {
				t.Fatalf("err = %v, want %v", err, tc.expected)
			}
			if fake.gameCalls.Load() != tc.calls {
				t.Fatalf("game calls = %d, want %d", fake.gameCalls.Load(), tc.calls)
			}
			if err != nil {
				assertNoSecrets(t, err, fake)
			}
		})
	}
}

func TestAttemptTimeoutIsRetriedThenReportedUnavailable(t *testing.T) {
	fake, client := newFakeIGDB(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	})
	client.timeout = 20 * time.Millisecond
	_, err := client.Search(t.Context(), "zelda", catalog.SearchFilter{}, 1)
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "timed out") || fake.gameCalls.Load() != maxAttempts {
		t.Fatalf("err = %v, game calls = %d", err, fake.gameCalls.Load())
	}
	assertNoSecrets(t, err, fake)
}

func TestCanceledContextStopsWithoutRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fake, client := newFakeIGDB(t, func(w http.ResponseWriter, _ *http.Request) {
		cancel()
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	if _, err := client.Search(ctx, "zelda", catalog.SearchFilter{}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if fake.gameCalls.Load() != 1 {
		t.Fatalf("game calls = %d, want 1", fake.gameCalls.Load())
	}
	if _, err := client.Search(ctx, "zelda", catalog.SearchFilter{}, 1); !errors.Is(err, context.Canceled) || fake.gameCalls.Load() != 1 {
		t.Fatalf("canceled context reached IGDB: %v", err)
	}
}

func TestTokenEndpointFailures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		tokenCalls int32
		expected   error
	}{
		{"invalid credentials", http.StatusForbidden, `{"message":"invalid client secret"}`, 1, ErrRejected},
		{"bad request", http.StatusBadRequest, `{"message":"invalid client"}`, 1, ErrRejected},
		{"token service unavailable", http.StatusServiceUnavailable, "", maxAttempts, ErrUnavailable},
		{"malformed token", http.StatusOK, `{"access_token":"","expires_in":3600}`, 1, ErrRejected},
		{"expired token", http.StatusOK, `{"access_token":"abc","expires_in":0}`, 1, ErrRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, client := newFakeIGDB(t, respond(http.StatusOK, `[]`))
			fake.tokenStatus, fake.tokenBody = tc.status, tc.body
			_, err := client.Search(t.Context(), "zelda", catalog.SearchFilter{}, 1)
			if !errors.Is(err, tc.expected) || fake.tokenCalls.Load() != tc.tokenCalls || fake.gameCalls.Load() != 0 {
				t.Fatalf("err = %v, token calls = %d, game calls = %d", err, fake.tokenCalls.Load(), fake.gameCalls.Load())
			}
			assertNoSecrets(t, err, fake)
		})
	}
}

func TestShortTokenLifetimeRenewsAtHalfLife(t *testing.T) {
	fake, client := newFakeIGDB(t, respond(http.StatusOK, `[]`))
	fake.tokenStatus, fake.tokenBody = http.StatusOK, `{"access_token":"short","expires_in":60}`
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }
	if _, err := client.accessToken(t.Context()); err != nil || !client.tokenExpiry.Equal(now.Add(30*time.Second)) {
		t.Fatalf("expiry = %v, err = %v", client.tokenExpiry, err)
	}
}

func TestOpenRequestsAreBounded(t *testing.T) {
	var active, peak atomic.Int32
	_, client := newFakeIGDB(t, func(w http.ResponseWriter, _ *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := peak.Load()
			if current <= observed || peak.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(w, `[]`)
	})
	client.open = make(chan struct{}, 2)
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if _, err := client.Search(t.Context(), "zelda", catalog.SearchFilter{}, 1); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if peak.Load() != 2 {
		t.Fatalf("peak open requests = %d, want 2", peak.Load())
	}
}

func TestRequestsAreSpacedByRateLimit(t *testing.T) {
	_, client := newFakeIGDB(t, respond(http.StatusOK, `[]`))
	client.rate = &limiter{interval: 25 * time.Millisecond}
	started := time.Now()
	for range 4 {
		if _, err := client.Search(t.Context(), "zelda", catalog.SearchFilter{}, 1); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(started); elapsed < 75*time.Millisecond {
		t.Fatalf("four requests took %v, want at least three intervals", elapsed)
	}
}

func TestRetryDelayHonorsBoundedRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{"2", 2 * time.Second},
		{"30", maxRetryAfter},
		{"", 0},
		{"soon", 0},
		{"-1", 0},
	} {
		t.Run(tc.header, func(t *testing.T) {
			if got := parseRetryAfter(tc.header); got != tc.want {
				t.Fatalf("parseRetryAfter(%q) = %v, want %v", tc.header, got, tc.want)
			}
		})
	}
	client := &Client{retryDelay: 100 * time.Millisecond}
	if got := client.backoff(1, 2*time.Second); got != 2*time.Second {
		t.Fatalf("Retry-After was not honored: %v", got)
	}
	for attempt, base := range map[int]time.Duration{1: 100 * time.Millisecond, 2: 200 * time.Millisecond} {
		if got := client.backoff(attempt, 0); got < base || got > base+base/2 {
			t.Fatalf("backoff(%d) = %v, want between %v and %v", attempt, got, base, base+base/2)
		}
	}
}
