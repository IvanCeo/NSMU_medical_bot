package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/IvanCeo/NSMU_medical_bot/internal/config"
	"github.com/IvanCeo/NSMU_medical_bot/internal/repository/sqlite"
)

func main() {
	os.Exit(exitCode())
}

func exitCode() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("Database migration failed", "event", "migration_failed", "err", err)
		return 1
	}
	slog.Info("Database migrations applied", "event", "migration_completed")
	return 0
}

func run(ctx context.Context) error {
	path, err := config.LoadDatabase(ctx)
	if err != nil {
		return err
	}
	db, err := sqlite.Open(ctx, path)
	if err != nil {
		return err
	}
	return errors.Join(sqlite.Migrate(ctx, db), db.Close())
}
