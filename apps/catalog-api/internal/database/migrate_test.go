package database

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type postgresFixture struct {
	db            *sql.DB
	config        Config
	foreignConfig Config
}

func newPostgresFixture(t *testing.T) postgresFixture {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate the shared PostgreSQL bootstrap")
	}
	catalogPassword, userPassword := rand.Text(), rand.Text()
	container, err := postgres.Run(t.Context(), "postgres:18.3-bookworm",
		postgres.WithDatabase("unplayd"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword(rand.Text()),
		postgres.WithInitScripts(filepath.Join(filepath.Dir(filename), "../../../../infra/postgres/init.sh")),
		testcontainers.WithEnv(map[string]string{
			"CATALOG_DB_PASSWORD":  catalogPassword,
			"USER_API_DB_PASSWORD": userPassword,
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
	values := map[string]string{
		"DATABASE_URL":      endpoint.String(),
		"DATABASE_USER":     "catalog",
		"DATABASE_PASSWORD": catalogPassword,
	}
	config, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	db := openFixtureDatabase(t, config.connectionString())
	var restricted bool
	if err := db.QueryRowContext(t.Context(), `SELECT current_user = 'catalog'
		AND NOT rolsuper AND NOT rolcreaterole AND NOT rolcreatedb AND NOT rolbypassrls
		FROM pg_roles WHERE rolname = current_user`).Scan(&restricted); err != nil {
		t.Fatal(err)
	}
	if !restricted {
		t.Fatal("migrations must run as the restricted catalog role")
	}
	return postgresFixture{
		db: db, config: config,
		foreignConfig: Config{endpoint: *endpoint, user: "user_api", password: userPassword},
	}
}

func openFixtureDatabase(t *testing.T, connection string) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close PostgreSQL connection: %v", err)
		}
	})
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	return db
}

func applyMigrations(t *testing.T, config Config, target *uint) {
	t.Helper()
	if err := Migrate(t.Context(), config, target); err != nil {
		t.Fatalf("apply production migrations: %v", err)
	}
}

func executeSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("execute fixture SQL: %v", err)
	}
}

func assertPostgresError(t *testing.T, db *sql.DB, code, query string, args ...any) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), query, args...)
	var postgresError *pq.Error
	if !errors.As(err, &postgresError) {
		t.Fatalf("expected PostgreSQL SQLSTATE %s, got %v", code, err)
	}
	if string(postgresError.Code) != code {
		t.Fatalf("expected PostgreSQL SQLSTATE %s, got %s: %v", code, postgresError.Code, err)
	}
}

