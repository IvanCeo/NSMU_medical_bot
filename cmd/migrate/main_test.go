package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRun_MigrateWithoutTelegram(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("BOT_TOKEN", "")
	t.Setenv("ADMIN_TELEGRAM_IDS", "")
	path := filepath.Join(dir, "data", "test.db")
	t.Setenv("DB_PATH", path)
	for i := 0; i < 2; i++ {
		if err := run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
