package sqlite

import (
	"strings"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func TestAssignmentCreated_RecipientsAndNoBackfill(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		disabled, unavailable bool
		want                  int
	}{{"all members including author", false, false, 2}, {"disabled member", true, false, 1}, {"unavailable member", false, true, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ctx := t.Context()
			if tc.disabled {
				check(t, f.app.SavePreferences(ctx, 3, domain.Preferences{}))
			}
			if tc.unavailable {
				check(t, f.store.Repositories().Users.SetPrivateChatAvailable(ctx, f.users[3].ID, false))
			}
			a := f.assignment(t, 48*time.Hour)
			if n := f.number(t, "SELECT count(*) FROM outbox WHERE kind='assignment_created'"); n != tc.want {
				t.Fatalf("queued=%d, want %d", n, tc.want)
			}
			_, e := f.app.JoinGroup(ctx, 4, f.group.InviteCode)
			check(t, e)
			f.wire() // Pending publication events survive a new application instance.
			check(t, f.app.Plan(ctx, f.now))
			f.drain(t)
			f.wire() // Restart the application with the same durable queue.
			f.drain(t)
			if len(f.tg.sent) != tc.want {
				t.Fatalf("sent=%d, want %d", len(f.tg.sent), tc.want)
			}
			for _, m := range f.tg.sent {
				if m.ChatID == 4 || m.Kind != "private" || m.EventKind != domain.AssignmentCreatedDelivery || !strings.Contains(m.Text, "Anatomy") || m.Buttons[0].AssignmentID != a.ID || m.Buttons[0].Label != a.Title {
					t.Fatalf("unexpected message: %+v", m)
				}
			}
		})
	}
}

func TestAssignmentCreated_QuietDeadlineAtPublication(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	check(t, f.app.SavePreferences(ctx, 3, domain.Preferences{RemindersEnabled: true, QuietHours: &domain.QuietHours{Start: "22:00", End: "09:00"}}))
	f.assignment(t, time.Hour) // Quiet ends exactly at the deadline.
	if f.number(t, "SELECT count(*) FROM outbox WHERE kind='assignment_created' AND user_id=?", f.users[3].ID) != 0 {
		t.Fatal("notice was queued at or after deadline")
	}
	f.drain(t)
	if len(f.tg.sent) != 1 || f.tg.sent[0].ChatID != 2 {
		t.Fatal("quiet deadline publication had wrong recipients")
	}
}

func TestAssignmentCreated_QuietHoursAndLatestContent(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	check(t, f.app.SavePreferences(ctx, 3, domain.Preferences{RemindersEnabled: true, QuietHours: &domain.QuietHours{Start: "22:00", End: "09:00"}}))
	a := f.assignment(t, 48*time.Hour) // 08:00 Moscow.
	f.drain(t)
	if len(f.tg.sent) != 1 || f.tg.sent[0].ChatID != 2 {
		t.Fatal("quiet participant received immediate notice")
	}
	title := "Updated topic"
	a, e := f.app.UpdateAssignment(ctx, 2, a.ID, a.Version, domain.AssignmentPatch{Title: &title})
	check(t, e)
	// Moving quiet hours keeps the queued notice and rechecks it at dispatch.
	check(t, f.app.SavePreferences(ctx, 3, domain.Preferences{RemindersEnabled: true, QuietHours: &domain.QuietHours{Start: "22:00", End: "10:00"}}))
	f.now = f.now.Add(time.Hour)
	f.drain(t)
	if len(f.tg.sent) != 1 || f.number(t, "SELECT count(*) FROM outbox WHERE kind='assignment_created' AND status='pending' AND attempts=0") != 1 {
		t.Fatal("quiet change lost notice or consumed an HTTP attempt")
	}
	f.now = f.now.Add(time.Hour)
	f.drain(t)
	if len(f.tg.sent) != 2 || !strings.Contains(f.tg.sent[1].Text, title) || f.tg.sent[1].Buttons[0].Label != title {
		t.Fatal("pending notice did not render latest content")
	}
	_, e = f.app.UpdateAssignment(ctx, 2, a.ID, a.Version, domain.AssignmentPatch{Title: &title})
	check(t, e)
	f.drain(t)
	if len(f.tg.sent) != 2 {
		t.Fatal("edit created another notice")
	}
}

