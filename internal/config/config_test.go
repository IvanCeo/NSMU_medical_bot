package config

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func TestLoad_ValidSources(t *testing.T) {
	tests := []struct {
		name string
		file string
		env  map[string]string
		want Config
		tz   string
	}{
		{
			name: "environment only with defaults",
			env:  map[string]string{"BOT_TOKEN": "test-only-token", "DB_PATH": "./data/test.db", "ADMIN_TELEGRAM_IDS": "1"},
			want: Config{BotToken: "test-only-token", DBPath: "./data/test.db", DigestTime: "08:00",
				ReminderOffsets: []time.Duration{24 * time.Hour, 3 * time.Hour}, LogLevel: slog.LevelInfo, Env: "dev"},
			tz: "Europe/Moscow",
		},
		{
			name: "quoted file with BOM",
			file: "\uFEFF# Configuration\nexport BOT_TOKEN='file-only-token'\nDB_PATH=\"./data/test bot.db\"\n" +
				"ADMIN_TELEGRAM_IDS=1\nTZ=UTC\nDIGEST_TIME=00:00\nREMINDER_OFFSETS=30m, 10s\nLOG_LEVEL=debug\nENV=prod\n",
			want: Config{BotToken: "file-only-token", DBPath: "./data/test bot.db", DigestTime: "00:00",
				ReminderOffsets: []time.Duration{30 * time.Minute, 10 * time.Second}, LogLevel: slog.LevelDebug, Env: "prod"},
			tz: "UTC",
		},
		{
			name: "environment overrides every file setting",
			file: "BOT_TOKEN=file-token\nDB_PATH=file.db\nTZ=UTC\nDIGEST_TIME=01:00\n" +
				"REMINDER_OFFSETS=1h\nLOG_LEVEL=debug\nENV=dev\n",
			env: map[string]string{"BOT_TOKEN": " env-token ", "DB_PATH": "./override.db", "TZ": "Europe/Moscow",
				"DIGEST_TIME": "23:59", "REMINDER_OFFSETS": " 2h, 5m ", "LOG_LEVEL": "warn", "ENV": "prod", "ADMIN_TELEGRAM_IDS": "1"},
			want: Config{BotToken: "env-token", DBPath: "./override.db", DigestTime: "23:59",
				ReminderOffsets: []time.Duration{2 * time.Hour, 5 * time.Minute}, LogLevel: slog.LevelWarn, Env: "prod"},
			tz: "Europe/Moscow",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if tt.file != "" {
				writeEnv(t, path, tt.file)
			}
			cfg, err := load(t.Context(), path, mapLookup(tt.env))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			assertConfig(t, cfg, tt.want, tt.tz)
		})
	}
}

func TestLoad_InvalidValues(t *testing.T) {
	tests := []struct{ name, key, value string }{
		{"empty token", "BOT_TOKEN", ""},
		{"blank token", "BOT_TOKEN", " \t "},
		{"empty database path", "DB_PATH", ""},
		{"blank database path", "DB_PATH", "  "},
		{"empty timezone", "TZ", ""},
		{"unknown timezone", "TZ", "invalid-test-secret-zone"},
		{"machine timezone", "TZ", "Local"},
		{"empty digest time", "DIGEST_TIME", ""},
		{"short digest hour", "DIGEST_TIME", "8:00"},
		{"out of range hour", "DIGEST_TIME", "24:00"},
		{"out of range minute", "DIGEST_TIME", "08:60"},
		{"digest with seconds", "DIGEST_TIME", "08:00:00"},
		{"empty offsets", "REMINDER_OFFSETS", ""},
		{"zero offset", "REMINDER_OFFSETS", "0s"},
		{"negative offset", "REMINDER_OFFSETS", "24h,-3h"},
		{"invalid duration", "REMINDER_OFFSETS", "1d"},
		{"overflow duration", "REMINDER_OFFSETS", "999999999999999999999h"},
		{"trailing offset separator", "REMINDER_OFFSETS", "24h,"},
		{"empty log level", "LOG_LEVEL", ""},
		{"unsupported log level", "LOG_LEVEL", "trace"},
		{"numeric log level", "LOG_LEVEL", "INFO+2"},
		{"empty environment", "ENV", ""},
		{"unsupported environment", "ENV", "staging"},
		{"empty admins", "ADMIN_TELEGRAM_IDS", ""},
		{"zero admin", "ADMIN_TELEGRAM_IDS", "0"},
		{"negative admin", "ADMIN_TELEGRAM_IDS", "-1"},
		{"duplicate admin", "ADMIN_TELEGRAM_IDS", "1,1"},
		{"username admin", "ADMIN_TELEGRAM_IDS", "@example"},
		{"overflow admin", "ADMIN_TELEGRAM_IDS", "9223372036854775808"},
		{"trailing admin separator", "ADMIN_TELEGRAM_IDS", "1,"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{"BOT_TOKEN": "test-secret-marker", "DB_PATH": "test.db", "ADMIN_TELEGRAM_IDS": "1", tt.key: tt.value}
			cfg, err := load(t.Context(), filepath.Join(t.TempDir(), ".env"), mapLookup(env))
			if !errors.Is(err, domain.ErrInvalidConfig) || !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("want invalid configuration naming %s, got %v", tt.key, err)
			}
			if cfg.BotToken != "" || strings.Contains(err.Error(), "test-secret") {
				t.Fatal("invalid configuration exposed a secret")
			}
		})
	}
}

