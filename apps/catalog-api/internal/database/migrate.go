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
	err := migrateDatabase(ctx, config, target)
	if err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), config.password, "[REDACTED]"))
	}
	return nil
}

func migrateDatabase(ctx context.Context, config Config, target *uint) error {
	db, err := sql.Open("postgres", config.connectionString())
	if err != nil {
		return err
	}
	defer db.Close()
	var role string
	if err := db.QueryRowContext(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "catalog" {
		return errors.New("migration connection must use the catalog role")
	}
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
