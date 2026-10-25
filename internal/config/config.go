package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/joho/godotenv"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

type Config struct {
	BotToken         string
	AdminTelegramIDs []int64
	DBPath           string
	Timezone         *time.Location
	DigestTime       string
	ReminderOffsets  []time.Duration
	LogLevel         slog.Level
	Env              string
}

func Load(ctx context.Context) (Config, error) {
	return load(ctx, ".env", os.LookupEnv)
}

func load(ctx context.Context, path string, lookup func(string) (string, bool)) (Config, error) {
	value, err := loadValues(ctx, path, lookup)
	if err != nil {
		return Config{}, err
	}
	return parse(value)
}

func LoadDatabase(ctx context.Context) (string, error) {
	value, err := loadValues(ctx, ".env", os.LookupEnv)
	if err != nil {
		return "", err
	}
	path := value("DB_PATH", "")
	if path == "" {
		return "", invalid("DB_PATH", "must not be empty")
	}
	return path, nil
}

func loadValues(ctx context.Context, path string, lookup func(string) (string, bool)) (func(string, string) string, error) {
	values, err := readDotEnv(ctx, path)
	if err != nil {
		return nil, err
	}
	value := func(key, fallback string) string {
		if v, ok := lookup(key); ok {
			return strings.TrimSpace(v)
		}
		if v, ok := values[key]; ok {
			return strings.TrimSpace(v)
		}
		return fallback
	}
	return value, nil
}

func readDotEnv(ctx context.Context, path string) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read .env: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}
	values, err := godotenv.Unmarshal(strings.TrimPrefix(string(data), "\uFEFF"))
	if err != nil {
		// Dotenv parser errors can include the contents of a secret-bearing line.
		return nil, invalid(".env", "must contain valid dotenv syntax")
	}
	return values, nil
}

func parse(value func(string, string) string) (Config, error) {
	cfg := Config{
		BotToken:   value("BOT_TOKEN", ""),
		DBPath:     value("DB_PATH", ""),
		DigestTime: value("DIGEST_TIME", "08:00"),
		Env:        value("ENV", "dev"),
	}
	if cfg.BotToken == "" {
		return Config{}, invalid("BOT_TOKEN", "must not be empty")
	}
	if cfg.DBPath == "" {
		return Config{}, invalid("DB_PATH", "must not be empty")
	}
	if cfg.Env != "dev" && cfg.Env != "prod" {
		return Config{}, invalid("ENV", "must be dev or prod")
	}
	var err error
	cfg.AdminTelegramIDs, err = parseAdminIDs(value("ADMIN_TELEGRAM_IDS", ""))
	if err != nil {
		return Config{}, err
	}
	cfg.Timezone, err = parseTimezone(value("TZ", "Europe/Moscow"))
	if err != nil {
		return Config{}, err
	}
	digestTime, err := time.Parse("15:04", cfg.DigestTime)
	if err != nil || digestTime.Format("15:04") != cfg.DigestTime {
		return Config{}, invalid("DIGEST_TIME", "must use HH:MM in the range 00:00..23:59")
	}
	cfg.ReminderOffsets, err = parseOffsets(value("REMINDER_OFFSETS", "24h,3h"))
	if err != nil {
		return Config{}, err
	}
	cfg.LogLevel, err = parseLogLevel(value("LOG_LEVEL", "info"))
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func parseTimezone(value string) (*time.Location, error) {
	if value == "" || value == "Local" {
		return nil, invalid("TZ", "must be an IANA timezone or UTC")
	}
	location, err := time.LoadLocation(value)
	if err != nil {
		return nil, invalid("TZ", "must be an IANA timezone or UTC")
	}
	return location, nil
}

func parseOffsets(value string) ([]time.Duration, error) {
	parts := strings.Split(value, ",")
	offsets := make([]time.Duration, 0, len(parts))
	for _, part := range parts {
		offset, err := time.ParseDuration(strings.TrimSpace(part))
		if err != nil || offset <= 0 {
			return nil, invalid("REMINDER_OFFSETS", "must contain comma-separated positive Go durations")
		}
		offsets = append(offsets, offset)
	}
	return offsets, nil
}

func parseLogLevel(value string) (slog.Level, error) {
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, invalid("LOG_LEVEL", "must be debug, info, warn or error")
	}
}

func parseAdminIDs(value string) ([]int64, error) {
	var ids []int64
	seen := make(map[int64]bool)
	for part := range strings.SplitSeq(value, ",") {
		part = strings.TrimSpace(part)
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 || strings.HasPrefix(part, "+") || seen[id] {
			return nil, invalid("ADMIN_TELEGRAM_IDS", "must contain unique positive Telegram IDs separated by commas")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

func invalid(key, reason string) error {
	return fmt.Errorf("%s %s: %w", key, reason, domain.ErrInvalidConfig)
}
