package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies only forward migrations inside the catalog-owned schema.
func Migrate(ctx context.Context, config Config, target *uint) error {
	if err := migrateDatabase(ctx, config, target); err != nil {
		return redact(err, config)
	}
	return nil
}

// Open returns a connection pool that is verified to use the catalog role.
func Open(ctx context.Context, config Config) (*sql.DB, error) {
	db, err := open(ctx, config)
	if err != nil {
		return nil, redact(err, config)
	}
	return db, nil
}

func open(ctx context.Context, config Config) (*sql.DB, error) {
	db, err := sql.Open("postgres", config.connectionString())
	if err != nil {
		return nil, err
	}
	var role string
	if err := db.QueryRowContext(ctx, "SELECT current_user").Scan(&role); err != nil {
		db.Close()
		return nil, err
	}
	if role != "catalog" {
		db.Close()
		return nil, errors.New("connection must use the catalog role")
	}
	return db, nil
}

func redact(err error, config Config) error {
	return errors.New(strings.ReplaceAll(err.Error(), config.password, "[REDACTED]"))
}

func migrateDatabase(ctx context.Context, config Config, target *uint) error {
	db, err := open(ctx, config)
	if err != nil {
		return err
	}
	defer db.Close()
	source, err := iofs.New(migrations, "migrations")
	if err != nil {
		return err
	}
	driver, err := postgres.WithInstance(db, &postgres.Config{SchemaName: "catalog", MigrationsTable: "schema_migrations"})
	if err != nil {
		return err
	}
	runner, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return err
	}
	defer runner.Close()
	if target != nil {
		current, _, versionErr := runner.Version()
		if versionErr != nil && !errors.Is(versionErr, migrate.ErrNilVersion) {
			return versionErr
		}
		if current > *target {
			return fmt.Errorf("refusing downgrade from version %d", current)
		}
		err = runner.Migrate(*target)
	} else {
		err = runner.Up()
	}
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return err
}
