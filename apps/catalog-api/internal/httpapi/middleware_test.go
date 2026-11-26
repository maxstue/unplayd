package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/requestid"
)

func TestRequestIDIsValidatedAndReturned(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		preserve bool
	}{
		{"valid", "trace_123-ABC", true},
		{"maximum length", strings.Repeat("a", 64), true},
		{"missing", "", false},
		{"invalid characters", "invalid request id", false},
		{"too long", strings.Repeat("a", 65), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/health/live", nil)
			r.Header.Set("X-Request-ID", tc.id)
			response := httptest.NewRecorder()
			newTestHandler(strings.Repeat("x", 32), &fakeCatalog{}).ServeHTTP(response, r)
			id := response.Header().Get("X-Request-ID")
			if !requestIDPattern.MatchString(id) {
				t.Fatalf("invalid response request ID: %q", id)
			}
			if tc.preserve && id != tc.id {
				t.Fatalf("valid request ID changed: %q", id)
			}
			if !tc.preserve && id == tc.id {
				t.Fatalf("invalid request ID was reused")
			}
		})
	}
}

func TestValidBearer(t *testing.T) {
	token := "service-token"
	expected := sha256.Sum256([]byte(token))
	for _, tc := range []struct {
		name   string
		values []string
		want   bool
	}{
		{"exact", []string{"Bearer " + token}, true},
		{"lowercase scheme", []string{"bearer " + token}, true},
		{"uppercase scheme", []string{"BEARER " + token}, true},
		{"none", nil, false},
		{"empty", []string{""}, false},
		{"two values", []string{"Bearer " + token, "Bearer " + token}, false},
		{"wrong token", []string{"Bearer other"}, false},
		{"token prefix", []string{"Bearer service"}, false},
		{"other scheme", []string{"Basic " + token}, false},
		{"no scheme", []string{token}, false},
		{"empty token", []string{"Bearer "}, false},
		{"double space", []string{"Bearer  " + token}, false},
		{"tab separator", []string{"Bearer\t" + token}, false},
		{"trailing newline", []string{"Bearer " + token + "\n"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validBearer(tc.values, expected); got != tc.want {
				t.Fatalf("validBearer(%q) = %v, want %v", tc.values, got, tc.want)
			}
		})
	}
}

func TestProtectedRoutesRequireExactlyOneValidBearerToken(t *testing.T) {
	token := rand.Text() + rand.Text()
	handler := newTestHandler(token, &fakeCatalog{})
	for _, tc := range []struct {
		name    string
		headers []string
	}{
		{"missing", nil},
		{"wrong token", []string{"Bearer " + rand.Text()}},
		{"duplicate valid", []string{"Bearer " + token, "Bearer " + token}},
		{"duplicate mixed", []string{"Bearer " + token, "Bearer " + rand.Text()}},
		{"wrong scheme", []string{"Basic " + token}},
		{"missing scheme", []string{token}},
		{"missing token", []string{"Bearer "}},
		{"missing separator", []string{"Bearer" + token}},
		{"tab separator", []string{"Bearer\t" + token}},
		{"extra space", []string{"Bearer  " + token}},
		{"trailing whitespace", []string{"Bearer " + token + " "}},
		{"comma joined", []string{"Bearer " + token + ", Bearer " + token}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := requestWithAuthorization(handler, tc.headers)
			assertUnauthorizedResponse(t, response)
			assertNoReflectedTokens(t, response.Body.String(), append([]string{token}, tc.headers...))
		})
	}
}

func TestValidBearerReachesRouting(t *testing.T) {
	token := rand.Text() + rand.Text()
	for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
		t.Run(scheme, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
			req.Header.Set("Authorization", scheme+" "+token)
			response := httptest.NewRecorder()
			newTestHandler(token, &fakeCatalog{}).ServeHTTP(response, req)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want authenticated 404", response.Code)
			}
			assertError(t, response, http.StatusNotFound, "not_found")
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Error("authenticated response must not be cached")
			}
			if !requestIDPattern.MatchString(response.Header().Get("X-Request-ID")) {
				t.Error("missing generated request ID")
			}
		})
	}
}