func insertGame(t *testing.T, db *sql.DB, title string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(t.Context(), `INSERT INTO catalog.games(title) VALUES ($1) RETURNING id`, title).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func assertSQLTrue(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	var result bool
	if err := db.QueryRowContext(t.Context(), query, args...).Scan(&result); err != nil {
		t.Fatal(err)
	}
	if !result {
		t.Fatal("database state did not satisfy the expected invariant")
	}
}

func TestMigrateUpgradesAndRepeatsWithoutChangingCatalogIdentity(t *testing.T) {
	fixture := newPostgresFixture(t)
	v1, v2 := uint(1), uint(2)
	applyMigrations(t, fixture.config, &v1)
	gameID := insertGame(t, fixture.db, "V1 fixture game")
	var createdAt, updatedAt time.Time
	if err := fixture.db.QueryRowContext(t.Context(), `SELECT created_at, updated_at FROM catalog.games WHERE id = $1`, gameID).Scan(&createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	assertSQLTrue(t, fixture.db, `SELECT version = 1 AND NOT dirty FROM catalog.schema_migrations`)
	applyMigrations(t, fixture.config, nil)
	assertSQLTrue(t, fixture.db, `SELECT title = 'V1 fixture game' AND is_active
		AND created_at = $2 AND updated_at = $3 FROM catalog.games WHERE id = $1`, gameID, createdAt, updatedAt)
	executeSQL(t, fixture.db, `INSERT INTO catalog.external_game_ids(source, external_id, game_id) VALUES ('igdb', '42', $1)`, gameID)

	t.Run("repeat latest and explicit target preserve data and external identity", func(t *testing.T) {
		applyMigrations(t, fixture.config, nil)
		applyMigrations(t, fixture.config, &v2)
		assertSQLTrue(t, fixture.db, `SELECT count(*) = 1 FROM catalog.games`)
		assertSQLTrue(t, fixture.db, `SELECT title = 'V1 fixture game' AND is_active
			AND created_at = $2 AND updated_at = $3 FROM catalog.games WHERE id = $1`, gameID, createdAt, updatedAt)
		assertSQLTrue(t, fixture.db, `SELECT count(*) = 1 AND bool_and(game_id = $1::uuid)
			FROM catalog.external_game_ids WHERE source = 'igdb' AND external_id = '42'`, gameID)
		assertSQLTrue(t, fixture.db, `SELECT version = 2 AND NOT dirty FROM catalog.schema_migrations`)
	})
	t.Run("downgrade is refused without changing version or data", func(t *testing.T) {
		err := Migrate(t.Context(), fixture.config, &v1)
		if err == nil || !strings.Contains(err.Error(), "refusing downgrade from version 2") {
			t.Fatalf("expected forward-only migration error, got %v", err)
		}
		assertSQLTrue(t, fixture.db, `SELECT version = 2 AND NOT dirty FROM catalog.schema_migrations`)
		assertSQLTrue(t, fixture.db, `SELECT count(*) = 1 FROM catalog.external_game_ids WHERE game_id = $1`, gameID)
	})
}

func TestExternalGameIDsEnforceSourceIdentity(t *testing.T) {
	fixture := newPostgresFixture(t)
	applyMigrations(t, fixture.config, nil)
	gameID := insertGame(t, fixture.db, "First game")
	otherGameID := insertGame(t, fixture.db, "Second game")
	executeSQL(t, fixture.db, `INSERT INTO catalog.external_game_ids(source, external_id, game_id)
		VALUES ('igdb', '42', $1), ('steam', '42', $1), ('igdb', '43', $2)`, gameID, otherGameID)
	for _, tc := range []struct {
		name, source, externalID, gameID, code string
	}{
		{"duplicate mapping", "igdb", "42", gameID, "23505"},
		{"reassign existing external identity", "igdb", "42", otherGameID, "23505"},
		{"unknown game", "igdb", "44", "10000000-0000-4000-8000-000000000099", "23503"},
		{"empty source", "", "45", gameID, "23514"},
		{"empty external identity", "igdb", "", gameID, "23514"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertPostgresError(t, fixture.db, tc.code, `INSERT INTO catalog.external_game_ids(source, external_id, game_id)
				VALUES ($1, $2, $3)`, tc.source, tc.externalID, tc.gameID)
		})
	}
	assertSQLTrue(t, fixture.db, `SELECT count(*) = 3 FROM catalog.external_game_ids`)
	assertSQLTrue(t, fixture.db, `SELECT game_id = $1::uuid FROM catalog.external_game_ids WHERE source = 'igdb' AND external_id = '42'`, gameID)
}

func TestGamePlatformsEnforceLocalRelationships(t *testing.T) {
	fixture := newPostgresFixture(t)
	applyMigrations(t, fixture.config, nil)
	gameID := insertGame(t, fixture.db, "Platform fixture")
	var platformID string
	if err := fixture.db.QueryRowContext(t.Context(), `INSERT INTO catalog.platforms(name) VALUES ('PC') RETURNING id`).Scan(&platformID); err != nil {
		t.Fatal(err)
	}
	executeSQL(t, fixture.db, `INSERT INTO catalog.game_platforms(game_id, platform_id) VALUES ($1, $2)`, gameID, platformID)
	missingID := "10000000-0000-4000-8000-000000000099"
	for _, tc := range []struct {
		name, gameID, platformID, code string
	}{
		{"unknown platform", gameID, missingID, "23503"},
		{"unknown game", missingID, platformID, "23503"},
		{"duplicate association", gameID, platformID, "23505"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertPostgresError(t, fixture.db, tc.code, `INSERT INTO catalog.game_platforms(game_id, platform_id) VALUES ($1, $2)`, tc.gameID, tc.platformID)
		})
	}
	t.Run("referenced platform cannot be deleted", func(t *testing.T) {
		assertPostgresError(t, fixture.db, "23001", `DELETE FROM catalog.platforms WHERE id = $1`, platformID)
	})
	assertSQLTrue(t, fixture.db, `SELECT count(*) = 1 FROM catalog.game_platforms WHERE game_id = $1 AND platform_id = $2`, gameID, platformID)
}

