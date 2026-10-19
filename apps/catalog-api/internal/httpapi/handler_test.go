package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLivenessReturnsServiceIdentity(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))
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
			NewHandler().ServeHTTP(response, r)
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
