package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/config"
	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func TestServe_Lifecycle(t *testing.T) {
	for _, env := range []string{"dev", "prod"} {
		t.Run(env, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			output := &notifyingWriter{written: make(chan struct{}, 2)}
			closed := false
			app := &application{runner: waitingRunner{}, close: func() error { closed = true; return nil }}
			done := make(chan error, 1)
			go func() { done <- serve(ctx, app, newLogger(output, config.Config{Env: env})) }()
			select {
			case <-output.written:
			case <-ctx.Done():
				t.Fatal("application did not start before timeout")
			}
			select {
			case err := <-done:
				t.Fatalf("application exited before cancellation: %v", err)
			default:
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("run: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("application did not stop after cancellation")
			}
			assertLifecycleLogs(t, output.String(), env)
			if !closed {
				t.Fatal("database was not closed")
			}
		})
	}
}

func TestRun_InvalidConfiguration(t *testing.T) {
	setConfigEnv(t, "dev")
	t.Setenv("TZ", "test-secret-marker")
	var output bytes.Buffer
	err := run(t.Context(), &output)
	if !errors.Is(err, domain.ErrInvalidConfig) {
		t.Fatalf("want invalid configuration, got %v", err)
	}
	if output.Len() != 0 || strings.Contains(err.Error(), "test-secret-marker") {
		t.Fatal("startup error exposed configuration contents")
	}
}

func TestNewLogger_LevelFiltering(t *testing.T) {
	for _, env := range []string{"dev", "prod"} {
		t.Run(env, func(t *testing.T) {
			var output bytes.Buffer
			logger := newLogger(&output, config.Config{Env: env, LogLevel: slog.LevelWarn})
			logger.InfoContext(t.Context(), "info-marker")
			logger.WarnContext(t.Context(), "warning-marker")
			if strings.Contains(output.String(), "info-marker") {
				t.Fatal("info message was logged at warn level")
			}
			if !strings.Contains(output.String(), "warning-marker") {
				t.Fatal("warning message was suppressed at warn level")
			}
		})
	}
}

type notifyingWriter struct {
	bytes.Buffer
	written chan struct{}
}

func (w *notifyingWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	w.written <- struct{}{}
	return n, err
}

func setConfigEnv(t *testing.T, env string) {
	t.Helper()
	t.Chdir(t.TempDir())
	for key, value := range map[string]string{
		"BOT_TOKEN": "test-secret-marker", "DB_PATH": "test.db", "TZ": "UTC", "DIGEST_TIME": "08:00",
		"REMINDER_OFFSETS": "24h,3h", "LOG_LEVEL": "info", "ENV": env, "ADMIN_TELEGRAM_IDS": "1",
	} {
		t.Setenv(key, value)
	}
}

type waitingRunner struct{}

func (waitingRunner) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func assertLifecycleLogs(t *testing.T, output, env string) {
	t.Helper()
	if strings.Contains(output, "test-secret-marker") {
		t.Fatal("logs exposed a token")
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Fatalf("want startup and shutdown logs, got %d lines", len(lines))
	}
	for i, event := range []string{"app_started", "app_stopped"} {
		if env == "dev" {
			if !strings.Contains(lines[i], "event="+event) {
				t.Errorf("missing text log event %s", event)
			}
			continue
		}
		var entry struct{ Event string }
		if err := json.Unmarshal([]byte(lines[i]), &entry); err != nil {
			t.Fatalf("invalid JSON log: %v", err)
		}
		if entry.Event != event {
			t.Errorf("JSON event = %s, want %s", entry.Event, event)
		}
	}
}