func TestAssignmentCreated_CancellationAndUndo(t *testing.T) {
	for _, action := range []string{"done undo", "leave rejoin", "remove", "disable enable", "cancel", "deadline", "quiet beyond deadline"} {
		t.Run(action, func(t *testing.T) {
			f := newFixture(t)
			ctx := t.Context()
			a := f.assignment(t, 12*time.Hour)
			switch action {
			case "done undo":
				_, e := f.app.SetComplete(ctx, 3, a.ID, a.Version, true)
				check(t, e)
				_, e = f.app.SetComplete(ctx, 3, a.ID, a.Version, false)
				check(t, e)
			case "leave rejoin":
				check(t, f.app.LeaveGroup(ctx, 3))
				_, e := f.app.JoinGroup(ctx, 3, f.group.InviteCode)
				check(t, e)
			case "remove":
				check(t, f.app.RemoveMember(ctx, 1, f.users[3].ID))
			case "disable enable":
				check(t, f.app.SavePreferences(ctx, 3, domain.Preferences{}))
				check(t, f.app.SavePreferences(ctx, 3, domain.Preferences{RemindersEnabled: true}))
			case "cancel":
				check(t, f.app.CancelAssignment(ctx, 2, a.ID, a.Version))
			case "deadline":
				f.now = a.DeadlineAt
			case "quiet beyond deadline":
				check(t, f.app.SavePreferences(ctx, 3, domain.Preferences{RemindersEnabled: true, QuietHours: &domain.QuietHours{Start: "07:00", End: "23:00"}}))
			}
			f.drain(t)
			for _, m := range f.tg.sent {
				if m.ChatID == 3 || action == "cancel" || action == "deadline" {
					t.Fatalf("obsolete notice sent: %+v", m)
				}
			}
		})
	}
}

func TestAssignmentCreated_RetryAndBlockCancelOtherNotices(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	f.assignment(t, 48*time.Hour)
	f.assignment(t, 49*time.Hour)
	f.tg.failure = &domain.DeliveryFailure{Code: "429", RetryAfter: time.Minute}
	ok, e := f.app.DispatchOne(ctx, f.now)
	check(t, e)
	if !ok || f.number(t, "SELECT count(*) FROM outbox WHERE attempts=1 AND next_attempt_at=?", stamp(f.now.Add(time.Minute))) != 1 {
		t.Fatal("creation notice retry_after not persisted")
	}
	f.tg.failure = &domain.DeliveryFailure{Code: "403", Permanent: true}
	f.drain(t)
	f.now = f.now.Add(time.Minute)
	f.tg.failure = nil
	f.drain(t)
	if len(f.tg.sent) != 0 || f.number(t, "SELECT count(*) FROM outbox WHERE status IN ('pending','processing')") != 0 {
		t.Fatal("403 failed to cancel all private deliveries")
	}
	for _, id := range []int64{2, 3} {
		u, e := f.store.Repositories().Users.ByTelegramID(ctx, id)
		check(t, e)
		if u.PrivateChatAvailable {
			t.Fatal("blocked chat still available")
		}
		_, e = f.app.Register(ctx, domain.TelegramUser{TelegramID: id, DisplayName: "Test"})
		check(t, e)
	}
	f.drain(t)
	if len(f.tg.sent) != 0 {
		t.Fatal("restoring private chat backfilled old notices")
	}
}

func TestTasks_AllFiltersBeforePaginationAndPreservesHistory(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	for i := 0; i < 25; i++ {
		a := f.assignment(t, time.Hour)
		_, e := f.app.SetComplete(ctx, 3, a.ID, a.Version, true)
		check(t, e)
	}
	boundary := f.assignment(t, time.Hour)
	first := f.assignment(t, 2*time.Hour)
	second := f.assignment(t, 3*time.Hour)
	f.now = boundary.DeadlineAt
	for _, period := range []string{"", "all"} {
		p, e := f.app.Tasks(ctx, 3, domain.AssignmentFilter{Period: period, SubjectID: &f.subject.ID}, domain.PageRequest{Limit: 1})
		check(t, e)
		if len(p.Items) != 1 || p.Items[0].ID != first.ID || p.NextCursor == "" {
			t.Fatalf("first page: %+v", p)
		}
		p, e = f.app.Tasks(ctx, 3, domain.AssignmentFilter{Period: period}, domain.PageRequest{Limit: 1, Cursor: p.NextCursor})
		check(t, e)
		if len(p.Items) != 1 || p.Items[0].ID != second.ID || p.NextCursor != "" {
			t.Fatalf("second page: %+v", p)
		}
	}
	_, e := f.app.SetComplete(ctx, 3, first.ID, first.Version, true)
	check(t, e)
	other, e := f.app.Tasks(ctx, 2, domain.AssignmentFilter{Period: "all"}, domain.PageRequest{})
	check(t, e)
	if len(other.Items) != 2 {
		t.Fatal("personal completion hid task from another member")
	}
	_, e = f.app.SetComplete(ctx, 3, first.ID, first.Version, false)
	check(t, e)
	p, e := f.app.Tasks(ctx, 3, domain.AssignmentFilter{Period: "all"}, domain.PageRequest{})
	check(t, e)
	if len(p.Items) != 2 {
		t.Fatal("undo did not restore future task")
	}
	_, e = f.app.Tasks(ctx, 3, domain.AssignmentFilter{Period: "all", DoneFilter: "done"}, domain.PageRequest{})
	expect(t, e, domain.ErrInvalidInput)
	p, e = f.app.Tasks(ctx, 3, domain.AssignmentFilter{Period: "day", DoneFilter: "done"}, domain.PageRequest{})
	check(t, e)
	if len(p.Items) != 10 || p.NextCursor == "" {
		t.Fatal("explicit history filter changed")
	}
	if f.number(t, "SELECT count(*) FROM assignments") != 28 {
		t.Fatal("list filter deleted assignments")
	}
}
