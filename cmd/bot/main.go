package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/IvanCeo/NSMU_medical_bot/internal/config"
)

func main() {
	os.Exit(mainExitCode())
}

func mainExitCode() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Stderr); err != nil {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		logger.Error("Application startup failed", "event", "startup_failed", "err", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, output io.Writer) error {
	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}
	logger := newLogger(output, cfg)
	app, err := newApplication(ctx, cfg, logger)
	if err != nil {
		return err
	}
	return serve(ctx, app, logger)
}

func serve(ctx context.Context, app *application, logger *slog.Logger) error {
	logger.InfoContext(ctx, "Application initialized", "event", "app_started")
	err := app.runner.Run(ctx)
	err = errors.Join(err, app.close())
	logger.InfoContext(ctx, "Application stopped", "event", "app_stopped")
	return err
}

func newLogger(output io.Writer, cfg config.Config) *slog.Logger {
	options := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.Env == "prod" {
		return slog.New(slog.NewJSONHandler(output, options))
	}
	return slog.New(slog.NewTextHandler(output, options))
}
