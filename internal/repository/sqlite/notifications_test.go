package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
	"github.com/IvanCeo/NSMU_medical_bot/internal/service"
)

func (f *fixture) drain(t *testing.T) {
	t.Helper()
	for i := 0; i < 100; i++ {
		ok, e := f.app.DispatchOne(t.Context(), f.now)
		check(t, e)
		if !ok {
			return
		}
	}
	t.Fatal("outbox did not drain")
}
func TestNotifications_CatchupCoalescingAndDeduplication(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	a := f.assignment(t, 60*time.Hour)
	check(t, f.app.BindChat(ctx, 1, f.group.ID, -100))
	f.now = a.DeadlineAt.Add(-2 * time.Hour)
	check(t, f.app.Plan(ctx, f.now))
	check(t, f.app.Plan(ctx, f.now))
	if n := f.number(t, "SELECT count(*) FROM outbox"); n != 3 {
		t.Fatalf("outbox=%d", n)
	}
	f.drain(t)
	if len(f.tg.sent) != 3 {
		t.Fatalf("sent=%d", len(f.tg.sent))
	}
	personal, group := 0, 0
	for _, m := range f.tg.sent {
		if m.Kind == "group" {
			group++
			if m.ChatID != -100 {
				t.Fatal("wrong group")
			}
		} else {
			personal++
		}
	}
	if personal != 2 || group != 1 {
		t.Fatalf("personal/group=%d/%d", personal, group)
	}
	check(t, f.app.Plan(ctx, f.now))
	f.drain(t)
	if len(f.tg.sent) != 3 {
		t.Fatal("repeated sent notifications")
	}
	if n := f.number(t, "SELECT count(*) FROM digest_runs WHERE status='sent'"); n != 1 {
		t.Fatalf("digest status=%d", n)
	}
}
func TestNotifications_TransientRetryAndPrivateBlock(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	a := f.assignment(t, 26*time.Hour)
	f.now = f.now.Add(3 * time.Hour)
	check(t, f.app.Plan(ctx, f.now))
	f.tg.failure = &domain.DeliveryFailure{Code: "429", RetryAfter: time.Minute}
	ok, e := f.app.DispatchOne(ctx, f.now)
	check(t, e)
	if !ok {
		t.Fatal("nothing claimed")
	}
	if f.number(t, "SELECT count(*) FROM outbox WHERE attempts=1 AND status='pending' AND next_attempt_at=?", stamp(f.now.Add(time.Minute))) != 1 {
		t.Fatal("retry_after not persisted")
	}
	f.tg.failure = &domain.DeliveryFailure{Code: "403", Permanent: true}
	f.drain(t)
	f.now = f.now.Add(time.Minute)
	f.tg.failure = nil
	f.drain(t)
	if len(f.tg.sent) != 1 {
		t.Fatalf("expected only unblocked participant to receive: %d", len(f.tg.sent))
	}
	f.now = a.DeadlineAt.Add(-2 * time.Hour)
	check(t, f.app.Plan(ctx, f.now))
	f.drain(t)
	if len(f.tg.sent) != 2 {
		t.Fatalf("blocked chat received later reminder: %d", len(f.tg.sent))
	}
	// A private interaction restores availability and schedules future intervals only.
	_, e = f.app.Register(ctx, domain.TelegramUser{TelegramID: 3, DisplayName: "Test"})
	check(t, e)
}
func TestNotifications_QuietCatchupAndDeadlineSkip(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	a := f.assignment(t, 36*time.Hour)
	check(t, f.app.SavePreferences(ctx, 3, domain.Preferences{RemindersEnabled: true, QuietHours: &domain.QuietHours{Start: "02:00", End: "18:00"}}))
	f.now = f.now.Add(21 * time.Hour)
	check(t, f.app.Plan(ctx, f.now))
	f.drain(t)
	if len(f.tg.sent) != 1 || f.tg.sent[0].ChatID != 2 {
		t.Fatal("quiet chat was sent during quiet hours")
	}
	if f.number(t, "SELECT count(*) FROM outbox WHERE user_id=? AND status='pending'", f.users[3].ID) != 1 {
		t.Fatal("quiet delivery not deferred")
	}
	f.now = a.DeadlineAt.Add(-90 * time.Minute)
	check(t, f.app.Plan(ctx, f.now))
	f.drain(t)
	n := 0
	for _, m := range f.tg.sent {
		if m.ChatID == 3 {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("quiet coalescing sent=%d", n)
	}
	f.now = a.DeadlineAt.Add(time.Minute)
	check(t, f.app.Plan(ctx, f.now))
	f.drain(t)
}
func TestNotifications_SnoozeAndMutationRevalidation(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	a := f.assignment(t, 27*time.Hour)
	f.now = f.now.Add(4 * time.Hour)
	check(t, f.app.Plan(ctx, f.now))
	check(t, f.app.Snooze(ctx, 3, a.ID, a.Version, 3*time.Hour))
	f.drain(t)
	if len(f.tg.sent) != 1 || f.tg.sent[0].ChatID != 2 {
		t.Fatal("snooze failed to revoke earlier pending delivery")
	}
	f.now = f.now.Add(3 * time.Hour)
	check(t, f.app.Plan(ctx, f.now))
	f.drain(t)
	if len(f.tg.sent) != 2 || f.tg.sent[1].ChatID != 3 {
		t.Fatal("snooze not delivered")
	}
	newTitle := "Changed"
	a, e := f.app.UpdateAssignment(ctx, 2, a.ID, a.Version, domain.AssignmentPatch{Title: &newTitle})
	check(t, e)
	f.now = a.DeadlineAt.Add(-2 * time.Hour)
	check(t, f.app.Plan(ctx, f.now))
	_, e = f.app.SetComplete(ctx, 3, a.ID, a.Version, true)
	check(t, e)
	check(t, f.app.LeaveGroup(ctx, 3))
	f.drain(t)
	for _, m := range f.tg.sent[2:] {
		if m.ChatID == 3 {
			t.Fatal("notification after leave/done")
		}
	}
}
func TestNotifications_RetryLimitAndStaleLease(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	f.assignment(t, 48*time.Hour)
	f.now = f.now.Add(25 * time.Hour)
	check(t, f.app.Plan(ctx, f.now))
	f.tg.failure = &domain.DeliveryFailure{Code: "503"}
	for i := 0; i < 5; i++ {
		f.drain(t)
		f.now = f.now.Add(2 * time.Minute)
	}
	if n := f.number(t, "SELECT count(*) FROM outbox WHERE status='failed' AND attempts=5"); n != 2 {
		t.Fatalf("failed rows=%d", n)
	}
	// Independently exercise lease fencing and recovery on a fresh pending record.
	f.assignment(t, 48*time.Hour)
	f.now = f.now.Add(25 * time.Hour)
	check(t, f.app.Plan(ctx, f.now))
	r := f.store.Repositories()
	d, e := r.Deliveries.Claim(ctx, f.now, f.now.Add(2*time.Minute), "first")
	check(t, e)
	if d == nil {
		t.Fatal("no lease")
	}
	f.now = f.now.Add(3 * time.Minute)
	next, e := r.Deliveries.Claim(ctx, f.now, f.now.Add(2*time.Minute), "second")
	check(t, e)
	if next == nil || next.ID != d.ID {
		t.Fatal("lease not recovered")
	}
	expect(t, r.Deliveries.Complete(ctx, d.ID, "first", domain.MessageReceipt{MessageID: 1}, f.now), domain.ErrConflict)
	check(t, r.Deliveries.Complete(ctx, next.ID, "second", domain.MessageReceipt{MessageID: 2}, f.now))
}
func TestNotifications_DigestPartsRebindingAndNextDate(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	check(t, f.app.BindChat(ctx, 1, f.group.ID, -100))
	check(t, f.app.SavePreferences(ctx, 2, domain.Preferences{}))
	check(t, f.app.SavePreferences(ctx, 3, domain.Preferences{}))
	for i := 0; i < 18; i++ {
		f.assignment(t, 10*time.Hour)
	}
	check(t, f.app.Plan(ctx, f.now))
	if n := f.number(t, "SELECT count(*) FROM outbox WHERE kind='digest'"); n != 3 {
		t.Fatalf("parts=%d", n)
	}
	ok, e := f.app.DispatchOne(ctx, f.now)
	check(t, e)
	if !ok || len(f.tg.sent) != 1 {
		t.Fatal("first part not delivered")
	}
	check(t, f.app.BindChat(ctx, 1, f.group.ID, -200))
	f.drain(t)
	check(t, f.app.Plan(ctx, f.now))
	f.drain(t)
	if len(f.tg.sent) != 1 {
		t.Fatal("rebind duplicated today's digest")
	}
	f.now = f.now.AddDate(0, 0, 1)
	check(t, f.app.Plan(ctx, f.now))
	f.drain(t)
	if f.number(t, "SELECT count(*) FROM digest_runs WHERE status='empty'") != 1 {
		t.Fatal("empty digest not recorded")
	}
}
func TestNotifications_DigestRevalidatesCancelledAssignment(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	check(t, f.app.BindChat(ctx, 1, f.group.ID, -100))
	a := f.assignment(t, 10*time.Hour)
	b := f.assignment(t, 11*time.Hour)
	check(t, f.app.Plan(ctx, f.now))
	check(t, f.app.CancelAssignment(ctx, 2, a.ID, a.Version))
	f.drain(t)
	if len(f.tg.sent) != 1 || len(f.tg.sent[0].Buttons) != 1 || f.tg.sent[0].Buttons[0].AssignmentID != b.ID {
		t.Fatal("digest retained cancelled task or was cancelled wholesale")
	}
}
func TestMigrations_ExistingIdentityDataPreserved(t *testing.T) {
	db := openTestDB(t)
	provider, e := migrationProvider(db)
	check(t, e)
	_, e = provider.UpTo(t.Context(), 1)
	check(t, e)
	_, e = db.ExecContext(t.Context(), "INSERT INTO groups(id,title,invite_code,created_at) VALUES(1,'Test','test-invite','2026-01-01T00:00:00Z')")
	check(t, e)
	_, e = db.ExecContext(t.Context(), "INSERT INTO users(id,telegram_id,display_name,group_id,role,created_at) VALUES(1,1,'Test',1,'editor','2026-01-01T00:00:00Z')")
	check(t, e)
	check(t, Migrate(t.Context(), db))
	u, e := NewUsers(db).ByID(t.Context(), 1)
	check(t, e)
	if u.Role != domain.Editor || u.GroupID == nil || u.CreatedAt.Format(time.RFC3339) != "2026-01-01T00:00:00Z" {
		t.Fatal("identity data changed")
	}
}

type failingWork struct{}

func (failingWork) WithinTx(context.Context, func(service.Repositories) error) error {
	return domain.ErrStorage
}
func TestNotifications_TransactionFailure(t *testing.T) {
	s := service.New(failingWork{}, nil, nil, service.Options{})
	expect(t, s.Plan(t.Context(), time.Now()), domain.ErrStorage)
	_, e := s.DispatchOne(t.Context(), time.Now())
	expect(t, e, domain.ErrStorage)
	if !errors.Is(e, domain.ErrStorage) {
		t.Fatal(fmt.Sprint(e))
	}
}

func TestNotifications_FractionalConfiguredOffset(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	f.app = service.New(f.store, f.tg, f.tg, service.Options{Now: func() time.Time { return f.now }, ReminderOffsets: []time.Duration{100 * time.Millisecond}})
	a := f.assignment(t, time.Hour)
	f.now = a.DeadlineAt.Add(-50 * time.Millisecond)
	due, e := f.store.Repositories().Reminders.Due(ctx, f.now, 10)
	check(t, e)
	if len(due) != 2 || due[0].Offset == nil || *due[0].Offset != 100*time.Millisecond {
		t.Fatal("fractional offset lost")
	}
}
