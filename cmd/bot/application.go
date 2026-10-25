package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/bot"
	"github.com/IvanCeo/NSMU_medical_bot/internal/config"
	"github.com/IvanCeo/NSMU_medical_bot/internal/repository/sqlite"
	"github.com/IvanCeo/NSMU_medical_bot/internal/scheduler"
	"github.com/IvanCeo/NSMU_medical_bot/internal/service"
)

type application struct {
	runner interface{ Run(context.Context) error }
	close  func() error
}

func newApplication(ctx context.Context, cfg config.Config, logger *slog.Logger) (_ *application, err error) {
	db, err := sqlite.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, db.Close())
		}
	}()
	if err := sqlite.Migrate(ctx, db); err != nil {
		return nil, err
	}
	users := sqlite.NewUsers(db)
	if err := service.BootstrapAdmins(ctx, users, cfg.AdminTelegramIDs, time.Now()); err != nil {
		return nil, err
	}
	client, err := bot.NewTelegram(ctx, cfg.BotToken)
	if err != nil {
		return nil, fmt.Errorf("initialize Telegram: %w", err)
	}
	cases := service.New(sqlite.NewStore(db), client, client, service.Options{Now: time.Now, Timezone: cfg.Timezone, ReminderOffsets: cfg.ReminderOffsets, DigestTime: cfg.DigestTime})
	runner := bot.NewMVP(client, cases, cfg.Timezone, logger)
	jobs := scheduler.New(cases, time.Now, logger)
	return &application{runner: combinedRunner{runner, jobs}, close: db.Close}, nil
}

type combinedRunner struct {
	bot, jobs interface{ Run(context.Context) error }
}

func (r combinedRunner) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.jobs.Run(ctx) }()
	err := r.bot.Run(ctx)
	cancel()
	return errors.Join(err, <-done)
}