func TestOnlyExactGetLivenessIsPublic(t *testing.T) {
	handler := newTestHandler(rand.Text()+rand.Text(), &fakeCatalog{})
	for _, tc := range []struct{ method, path string }{
		{http.MethodHead, "/health/live"},
		{http.MethodPost, "/health/live"},
		{http.MethodGet, "/health/live/"},
		{http.MethodGet, "/health/live/extra"},
		{http.MethodGet, "/unknown"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("nonpublic route status = %d, want 401", response.Code)
			}
		})
	}
}

func TestEmptyConfiguredTokenFailsClosed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/games", nil)
	req.Header.Set("Authorization", "Bearer ")
	response := httptest.NewRecorder()
	newTestHandler("", &fakeCatalog{}).ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func requestWithAuthorization(handler http.Handler, headers []string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/games", nil)
	req.Header.Set("X-Request-ID", "auth_rejected-123")
	for _, value := range headers {
		req.Header.Add("Authorization", value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func assertUnauthorizedResponse(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	for name, expected := range map[string]string{
		"Cache-Control":    "no-store",
		"WWW-Authenticate": "Bearer",
		"X-Request-ID":     "auth_rejected-123",
		"Content-Type":     "application/json",
	} {
		if response.Header().Get(name) != expected {
			t.Errorf("unexpected authentication response header %s", name)
		}
	}
	var body struct {
		Error struct{ Code, Message, RequestID string }
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "unauthorized" || body.Error.Message != "Unauthorized" || body.Error.RequestID != "auth_rejected-123" {
		t.Error("unexpected authentication error envelope")
	}
}

func assertNoReflectedTokens(t *testing.T, body string, values []string) {
	t.Helper()
	for _, value := range values {
		if len(value) > 10 && strings.Contains(body, value) {
			t.Error("response reflected authentication material")
		}
	}
}

func TestCatalogRequestsHaveABoundedDeadline(t *testing.T) {
	games := &fakeCatalog{}
	before := time.Now()
	serve(newTestHandler(testToken, games), http.MethodGet, "/games?query=halo", "")
	if games.deadline.IsZero() || games.deadline.After(before.Add(3*time.Second+100*time.Millisecond)) {
		t.Fatalf("request deadline=%v, want at most three seconds", games.deadline)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/games?query=halo", nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+testToken)
	newTestHandler(testToken, games).ServeHTTP(httptest.NewRecorder(), request)
	expected, _ := ctx.Deadline()
	if !games.deadline.Equal(expected) {
		t.Fatal("shorter caller deadline was not preserved")
	}
}

func TestRequestLogsCarryRequestIDWithoutCredentials(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(requestid.LogHandler{Handler: slog.NewJSONHandler(&logs, nil)})
	token := rand.Text() + rand.Text()
	handler := NewHandler(token, &fakeCatalog{err: errors.New("database down")}, logger)
	req := httptest.NewRequest(http.MethodGet, "/games?query=zelda", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Request-ID", "log_trace-42")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	var entries []map[string]any
	for line := range strings.Lines(logs.String()) {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	if len(entries) != 2 {
		t.Fatalf("log entries = %v, want failure and completion", entries)
	}
	for _, entry := range entries {
		if entry["requestId"] != "log_trace-42" {
			t.Fatalf("log entry without request ID: %v", entry)
		}
	}
	if entries[0]["error"] != "database down" || entries[1]["status"] != float64(500) || entries[1]["path"] != "/games" {
		t.Fatalf("log entries = %v", entries)
	}
	if strings.Contains(logs.String(), token) || strings.Contains(logs.String(), "zelda") {
		t.Fatal("logs contain credentials or query strings")
	}
}
