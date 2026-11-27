package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/requestid"
)

// requestTimeout bounds each request, including catalog and provider calls.
const requestTimeout = 3 * time.Second

var requestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// isPublic reports whether r targets a health route, the only routes that need no service token.
func isPublic(r *http.Request) bool {
	return r.Method == http.MethodGet && (r.URL.Path == "/health/live" || r.URL.Path == "/health/ready")
}

// withTimeout limits the request context to requestTimeout. A shorter
// deadline set by the caller is kept.
func withTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// withRequestID adopts a valid X-Request-ID or generates a new one, stores it
// in the request context for logs and errors, and returns it to the caller.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !requestIDPattern.MatchString(id) {
			id = rand.Text()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(requestid.With(r.Context(), id)))
	})
}

// withLogging writes one entry per request. Health probes log at debug
// level. Query strings are left out because they can carry user input.
func withLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		defer func() {
			level := slog.LevelInfo
			if isPublic(r) {
				level = slog.LevelDebug
			}
			logger.Log(r.Context(), level, "request completed", "method", r.Method, "path", r.URL.Path,
				"status", recorder.code(), "durationMs", time.Since(started).Milliseconds())
		}()
		next.ServeHTTP(recorder, r)
	})
}

// withAuth requires the service token on every route except the health
// routes and marks those responses as not cacheable.
func withAuth(serviceToken string, next http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(serviceToken))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublic(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		// An empty configured token rejects every request instead of matching an empty credential.
		if serviceToken == "" || !validBearer(r.Header.Values("Authorization"), expected) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// validBearer reports whether the Authorization header values hold exactly one
// "Bearer <token>" credential whose token hashes to expected. The scheme is
// case-insensitive; the token must be separated by a single space and contain
// no whitespace. Hashes are compared in constant time.
func validBearer(values []string, expected [sha256.Size]byte) bool {
	if len(values) != 1 {
		return false
	}
	scheme, token, found := strings.Cut(values[0], " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return false
	}
	provided := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(expected[:], provided[:]) == 1
}

// statusRecorder remembers the response status for the request log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}

func (r *statusRecorder) code() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}
