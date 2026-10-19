package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/database"
)

func main() {
	config, err := database.Load(os.Getenv)
	if err != nil {
		slog.Error("invalid database configuration", "error", err)
		os.Exit(1)
	}
	var target *uint
	if len(os.Args) > 2 {
		slog.Error("usage: migrate [targetVersion]")
		os.Exit(1)
	}
	if len(os.Args) == 2 {
		value, err := strconv.ParseUint(os.Args[1], 10, 32)
		if err != nil || value == 0 {
			slog.Error("migration target must be a positive version")
			os.Exit(1)
		}
		version := uint(value)
		target = &version
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, config, target); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("catalog migrations complete")
}
