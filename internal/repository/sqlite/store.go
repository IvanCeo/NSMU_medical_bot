package sqlite

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
	"github.com/IvanCeo/NSMU_medical_bot/internal/service"
	driver "modernc.org/sqlite"
)

type queryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type scanner interface{ Scan(...any) error }

func closeRows(rows *sql.Rows, err *error) { *err = errors.Join(*err, repoError(rows.Close())) }
func readAssignments(rows *sql.Rows) (out []domain.AssignmentView, err error) {
	defer closeRows(rows, &err)
	for rows.Next() {
		a, e := scanAssignment(rows)
		if e != nil {
			return out, e
		}
		out = append(out, domain.AssignmentView{Assignment: a})
	}
	return out, repoError(rows.Err())
}

type store struct{ q queryer }
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store                    { return &Store{db} }
func (s *Store) Repositories() service.Repositories { return repositories(s.db) }
func repositories(q queryer) service.Repositories {
	s := store{q}
	return service.Repositories{
		Users: &Users{q: q}, Groups: groups{s}, Subjects: subjects{s}, Assignments: assignments{s},
		Completions: completions{s}, Reminders: reminders{s}, Settings: settings{s}, Dialogs: dialogs{s},
		Digests: digests{s}, Deliveries: deliveries{s}, Audit: audit{s},
	}
}
func (s *Store) WithinTx(ctx context.Context, fn func(service.Repositories) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return repoError(err)
	}
	defer func() {
		if e := tx.Rollback(); e != nil && !errors.Is(e, sql.ErrTxDone) {
			err = errors.Join(err, repoError(e))
		}
	}()
	if err = fn(repositories(tx)); err != nil {
		return err
	}
	return repoError(tx.Commit())
}
func repoError(err error) error {
	if err == nil {
		return nil
	}
	kind := domain.ErrStorage
	if errors.Is(err, sql.ErrNoRows) {
		kind = domain.ErrNotFound
	}
	var e *driver.Error
	if errors.As(err, &e) && e.Code()&255 == 19 {
		kind = domain.ErrConflict
	}
	return fmt.Errorf("repository: %w", errors.Join(kind, err))
}
func exec(ctx context.Context, q queryer, query string, args ...any) error {
	_, err := q.ExecContext(ctx, query, args...)
	return repoError(err)
}
func cas(ctx context.Context, q queryer, query string, args ...any) error {
	r, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return repoError(err)
	}
	n, err := r.RowsAffected()
	if err != nil {
		return repoError(err)
	}
	if n != 1 {
		return domain.ErrConflict
	}
	return nil
}
func stamp(t time.Time) string { return t.UTC().Truncate(time.Second).Format(time.RFC3339) }
func parse(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }
func nullableTime(s sql.NullString) *time.Time {
	if !s.Valid {
		return nil
	}
	t := parse(s.String)
	return &t
}
func timeArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return stamp(*t)
}
func textArg(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func page(p domain.PageRequest) (int, int64, error) {
	n := p.Limit
	if n == 0 {
		n = 10
	}
	if n < 1 || n > 20 {
		return 0, 0, domain.ErrInvalidInput
	}
	if p.Cursor == "" || p.Cursor == "0" {
		return n, 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(p.Cursor)
	if err != nil || len(b) != 8 {
		return 0, 0, domain.ErrInvalidInput
	}
	id := int64(binary.BigEndian.Uint64(b))
	if id < 0 {
		return 0, 0, domain.ErrInvalidInput
	}
	return n, id, nil
}
func cursor(id int64) string {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(id))
	return base64.RawURLEncoding.EncodeToString(b)
}
func assignmentCursor(t time.Time, id int64) string {
	b := make([]byte, 16)
	binary.BigEndian.PutUint64(b, uint64(t.Unix()))
	binary.BigEndian.PutUint64(b[8:], uint64(id))
	return base64.RawURLEncoding.EncodeToString(b)
}
func assignmentPage(p domain.PageRequest) (int, string, int64, error) {
	n := p.Limit
	if n == 0 {
		n = 10
	}
	if n < 1 || n > 20 {
		return 0, "", 0, domain.ErrInvalidInput
	}
	if p.Cursor == "" || p.Cursor == "0" {
		return n, "", 0, nil
	}
	b, e := base64.RawURLEncoding.DecodeString(p.Cursor)
	if e != nil || len(b) != 16 {
		return 0, "", 0, domain.ErrInvalidInput
	}
	sec, id := int64(binary.BigEndian.Uint64(b)), int64(binary.BigEndian.Uint64(b[8:]))
	if sec < 0 || id <= 0 {
		return 0, "", 0, domain.ErrInvalidInput
	}
	return n, stamp(time.Unix(sec, 0)), id, nil
}
