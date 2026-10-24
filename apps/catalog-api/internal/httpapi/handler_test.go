package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLivenessReturnsServiceIdentity(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler(strings.Repeat("x", 32)).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected response: %d %v", response.Code, response.Header())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["service"] != "catalog-api" {
		t.Fatalf("unexpected health payload: %v", body)
	}
}

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
			NewHandler(strings.Repeat("x", 32)).ServeHTTP(response, r)
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

func TestProtectedRoutesRequireExactlyOneValidBearerToken(t *testing.T) {
	token := rand.Text() + rand.Text()
	handler := NewHandler(token)
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
			req := httptest.NewRequest(http.MethodGet, "/games", nil)
			req.Header.Set("X-Request-ID", "auth_rejected-123")
			for _, value := range tc.headers {
				req.Header.Add("Authorization", value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", response.Code)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Error("authentication error must not be cached")
			}
			if response.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Error("missing bearer challenge")
			}
			if response.Header().Get("X-Request-ID") != "auth_rejected-123" {
				t.Error("request ID was lost on authentication error")
			}
			if response.Header().Get("Content-Type") != "application/json" {
				t.Error("unexpected authentication error media type")
			}
			var problem struct {
				Error struct {
					Code      string
					Message   string
					RequestID string
				}
			}
			if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if problem.Error.Code != "unauthorized" || problem.Error.Message != "Unauthorized" || problem.Error.RequestID != "auth_rejected-123" {
				t.Error("unexpected authentication problem details")
			}
			if strings.Contains(response.Body.String(), token) {
				t.Error("response leaked configured token")
			}
			for _, header := range tc.headers {
				if len(header) > 10 && strings.Contains(response.Body.String(), header) {
					t.Error("response reflected authorization header")
				}
			}
		})
	}
}

func TestValidBearerReachesRouting(t *testing.T) {
	token := rand.Text() + rand.Text()
	for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
		t.Run(scheme, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/games", nil)
			req.Header.Set("Authorization", scheme+" "+token)
			response := httptest.NewRecorder()
			NewHandler(token).ServeHTTP(response, req)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want authenticated 404", response.Code)
			}
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
	handler := NewHandler(rand.Text() + rand.Text())
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
	NewHandler("").ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}
