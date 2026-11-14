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

func TestDatabaseStatementAndLockWaitsAreBounded(t *testing.T) {
	fixture := newPostgresFixture(t)
	config := fixture.config
	query := config.endpoint.Query()
	query.Set("statement_timeout", "50")
	query.Set("lock_timeout", "25")
	config.endpoint.RawQuery = query.Encode()
	db, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	assertState := func(err error, state string) {
		t.Helper()
		var failure *pq.Error
		if !errors.As(err, &failure) || string(failure.Code) != state {
			t.Fatalf("error=%v, want SQLSTATE %s", err, state)
		}
	}
	_, err = db.ExecContext(t.Context(), "SELECT pg_sleep(1)")
	assertState(err, "57014")
	executeSQL(t, fixture.db, "CREATE TABLE catalog.timeout_probe(id integer)")
	lock, err := fixture.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	if _, err := lock.ExecContext(t.Context(), "LOCK TABLE catalog.timeout_probe IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(t.Context(), "SELECT * FROM catalog.timeout_probe")
	assertState(err, "55P03")
}

func newPostgresFixture(t *testing.T) postgresFixture {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate the shared PostgreSQL bootstrap")
	}
	catalogPassword, userPassword := rand.Text(), rand.Text()
	// renovate: datasource=docker depName=postgres
	const image = "postgres:18.6-bookworm"
	container, err := postgres.Run(t.Context(), image,
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
	db, err := Open(t.Context(), config)
	if err != nil {
		t.Fatalf("open catalog connection: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close PostgreSQL connection: %v", err)
		}
	})
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
	if err := db.QueryRowContext(t.Context(), `INSERT INTO catalog.games(title, kind) VALUES ($1, 'game') RETURNING id`, title).Scan(&id); err != nil {
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
	v1, latest := uint(1), uint(8)
	applyMigrations(t, fixture.config, &v1)
	var gameID string
	var createdAt, updatedAt time.Time
	if err := fixture.db.QueryRowContext(t.Context(), `INSERT INTO catalog.games(title) VALUES ('V1 fixture game')
		RETURNING id, created_at, updated_at`).Scan(&gameID, &createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	assertSQLTrue(t, fixture.db, `SELECT version = 1 AND NOT dirty FROM catalog.schema_migrations`)
	applyMigrations(t, fixture.config, nil)
	assertSQLTrue(t, fixture.db, `SELECT title = 'V1 fixture game' AND is_active AND kind = 'game' AND parent_id IS NULL
		AND created_at = $2 AND updated_at = $3 FROM catalog.games WHERE id = $1`, gameID, createdAt, updatedAt)
	executeSQL(t, fixture.db, `INSERT INTO catalog.external_game_ids(source, external_id, game_id) VALUES ('igdb', '42', $1)`, gameID)

	t.Run("repeat latest and explicit target preserve data and external identity", func(t *testing.T) {
		applyMigrations(t, fixture.config, nil)
		applyMigrations(t, fixture.config, &latest)
		assertSQLTrue(t, fixture.db, `SELECT count(*) = 1 FROM catalog.games`)
		assertSQLTrue(t, fixture.db, `SELECT title = 'V1 fixture game' AND is_active
			AND created_at = $2 AND updated_at = $3 FROM catalog.games WHERE id = $1`, gameID, createdAt, updatedAt)
		assertSQLTrue(t, fixture.db, `SELECT count(*) = 1 AND bool_and(game_id = $1::uuid)
			FROM catalog.external_game_ids WHERE source = 'igdb' AND external_id = '42'`, gameID)
		assertSQLTrue(t, fixture.db, `SELECT version = 8 AND NOT dirty FROM catalog.schema_migrations`)
	})
	t.Run("downgrade is refused without changing version or data", func(t *testing.T) {
		err := Migrate(t.Context(), fixture.config, &v1)
		if err == nil || !strings.Contains(err.Error(), "refusing downgrade from version 8") {
			t.Fatalf("expected forward-only migration error, got %v", err)
		}
		assertSQLTrue(t, fixture.db, `SELECT version = 8 AND NOT dirty FROM catalog.schema_migrations`)
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

func TestExternalPlatformIDsEnforceSourceIdentity(t *testing.T) {
	fixture := newPostgresFixture(t)
	applyMigrations(t, fixture.config, nil)
	var platformID, otherPlatformID string
	if err := fixture.db.QueryRowContext(t.Context(), `INSERT INTO catalog.platforms(name) VALUES ('PC') RETURNING id`).Scan(&platformID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRowContext(t.Context(), `INSERT INTO catalog.platforms(name) VALUES ('Switch') RETURNING id`).Scan(&otherPlatformID); err != nil {
		t.Fatal(err)
	}
	executeSQL(t, fixture.db, `INSERT INTO catalog.external_platform_ids(source, external_id, platform_id)
		VALUES ('igdb', '6', $1), ('steam', '6', $1)`, platformID)
	for _, tc := range []struct {
		name, source, externalID, platformID, code string
	}{
		{"reassign existing external identity", "igdb", "6", otherPlatformID, "23505"},
		{"unknown platform", "igdb", "7", "10000000-0000-4000-8000-000000000099", "23503"},
		{"empty source", "", "8", platformID, "23514"},
		{"empty external identity", "igdb", "", platformID, "23514"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertPostgresError(t, fixture.db, tc.code, `INSERT INTO catalog.external_platform_ids(source, external_id, platform_id)
				VALUES ($1, $2, $3)`, tc.source, tc.externalID, tc.platformID)
		})
	}
	t.Run("mapped platform cannot be deleted", func(t *testing.T) {
		assertPostgresError(t, fixture.db, "23001", `DELETE FROM catalog.platforms WHERE id = $1`, platformID)
	})
	assertSQLTrue(t, fixture.db, `SELECT count(*) = 2 AND bool_and(platform_id = $1::uuid) FROM catalog.external_platform_ids`, platformID)
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

func TestSearchCacheReferencesStoredGames(t *testing.T) {
	fixture := newPostgresFixture(t)
	applyMigrations(t, fixture.config, nil)
	gameID := insertGame(t, fixture.db, "Search fixture")
	executeSQL(t, fixture.db, `INSERT INTO catalog.search_queries(query_key, refreshed_at) VALUES ('botw', now())`)
	executeSQL(t, fixture.db, `INSERT INTO catalog.search_query_games(query_key, game_id) VALUES ('botw', $1)`, gameID)
	for _, tc := range []struct{ name, code, query string }{
		{"empty key", "23514", `INSERT INTO catalog.search_queries(query_key, refreshed_at) VALUES ('', now())`},
		{"oversized key", "23514", `INSERT INTO catalog.search_queries(query_key, refreshed_at) VALUES (repeat('a', 401), now())`},
		{"unknown search", "23503", `INSERT INTO catalog.search_query_games(query_key, game_id) VALUES ('zelda', '` + gameID + `')`},
		{"unknown game", "23503", `INSERT INTO catalog.search_query_games(query_key, game_id) VALUES ('botw', gen_random_uuid())`},
		{"duplicate game", "23505", `INSERT INTO catalog.search_query_games(query_key, game_id) VALUES ('botw', '` + gameID + `')`},
	} {
		t.Run(tc.name+" is rejected", func(t *testing.T) {
			assertPostgresError(t, fixture.db, tc.code, tc.query)
		})
	}
	t.Run("dropping a search removes its results but not the game", func(t *testing.T) {
		executeSQL(t, fixture.db, `DELETE FROM catalog.search_queries WHERE query_key = 'botw'`)
		assertSQLTrue(t, fixture.db, `SELECT count(*) = 0 FROM catalog.search_query_games`)
		assertSQLTrue(t, fixture.db, `SELECT count(*) = 1 FROM catalog.games WHERE id = $1`, gameID)
	})
}

func insertChild(t *testing.T, db *sql.DB, title, parentID string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(t.Context(), `INSERT INTO catalog.games(title, kind, parent_id) VALUES ($1, 'dlc', $2) RETURNING id`, title, parentID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGameHierarchyKeepsASingleLevel(t *testing.T) {
	fixture := newPostgresFixture(t)
	applyMigrations(t, fixture.config, nil)
	parentID := insertGame(t, fixture.db, "Parent")
	otherParentID := insertGame(t, fixture.db, "Other parent")
	childID := insertChild(t, fixture.db, "Child", parentID)
	for _, tc := range []struct {
		name, code, query string
		args              []any
	}{
		{"kind is required", "23502", `INSERT INTO catalog.games(title) VALUES ('No kind')`, nil},
		{"unknown kind", "23514", `INSERT INTO catalog.games(title, kind) VALUES ('Sequel', 'sequel')`, nil},
		{"unknown parent", "23503", `INSERT INTO catalog.games(title, kind, parent_id) VALUES ('Orphan', 'dlc', '10000000-0000-4000-8000-000000000099')`, nil},
		{"self reference", "23514", `UPDATE catalog.games SET parent_id = id WHERE id = $1`, []any{parentID}},
		{"child of a child", "23514", `INSERT INTO catalog.games(title, kind, parent_id) VALUES ('Grandchild', 'dlc', $1)`, []any{childID}},
		{"parent with children gets a parent", "23514", `UPDATE catalog.games SET parent_id = $2 WHERE id = $1`, []any{parentID, otherParentID}},
		{"cycle", "23514", `UPDATE catalog.games SET parent_id = CASE id WHEN $1 THEN $2 ELSE $1 END WHERE id IN ($1, $2)`, []any{otherParentID, parentID}},
		{"parent is not hard deleted", "P0001", `DELETE FROM catalog.games WHERE id = $1`, []any{parentID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertPostgresError(t, fixture.db, tc.code, tc.query, tc.args...)
		})
	}
	t.Run("a transaction may re-parent in any order", func(t *testing.T) {
		tx, err := fixture.db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for _, query := range []string{
			`UPDATE catalog.games SET parent_id = $2 WHERE id = $1`,
			`UPDATE catalog.games SET parent_id = $2 WHERE parent_id = $1 AND id <> $2`,
		} {
			if _, err := tx.ExecContext(t.Context(), query, parentID, otherParentID); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("intermediate states must be allowed until commit: %v", err)
		}
		assertSQLTrue(t, fixture.db, `SELECT count(*) = 2 FROM catalog.games WHERE parent_id = $1`, otherParentID)
	})
}

func TestConcurrentHierarchyChangesCannotNestGames(t *testing.T) {
	fixture := newPostgresFixture(t)
	applyMigrations(t, fixture.config, nil)
	a, b, c := insertGame(t, fixture.db, "A"), insertGame(t, fixture.db, "B"), insertGame(t, fixture.db, "C")
	first, err := fixture.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback()
	second, err := fixture.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback()
	// Each change is valid on its own, but together A would sit two levels deep.
	if _, err := first.ExecContext(t.Context(), `UPDATE catalog.games SET parent_id = $2 WHERE id = $1`, a, b); err != nil {
		t.Fatal(err)
	}
	if _, err := second.ExecContext(t.Context(), `UPDATE catalog.games SET parent_id = $2 WHERE id = $1`, b, c); err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.Commit() }()
	secondErr := second.Commit()
	firstErr := <-firstDone
	if (firstErr == nil) == (secondErr == nil) {
		t.Fatalf("exactly one transaction must fail: first %v, second %v", firstErr, secondErr)
	}
	assertSQLTrue(t, fixture.db, `SELECT count(*) = 0 FROM catalog.games child JOIN catalog.games parent ON parent.id = child.parent_id
		WHERE parent.parent_id IS NOT NULL`)
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
		if err == nil || !strings.Contains(err.Error(), "connection must use the catalog role") {
			t.Fatalf("expected migration role rejection, got %v", err)
		}
	})
	t.Run("non catalog connection role is rejected", func(t *testing.T) {
		db, err := Open(t.Context(), fixture.foreignConfig)
		if db != nil || err == nil || !strings.Contains(err.Error(), "connection must use the catalog role") {
			t.Fatalf("expected connection role rejection, got %v", err)
		}
	})
	t.Run("connection failures redact the password", func(t *testing.T) {
		wrong := fixture.config
		wrong.password = rand.Text()
		db, err := Open(t.Context(), wrong)
		if db != nil || err == nil || strings.Contains(err.Error(), wrong.password) {
			t.Fatalf("expected redacted connection failure, got %v", err)
		}
	})
}

func TestRefreshMigrationBackfillsOnlyActiveIGDBGames(t *testing.T) {
	fixture := newPostgresFixture(t)
	v6 := uint(6)
	applyMigrations(t, fixture.config, &v6)
	active := insertGame(t, fixture.db, "Active")
	inactive := insertGame(t, fixture.db, "Inactive")
	other := insertGame(t, fixture.db, "Other")
	executeSQL(t, fixture.db, `INSERT INTO catalog.external_game_ids(source,external_id,game_id) VALUES ('igdb','1',$1),('igdb','2',$2),('steam','3',$3)`, active, inactive, other)
	executeSQL(t, fixture.db, `UPDATE catalog.games SET is_active=false WHERE id=$1`, inactive)
	applyMigrations(t, fixture.config, nil)
	assertSQLTrue(t, fixture.db, `SELECT count(*)=1 AND bool_and(game_id=$1::uuid AND state='pending' AND attempts=0 AND next_run_at>=now() AND next_run_at<=now()+interval '24 hours') FROM catalog.refresh_jobs`, active)
	applyMigrations(t, fixture.config, nil)
	assertSQLTrue(t, fixture.db, `SELECT count(*)=1 FROM catalog.refresh_jobs`)
	assertPostgresError(t, fixture.db, "23514", `UPDATE catalog.refresh_jobs SET state='running'`)
}

func TestRecoveryMigrationRequeuesOnlyTerminalTransientFailures(t *testing.T) {
	fixture := newPostgresFixture(t)
	v7 := uint(7)
	applyMigrations(t, fixture.config, &v7)
	transient := insertGame(t, fixture.db, "Transient failure")
	rejected := insertGame(t, fixture.db, "Permanent rejection")
	missing := insertGame(t, fixture.db, "Missing provider game")
	running := insertGame(t, fixture.db, "In progress")
	executeSQL(t, fixture.db, `INSERT INTO catalog.refresh_jobs(game_id,state,attempts,last_error_code,next_run_at) VALUES
		($1,'failed',5,'provider_unavailable',now()-interval '1 hour'),
		($2,'failed',1,'provider_rejected',now()-interval '1 hour'),
		($3,'failed',1,'provider_missing',now()-interval '1 hour')`, transient, rejected, missing)
	executeSQL(t, fixture.db, `INSERT INTO catalog.refresh_jobs(game_id,state,attempts,next_run_at,lease_token,lease_until)
		VALUES($1,'running',2,now(),gen_random_uuid(),now()+interval '1 minute')`, running)
	applyMigrations(t, fixture.config, nil)
	assertSQLTrue(t, fixture.db, `SELECT state='pending' AND attempts=0 AND last_error_code='provider_unavailable'
		AND next_run_at>=now()+interval '59 minutes' FROM catalog.refresh_jobs WHERE game_id=$1`, transient)
	assertSQLTrue(t, fixture.db, `SELECT count(*)=2 AND bool_and(state='failed') FROM catalog.refresh_jobs WHERE game_id IN ($1,$2)`, rejected, missing)
	assertSQLTrue(t, fixture.db, `SELECT state='running' AND attempts=2 AND lease_token IS NOT NULL AND lease_until>now() FROM catalog.refresh_jobs WHERE game_id=$1`, running)
	// Reapplying migrations must leave an already resumed retry round alone.
	executeSQL(t, fixture.db, `UPDATE catalog.refresh_jobs SET attempts=2 WHERE game_id=$1`, transient)
	applyMigrations(t, fixture.config, nil)
	assertSQLTrue(t, fixture.db, `SELECT state='pending' AND attempts=2 FROM catalog.refresh_jobs WHERE game_id=$1`, transient)
}
