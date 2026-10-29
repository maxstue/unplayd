package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/maxstue/unplayd/apps/catalog-api/internal/auth"
	"github.com/maxstue/unplayd/apps/catalog-api/internal/catalog"
	"github.com/maxstue/unplayd/apps/catalog-api/internal/database"
	"github.com/maxstue/unplayd/apps/catalog-api/internal/httpapi"
	"github.com/maxstue/unplayd/apps/catalog-api/internal/igdb"
	"github.com/maxstue/unplayd/apps/catalog-api/internal/requestid"
)

func main() {
	slog.SetDefault(slog.New(requestid.LogHandler{Handler: slog.NewJSONHandler(os.Stdout, nil)}))
	authConfig, err := auth.Load(os.Getenv)
	if err != nil {
		slog.Error("invalid authentication configuration", "error", err)
		os.Exit(1)
	}
	databaseConfig, err := database.Load(os.Getenv)
	if err != nil {
		slog.Error("invalid database configuration", "error", err)
		os.Exit(1)
	}
	igdbConfig, err := igdb.Load(os.Getenv)
	if err != nil {
		slog.Error("invalid IGDB configuration", "error", err)
		os.Exit(1)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8446"
	}
	if value, err := strconv.Atoi(port); err != nil || value < 1 || value > 65535 {
		slog.Error("PORT must be between 1 and 65535")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := database.Open(ctx, databaseConfig)
	if err != nil {
		slog.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	db.SetConnMaxIdleTime(5 * time.Minute)
	store := catalog.NewStore(db)
	var importer *catalog.Importer
	if igdbConfig.Enabled() {
		client, err := igdb.NewClient(igdbConfig)
		if err != nil {
			slog.Error("invalid IGDB configuration", "error", err)
			os.Exit(1)
		}
		importer = catalog.NewImporter(client, store)
	}
	games := catalog.NewService(store, importer, catalog.DefaultSearchPolicy(), slog.Default())
	server := &http.Server{
		Addr: ":" + port, Handler: httpapi.NewHandler(authConfig.Token(), games, slog.Default()),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.Error("shutdown failed", "error", err)
		}
	}()
	slog.Info("catalog-api listening", "port", port, "igdbEnabled", igdbConfig.Enabled())
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
	<-shutdownDone
	games.Wait()
}
