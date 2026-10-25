package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func TestOpen_PragmasOnNewConnections(t *testing.T) {
	db := openTestDB(t)
	db.SetMaxIdleConns(0)
	for i := 0; i < 2; i++ {
		var mode string
		if err := db.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
			t.Fatalf("WAL is not enabled: %s %v", mode, err)
		}
		for pragma, want := range map[string]int{"PRAGMA foreign_keys": 1, "PRAGMA busy_timeout": 5000} {
			var value int
			if err := db.QueryRowContext(t.Context(), pragma).Scan(&value); err != nil || value != want {
				t.Fatalf("pragma %s = %d, want %d: %v", pragma, value, want, err)
			}
		}
	}
	if db.Stats().MaxOpenConnections != 1 {
		t.Fatal("pool is not limited to one connection")
	}
}

func TestMigrate_UpDownUp(t *testing.T) {
	db := openTestDB(t)
	for i := 0; i < 2; i++ {
		if err := Migrate(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	provider, err := migrationProvider(db)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := provider.Down(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('users', 'groups')").Scan(&count); err != nil || count != 0 {
		t.Fatalf("down did not remove identity tables: count=%d err=%v", count, err)
	}
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := NewUsers(db).UpsertTelegramUser(t.Context(), domain.TelegramUser{TelegramID: 1}, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestOpen_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Open(ctx, filepath.Join(t.TempDir(), "test.db")); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation, got %v", err)
	}
}

func TestUsers_PersistenceAndConstraints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "test #1.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	users := NewUsers(db)
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.FixedZone("test", 3*3600))
	if err := users.UpsertTelegramUser(t.Context(), domain.TelegramUser{TelegramID: 1, DisplayName: "Test ' User"}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "INSERT INTO groups (id, title, invite_code, created_at) VALUES (1, 'Test group', 'test-code', ?)", at.UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "UPDATE users SET group_id=1, role='editor' WHERE telegram_id=1"); err != nil {
		t.Fatal(err)
	}
	if err := users.UpsertTelegramUser(t.Context(), domain.TelegramUser{TelegramID: 1, DisplayName: "Updated"}, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	var name, role, created string
	var group, count int
	if err := db.QueryRowContext(t.Context(), "SELECT display_name, role, group_id, created_at FROM users WHERE telegram_id=1").Scan(&name, &role, &group, &created); err != nil {
		t.Fatal(err)
	}
	if name != "Updated" || role != "editor" || group != 1 || created != "2026-10-25T09:00:00Z" {
		t.Fatal("upsert did not preserve identity fields or UTC timestamp")
	}
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM users").Scan(&count); err != nil || count != 1 {
		t.Fatal("upsert created a duplicate")
	}
	for _, query := range []string{"UPDATE users SET group_id=999", "UPDATE users SET role='owner'", "DELETE FROM groups WHERE id=1", "UPDATE users SET telegram_id=0"} {
		if _, err := db.ExecContext(t.Context(), query); err == nil {
			t.Fatalf("constraint not enforced: %s", query)
		}
	}
}

func TestSyncAdmins_AtomicAndAuthoritative(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	r := NewUsers(db)
	at := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	if err := r.UpsertTelegramUser(t.Context(), domain.TelegramUser{TelegramID: 1, DisplayName: "Known admin"}, at); err != nil {
		t.Fatal(err)
	}
	if err := r.UpsertTelegramUser(t.Context(), domain.TelegramUser{TelegramID: 3, DisplayName: "Editor"}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "INSERT INTO groups (id, title, invite_code, created_at) VALUES (1, 'Test group', 'test-code', ?)", at.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "UPDATE users SET group_id=1, role='editor' WHERE telegram_id IN (1, 3)"); err != nil {
		t.Fatal(err)
	}
	if err := r.SyncAdmins(t.Context(), []int64{1, 2}, at); err != nil {
		t.Fatal(err)
	}
	if err := r.SyncAdmins(t.Context(), []int64{1, 2}, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var name, role, created string
	if err := db.QueryRowContext(t.Context(), "SELECT display_name, role, created_at FROM users WHERE telegram_id=1").Scan(&name, &role, &created); err != nil {
		t.Fatal(err)
	}
	if name != "Known admin" || role != "admin" || created != at.Format(time.RFC3339) {
		t.Fatal("admin bootstrap overwrote identity fields")
	}
	var group int
	if err := db.QueryRowContext(t.Context(), "SELECT group_id FROM users WHERE telegram_id=1").Scan(&group); err != nil || group != 1 {
		t.Fatal("admin bootstrap changed group membership")
	}
	if err := db.QueryRowContext(t.Context(), "SELECT role FROM users WHERE telegram_id=3").Scan(&role); err != nil || role != "editor" {
		t.Fatal("admin bootstrap changed an unrelated editor")
	}
	if err := r.UpsertTelegramUser(t.Context(), domain.TelegramUser{TelegramID: 2, DisplayName: "First message"}, at.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := r.SyncAdmins(t.Context(), []int64{2, -1}, at); err == nil {
		t.Fatal("invalid admin ID should fail")
	}
	if err := db.QueryRowContext(t.Context(), "SELECT role FROM users WHERE telegram_id=1").Scan(&role); err != nil || role != "admin" {
		t.Fatal("failed sync partially revoked admins")
	}
	if err := r.SyncAdmins(t.Context(), []int64{2}, at); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT role FROM users WHERE telegram_id=1").Scan(&role); err != nil || role != "student" {
		t.Fatal("removed admin retained permissions")
	}
	if err := db.QueryRowContext(t.Context(), "SELECT display_name, role, created_at FROM users WHERE telegram_id=2").Scan(&name, &role, &created); err != nil {
		t.Fatal(err)
	}
	if name != "First message" || role != "admin" || created != at.Format(time.RFC3339) {
		t.Fatal("seeded admin changed unexpectedly after first message")
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}
