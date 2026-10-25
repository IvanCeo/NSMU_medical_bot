package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	driver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

type Users struct {
	db *sql.DB
	q  queryer
}

func NewUsers(db *sql.DB) *Users {
	return &Users{db: db, q: db}
}

func (r *Users) UpsertTelegramUser(ctx context.Context, user domain.TelegramUser, createdAt time.Time) error {
	_, err := r.q.ExecContext(ctx, `
		INSERT INTO users (telegram_id, display_name, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT (telegram_id) DO UPDATE SET display_name = excluded.display_name`,
		user.TelegramID, user.DisplayName, createdAt.UTC().Format(time.RFC3339))
	if err != nil {
		return storageError("upsert Telegram user", err)
	}
	return nil
}

func (r *Users) SyncAdmins(ctx context.Context, telegramIDs []int64, createdAt time.Time) (err error) {
	if len(telegramIDs) == 0 {
		return fmt.Errorf("admin list must not be empty: %w", domain.ErrInvalidUser)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError("begin admin sync", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("rollback admin sync: %w", rollbackErr))
		}
	}()
	if _, err := tx.ExecContext(ctx, "UPDATE users SET role = 'student' WHERE role = 'admin'"); err != nil {
		return storageError("reset configured admins", err)
	}
	for _, id := range telegramIDs {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO users (telegram_id, display_name, role, created_at)
			VALUES (?, '', 'admin', ?)
			ON CONFLICT (telegram_id) DO UPDATE SET role = 'admin'`,
			id, createdAt.UTC().Format(time.RFC3339))
		if err != nil {
			return storageError("seed configured admin", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return storageError("commit admin sync", err)
	}
	return nil
}

func storageError(action string, err error) error {
	kind := domain.ErrStorage
	var sqliteError *driver.Error
	if errors.As(err, &sqliteError) && sqliteError.Code()&0xff == sqlite3.SQLITE_CONSTRAINT {
		kind = domain.ErrInvalidUser
	}
	return fmt.Errorf("%s: %w", action, errors.Join(kind, err))
}
