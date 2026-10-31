package sqlite

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/backup"
	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func TestBackup_ClosedDatabaseRestoresWithSchemaAndData(t *testing.T) {
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		t.Skip("container restore assigns UID 65532 and requires root on Unix")
	}
	ctx := t.Context()
	source, archives, restored := t.TempDir(), t.TempDir(), t.TempDir()
	db, e := Open(ctx, filepath.Join(source, "bot.db"))
	check(t, e)
	check(t, Migrate(ctx, db))
	check(t, NewUsers(db).UpsertTelegramUser(ctx, domain.TelegramUser{TelegramID: 42, DisplayName: "Test student"}, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)))
	check(t, db.Close())
	archive, e := backup.Create(ctx, source, archives)
	check(t, e)
	check(t, backup.Verify(ctx, archive))
	check(t, backup.Restore(ctx, archive, restored))
	restoredDB, e := Open(ctx, filepath.Join(restored, "bot.db"))
	check(t, e)
	t.Cleanup(func() { check(t, restoredDB.Close()) })
	var integrity string
	check(t, restoredDB.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity))
	if integrity != "ok" {
		t.Fatalf("restored integrity: %s", integrity)
	}
	u, e := NewUsers(restoredDB).ByTelegramID(ctx, 42)
	check(t, e)
	if u.DisplayName != "Test student" || u.TelegramID != 42 {
		t.Fatal("restored user changed")
	}
	check(t, Migrate(ctx, restoredDB))
}
