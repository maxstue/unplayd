// Package databasetest starts isolated PostgreSQL instances for tests of
// packages that use the catalog schema.
package databasetest

import (
	"crypto/rand"
	"database/sql"
	"net/url"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/database"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Open starts PostgreSQL with the shared bootstrap, applies the production
// migrations and returns a connection as the restricted catalog role.
func Open(t testing.TB) *sql.DB {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate the shared PostgreSQL bootstrap")
	}
	catalogPassword := rand.Text()
	container, err := postgres.Run(t.Context(), "postgres:18.3-bookworm",
		postgres.WithDatabase("unplayd"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword(rand.Text()),
		postgres.WithInitScripts(filepath.Join(filepath.Dir(filename), "../../../../../infra/postgres/init.sh")),
		testcontainers.WithEnv(map[string]string{
			"CATALOG_DB_PASSWORD":  catalogPassword,
			"USER_API_DB_PASSWORD": rand.Text(),
		}),
		postgres.BasicWaitStrategies(),
	)
	if container != nil {
		testcontainers.CleanupContainer(t, container)
	}
	if err != nil {
		t.Fatalf("start PostgreSQL (Docker is required): %v", err)
	}
	connection, err := container.ConnectionString(t.Context(), "sslmode=disable")
	if err != nil {
		t.Fatalf("resolve PostgreSQL's mapped port: %v", err)
	}
	endpoint, err := url.Parse(connection)
	if err != nil {
		t.Fatal("parse PostgreSQL endpoint")
	}
	endpoint.User = nil
	values := map[string]string{"DATABASE_URL": endpoint.String(), "DATABASE_PASSWORD": catalogPassword}
	config, err := database.Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(t.Context(), config, nil); err != nil {
		t.Fatalf("apply production migrations: %v", err)
	}
	db, err := database.Open(t.Context(), config)
	if err != nil {
		t.Fatalf("open catalog connection: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close PostgreSQL connection: %v", err)
		}
	})
	return db
}