func TestGameDeactivationPreservesIdentityAndRejectsHardDelete(t *testing.T) {
	fixture := newPostgresFixture(t)
	applyMigrations(t, fixture.config, nil)
	gameID := insertGame(t, fixture.db, "Lifecycle fixture")
	t.Run("hard delete is rejected even without external references", func(t *testing.T) {
		assertPostgresError(t, fixture.db, "P0001", `DELETE FROM catalog.games WHERE id = $1`, gameID)
		assertSQLTrue(t, fixture.db, `SELECT is_active FROM catalog.games WHERE id = $1`, gameID)
	})
	executeSQL(t, fixture.db, `INSERT INTO catalog.external_game_ids(source, external_id, game_id) VALUES ('igdb', '42', $1)`, gameID)
	t.Run("deactivation retains the game and external mapping", func(t *testing.T) {
		executeSQL(t, fixture.db, `UPDATE catalog.games SET is_active = false WHERE id = $1`, gameID)
		assertSQLTrue(t, fixture.db, `SELECT NOT g.is_active AND g.title = 'Lifecycle fixture' AND e.game_id = $1::uuid
			FROM catalog.games g JOIN catalog.external_game_ids e ON e.game_id = g.id
			WHERE g.id = $1 AND e.source = 'igdb' AND e.external_id = '42'`, gameID)
	})
}

func TestCatalogRoleIsIsolatedBySharedPostgresBootstrap(t *testing.T) {
	fixture := newPostgresFixture(t)
	applyMigrations(t, fixture.config, nil)
	// This probe belongs only to the fixture; no User API migration or model is imported.
	foreignEndpoint := fixture.foreignConfig.endpoint
	foreignEndpoint.User = url.UserPassword(fixture.foreignConfig.user, fixture.foreignConfig.password)
	foreignDB := openFixtureDatabase(t, foreignEndpoint.String())
	executeSQL(t, foreignDB, `CREATE TABLE user_data.catalog_boundary_probe(id integer PRIMARY KEY)`)
	executeSQL(t, foreignDB, `INSERT INTO user_data.catalog_boundary_probe VALUES (1)`)
	assertSQLTrue(t, foreignDB, `SELECT count(*) = 1 FROM user_data.catalog_boundary_probe`)
	// Object grants make the denied reads and writes specifically test the schema boundary.
	executeSQL(t, foreignDB, `GRANT SELECT, INSERT ON user_data.catalog_boundary_probe TO catalog`)

	t.Run("own schema supports create write and read", func(t *testing.T) {
		executeSQL(t, fixture.db, `CREATE TABLE catalog.ownership_probe(id integer PRIMARY KEY)`)
		executeSQL(t, fixture.db, `INSERT INTO catalog.ownership_probe VALUES (1)`)
		assertSQLTrue(t, fixture.db, `SELECT count(*) = 1 FROM catalog.ownership_probe`)
		assertSQLTrue(t, fixture.db, `SELECT current_schema() = 'catalog'`)
	})
	for _, tc := range []struct{ name, query string }{
		{"foreign table read", `SELECT * FROM user_data.catalog_boundary_probe`},
		{"foreign table write", `INSERT INTO user_data.catalog_boundary_probe VALUES (2)`},
		{"foreign schema creation", `CREATE TABLE user_data.forbidden_probe(id integer)`},
		{"public schema creation", `CREATE TABLE public.forbidden_probe(id integer)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertPostgresError(t, fixture.db, "42501", tc.query)
		})
	}
	t.Run("non catalog migration role is rejected", func(t *testing.T) {
		err := Migrate(t.Context(), fixture.foreignConfig, nil)
		if err == nil || !strings.Contains(err.Error(), "migration connection must use the catalog role") {
			t.Fatalf("expected migration role rejection, got %v", err)
		}
	})
}