func TestLoad_EmptyEnvironmentOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	writeEnv(t, path, "BOT_TOKEN=file-token\nDB_PATH=file.db\n")
	_, err := load(t.Context(), path, mapLookup(map[string]string{"BOT_TOKEN": ""}))
	if !errors.Is(err, domain.ErrInvalidConfig) || !strings.Contains(err.Error(), "BOT_TOKEN") {
		t.Fatalf("empty environment value should override file: %v", err)
	}
}

func TestLoad_MissingRequiredValues(t *testing.T) {
	for _, key := range []string{"BOT_TOKEN", "DB_PATH", "ADMIN_TELEGRAM_IDS"} {
		t.Run(key, func(t *testing.T) {
			env := map[string]string{"BOT_TOKEN": "test-only-token", "DB_PATH": "test.db", "ADMIN_TELEGRAM_IDS": "1"}
			delete(env, key)
			_, err := load(t.Context(), filepath.Join(t.TempDir(), ".env"), mapLookup(env))
			if !errors.Is(err, domain.ErrInvalidConfig) || !strings.Contains(err.Error(), key) {
				t.Fatalf("want missing %s, got %v", key, err)
			}
		})
	}
}

func TestLoad_MalformedFileHidesContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	writeEnv(t, path, "BOT_TOKEN=\"test-secret-marker\n")
	_, err := load(t.Context(), path, mapLookup(nil))
	if !errors.Is(err, domain.ErrInvalidConfig) {
		t.Fatalf("want invalid dotenv syntax, got %v", err)
	}
	if strings.Contains(err.Error(), "test-secret-marker") {
		t.Fatal("dotenv syntax error exposed a secret")
	}
}

func TestLoad_ExampleConfiguration(t *testing.T) {
	_, err := load(t.Context(), filepath.Join("..", "..", ".env.example"),
		mapLookup(map[string]string{"BOT_TOKEN": "test-only-token", "ADMIN_TELEGRAM_IDS": "1"}))
	if err != nil {
		t.Fatalf("example configuration with a supplied token must be valid: %v", err)
	}
}

func TestLoad_FileReadError(t *testing.T) {
	_, err := load(t.Context(), t.TempDir(), mapLookup(nil))
	var pathError *os.PathError
	if !errors.As(err, &pathError) {
		t.Fatalf("want wrapped file read error, got %v", err)
	}
}

func TestLoad_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := load(ctx, filepath.Join(t.TempDir(), ".env"), mapLookup(nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want wrapped context cancellation, got %v", err)
	}
}

func TestLoad_ProcessEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	for key, value := range map[string]string{
		"BOT_TOKEN": "environment-test-token", "DB_PATH": "env.db", "TZ": "UTC", "DIGEST_TIME": "08:00",
		"REMINDER_OFFSETS": "24h,3h", "LOG_LEVEL": "error", "ENV": "dev", "ADMIN_TELEGRAM_IDS": "1",
	} {
		t.Setenv(key, value)
	}
	if err := os.Unsetenv("TZ"); err != nil {
		t.Fatalf("unset test timezone: %v", err)
	}
	writeEnv(t, ".env", "BOT_TOKEN=file-test-token\nDB_PATH=file.db\nTZ=Europe/Moscow\n")
	cfg, err := Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BotToken != "environment-test-token" || cfg.DBPath != "env.db" || cfg.LogLevel != slog.LevelError {
		t.Fatal("process environment did not override file values")
	}
	if cfg.Timezone.String() != "Europe/Moscow" {
		t.Fatal("file timezone was not loaded")
	}
	if _, ok := os.LookupEnv("TZ"); ok {
		t.Fatal("Load mutated the process environment")
	}
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func writeEnv(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test dotenv file: %v", err)
	}
}

func assertConfig(t *testing.T, got, want Config, timezone string) {
	t.Helper()
	if got.BotToken != want.BotToken {
		t.Error("unexpected token source")
	}
	if got.DBPath != want.DBPath || got.DigestTime != want.DigestTime || got.Env != want.Env || got.LogLevel != want.LogLevel {
		t.Error("unexpected parsed configuration values")
	}
	if got.Timezone.String() != timezone {
		t.Errorf("timezone = %s, want %s", got.Timezone, timezone)
	}
	if !slices.Equal(got.ReminderOffsets, want.ReminderOffsets) {
		t.Errorf("offsets = %v, want %v", got.ReminderOffsets, want.ReminderOffsets)
	}
	if !slices.Equal(got.AdminTelegramIDs, []int64{1}) {
		t.Error("unexpected configured admin IDs")
	}
}

func TestLoad_AdminList(t *testing.T) {
	env := map[string]string{"BOT_TOKEN": "test-only-token", "DB_PATH": "test.db", "ADMIN_TELEGRAM_IDS": " 1, 9000000000 "}
	cfg, err := load(t.Context(), filepath.Join(t.TempDir(), ".env"), mapLookup(env))
	if err != nil || !slices.Equal(cfg.AdminTelegramIDs, []int64{1, 9000000000}) {
		t.Fatalf("large Telegram IDs should be supported: %v", err)
	}
}

func TestLoadDatabase_IndependentConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("BOT_TOKEN", "")
	t.Setenv("ADMIN_TELEGRAM_IDS", "")
	t.Setenv("DB_PATH", "override.db")
	writeEnv(t, ".env", "DB_PATH=file.db\nTZ=invalid-zone\n")
	path, err := LoadDatabase(t.Context())
	if err != nil || path != "override.db" {
		t.Fatalf("database loading should only validate DB_PATH: path=%s err=%v", path, err)
	}
	t.Setenv("DB_PATH", "")
	if _, err := LoadDatabase(t.Context()); !errors.Is(err, domain.ErrInvalidConfig) {
		t.Fatalf("empty database path should fail: %v", err)
	}
}
