package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
	"github.com/IvanCeo/NSMU_medical_bot/internal/service"
)

type fakeTelegram struct {
	sent        []domain.Outgoing
	failure     error
	permissions domain.ChatPermissions
}

func (f *fakeTelegram) CheckBinding(context.Context, int64, int64) (domain.ChatPermissions, error) {
	return f.permissions, nil
}
func (f *fakeTelegram) SendNotification(_ context.Context, m domain.Outgoing) (domain.MessageReceipt, error) {
	if f.failure != nil {
		return domain.MessageReceipt{}, f.failure
	}
	f.sent = append(f.sent, m)
	return domain.MessageReceipt{ChatID: m.ChatID, MessageID: len(f.sent)}, nil
}

type fixture struct {
	app     *service.App
	store   *Store
	dbpath  string
	now     time.Time
	tg      *fakeTelegram
	group   domain.Group
	subject domain.Subject
	users   map[int64]domain.User
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := openTestDB(t)
	check(t, Migrate(t.Context(), db))
	f := &fixture{store: NewStore(db), dbpath: "", now: time.Date(2026, 10, 25, 5, 0, 0, 0, time.UTC), tg: &fakeTelegram{permissions: domain.ChatPermissions{ActorIsAdministrator: true, BotIsAdministrator: true, BotCanSend: true}}, users: make(map[int64]domain.User)}
	check(t, service.BootstrapAdmins(t.Context(), NewUsers(db), []int64{1}, f.now))
	f.wire()
	for _, id := range []int64{1, 2, 3, 4} {
		u, e := f.app.Register(t.Context(), domain.TelegramUser{TelegramID: id, DisplayName: fmt.Sprintf("Test %d", id)})
		check(t, e)
		f.users[id] = u
	}
	var e error
	f.group, e = f.app.CreateGroup(t.Context(), 1, "Test cohort")
	check(t, e)
	for _, id := range []int64{2, 3} {
		_, e = f.app.JoinGroup(t.Context(), id, f.group.InviteCode)
		check(t, e)
	}
	check(t, f.app.SetEditor(t.Context(), 1, f.users[2].ID, true))
	f.subject, e = f.app.CreateSubject(t.Context(), 2, domain.SubjectInput{Name: "Anatomy"})
	check(t, e)
	return f
}
func (f *fixture) wire() {
	tz, _ := time.LoadLocation("Europe/Moscow")
	f.app = service.New(f.store, f.tg, f.tg, service.Options{Now: func() time.Time { return f.now }, Timezone: tz, ReminderOffsets: []time.Duration{24 * time.Hour, 3 * time.Hour}, DigestTime: "08:00"})
}
func check(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func expect(t *testing.T, e, want error) {
	t.Helper()
	if !errors.Is(e, want) {
		t.Fatalf("got %v, want %v", e, want)
	}
}
func (f *fixture) assignment(t *testing.T, after time.Duration) domain.Assignment {
	t.Helper()
	a, e := f.app.CreateAssignment(t.Context(), 2, domain.AssignmentInput{SubjectID: f.subject.ID, Kind: domain.Homework, Title: "Test assignment", DeadlineAt: f.now.Add(after), Attachments: []domain.AttachmentInput{{Kind: domain.Document, FileID: "test-document"}}})
	check(t, e)
	return a
}
func (f *fixture) number(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	check(t, f.store.db.QueryRowContext(t.Context(), query, args...).Scan(&n))
	return n
}
func TestMVP_MembershipAndAccess(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	a := f.assignment(t, 48*time.Hour)
	_, e := f.app.Task(ctx, 4, a.ID)
	expect(t, e, domain.ErrNotInGroup)
	other, e := f.app.CreateGroup(ctx, 1, "Other cohort")
	check(t, e)
	_, e = f.app.JoinGroup(ctx, 4, other.InviteCode)
	check(t, e)
	_, e = f.app.Task(ctx, 4, a.ID)
	expect(t, e, domain.ErrForbidden)
	_, e = f.app.Tasks(ctx, 4, domain.AssignmentFilter{GroupID: f.group.ID}, domain.PageRequest{})
	expect(t, e, domain.ErrForbidden)
	_, e = f.app.CreateSubject(ctx, 3, domain.SubjectInput{Name: "Forbidden"})
	expect(t, e, domain.ErrForbidden)
	_, e = f.app.CreateSubject(ctx, 4, domain.SubjectInput{GroupID: f.group.ID, Name: "Forbidden"})
	expect(t, e, domain.ErrForbidden)
	_, e = f.app.JoinGroup(ctx, 3, other.InviteCode)
	expect(t, e, domain.ErrConflict)
	check(t, f.app.LeaveGroup(ctx, 3))
	_, e = f.app.JoinGroup(ctx, 3, other.InviteCode)
	check(t, e)
	expect(t, f.app.LeaveGroup(ctx, 2), domain.ErrForbidden)
	check(t, f.app.SetEditor(ctx, 1, f.users[2].ID, false))
	check(t, f.app.LeaveGroup(ctx, 2))
	p, e := f.app.Profile(ctx, 3)
	check(t, e)
	if p.Group.InviteCode != "" {
		t.Fatal("student saw invite code")
	}
	_, e = f.app.Groups(ctx, 3, domain.PageRequest{})
	expect(t, e, domain.ErrForbidden)
	_, e = f.app.Members(ctx, 3, f.group.ID, domain.PageRequest{})
	expect(t, e, domain.ErrForbidden)
	expect(t, f.app.SetEditor(ctx, 3, f.users[4].ID, true), domain.ErrForbidden)
	expect(t, f.app.SetEditor(ctx, 1, f.users[1].ID, true), domain.ErrForbidden)
	check(t, f.app.RemoveMember(ctx, 1, f.users[3].ID))
	expect(t, f.app.RemoveMember(ctx, 1, f.users[3].ID), domain.ErrNotInGroup)
	_, e = f.app.JoinGroup(ctx, 3, other.InviteCode)
	check(t, e)
	_, e = f.app.JoinGroup(ctx, 3, other.InviteCode)
	check(t, e)
}
func TestMVP_AssignmentsVersionsAndCompletions(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	a := f.assignment(t, 48*time.Hour)
	if n := f.number(t, "SELECT count(*) FROM reminders WHERE assignment_id=?", a.ID); n != 4 {
		t.Fatalf("reminders=%d", n)
	}
	_, e := f.app.SetComplete(ctx, 3, a.ID, a.Version, true)
	check(t, e)
	_, e = f.app.SetComplete(ctx, 3, a.ID, a.Version, true)
	check(t, e)
	v, e := f.app.Task(ctx, 3, a.ID)
	check(t, e)
	if !v.Done {
		t.Fatal("completion missing")
	}
	v, e = f.app.Task(ctx, 2, a.ID)
	check(t, e)
	if v.Done || len(v.Attachments) != 1 {
		t.Fatal("personal completion leaked or attachment missing")
	}
	_, e = f.app.SetComplete(ctx, 3, a.ID, a.Version, false)
	check(t, e)
	title := "Changed"
	deadline := f.now.Add(72 * time.Hour)
	a2, e := f.app.UpdateAssignment(ctx, 2, a.ID, a.Version, domain.AssignmentPatch{Title: &title, DeadlineAt: &deadline})
	check(t, e)
	_, e = f.app.UpdateAssignment(ctx, 2, a.ID, a.Version, domain.AssignmentPatch{Title: &title})
	expect(t, e, domain.ErrConflict)
	_, e = f.app.SetComplete(ctx, 3, a.ID, a.Version, true)
	expect(t, e, domain.ErrConflict)
	check(t, f.app.Snooze(ctx, 3, a.ID, a2.Version, 3*time.Hour))
	expect(t, f.app.Snooze(ctx, 3, a.ID, a2.Version, time.Hour), domain.ErrInvalidInput)
	other, e := f.app.CreateGroup(ctx, 1, "Other")
	check(t, e)
	sub, e := f.app.CreateSubject(ctx, 1, domain.SubjectInput{GroupID: other.ID, Name: "Other subject"})
	check(t, e)
	_, e = f.app.UpdateAssignment(ctx, 2, a.ID, a2.Version, domain.AssignmentPatch{SubjectID: &sub.ID})
	expect(t, e, domain.ErrForbidden)
	check(t, f.app.CancelAssignment(ctx, 2, a.ID, a2.Version))
	v, e = f.app.Task(ctx, 3, a.ID)
	check(t, e)
	if v.Status != domain.Cancelled {
		t.Fatal("not cancelled")
	}
	if f.number(t, "SELECT count(*) FROM reminders WHERE assignment_id=? AND sent_at IS NULL AND cancelled_at IS NULL", a.ID) != 0 {
		t.Fatal("cancel kept reminders")
	}
	if f.number(t, "SELECT count(*) FROM audit_log") < 8 {
		t.Fatal("audit missing")
	}
}
func TestMVP_SubjectsSettingsAndPages(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	archived := true
	sub, e := f.app.UpdateSubject(ctx, 2, f.subject.ID, 1, domain.SubjectPatch{Archived: &archived})
	check(t, e)
	_, e = f.app.CreateAssignment(ctx, 2, domain.AssignmentInput{SubjectID: sub.ID, Kind: domain.Test, Title: "Test", DeadlineAt: f.now.Add(time.Hour)})
	expect(t, e, domain.ErrInvalidInput)
	name := " Anatomy "
	_, e = f.app.CreateSubject(ctx, 2, domain.SubjectInput{Name: name})
	check(t, e)
	archived = false
	_, e = f.app.UpdateSubject(ctx, 2, sub.ID, sub.Version, domain.SubjectPatch{Archived: &archived})
	expect(t, e, domain.ErrConflict)
	name = "Restored"
	sub, e = f.app.UpdateSubject(ctx, 2, sub.ID, sub.Version, domain.SubjectPatch{Name: &name, Archived: &archived})
	check(t, e)
	f.subject = sub
	for i := 0; i < 23; i++ {
		f.assignment(t, time.Duration(48+i)*time.Hour)
	}
	p, e := f.app.Tasks(ctx, 3, domain.AssignmentFilter{Period: "all"}, domain.PageRequest{Limit: 20})
	check(t, e)
	if len(p.Items) != 20 || p.NextCursor == "" {
		t.Fatal("pagination missing")
	}
	p2, e := f.app.Tasks(ctx, 3, domain.AssignmentFilter{Period: "all"}, domain.PageRequest{Limit: 20, Cursor: p.NextCursor})
	check(t, e)
	if len(p2.Items) != 3 || p2.Items[0].ID == p.Items[0].ID {
		t.Fatal("page overlap")
	}
	_, e = f.app.Tasks(ctx, 3, domain.AssignmentFilter{}, domain.PageRequest{Cursor: "broken"})
	expect(t, e, domain.ErrInvalidInput)
	_, e = f.app.Tasks(ctx, 3, domain.AssignmentFilter{Period: "invalid"}, domain.PageRequest{})
	expect(t, e, domain.ErrInvalidInput)
	pref := domain.Preferences{RemindersEnabled: false}
	check(t, f.app.SavePreferences(ctx, 3, pref))
	pref, e = f.app.Preferences(ctx, 3)
	check(t, e)
	if pref.RemindersEnabled {
		t.Fatal("not disabled")
	}
	pref.RemindersEnabled = true
	pref.QuietHours = &domain.QuietHours{Start: "22:00", End: "08:00"}
	check(t, f.app.SavePreferences(ctx, 3, pref))
	pref.QuietHours.End = "22:00"
	expect(t, f.app.SavePreferences(ctx, 3, pref), domain.ErrInvalidInput)
	pref.QuietHours = nil
	check(t, f.app.SavePreferences(ctx, 3, pref))
	_, e = f.app.Subjects(ctx, 3, 0, true, domain.PageRequest{})
	expect(t, e, domain.ErrForbidden)
	_, e = f.app.Subjects(ctx, 2, 0, true, domain.PageRequest{Limit: 1})
	check(t, e)
}
func TestMVP_GroupAdministrationAndBinding(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	groups, e := f.app.Groups(ctx, 1, domain.PageRequest{})
	check(t, e)
	if len(groups.Items) != 1 {
		t.Fatal("group missing")
	}
	title := "Renamed"
	g, e := f.app.UpdateGroup(ctx, 1, f.group.ID, f.group.Version, domain.GroupPatch{Title: &title})
	check(t, e)
	_, e = f.app.UpdateGroup(ctx, 1, g.ID, 1, domain.GroupPatch{Title: &title})
	expect(t, e, domain.ErrConflict)
	old := g.InviteCode
	code, e := f.app.RotateInvite(ctx, 1, g.ID)
	check(t, e)
	if code == old {
		t.Fatal("invite not rotated")
	}
	_, e = f.app.JoinGroup(ctx, 4, old)
	expect(t, e, domain.ErrNotFound)
	f.tg.permissions.ActorIsAdministrator = false
	expect(t, f.app.BindChat(ctx, 1, g.ID, -100), domain.ErrForbidden)
	f.tg.permissions.ActorIsAdministrator = true
	check(t, f.app.BindChat(ctx, 1, g.ID, -100))
	check(t, f.app.BindChat(ctx, 1, g.ID, -100))
	expect(t, f.app.BindChat(ctx, 2, g.ID, -100), domain.ErrForbidden)
	other, e := f.app.CreateGroup(ctx, 1, "Other")
	check(t, e)
	expect(t, f.app.BindChat(ctx, 1, other.ID, -100), domain.ErrConflict)
	check(t, f.app.BindChat(ctx, 1, g.ID, -200))
	members, e := f.app.Members(ctx, 1, g.ID, domain.PageRequest{Limit: 1})
	check(t, e)
	if members.NextCursor == "" {
		t.Fatal("member pagination missing")
	}
	_, e = f.app.Audit(ctx, 1, g.ID, domain.PageRequest{Limit: 1})
	check(t, e)
	_, e = f.app.PreviewDigest(ctx, 1, g.ID, f.now)
	check(t, e)
}
func TestMVP_DialogPersistenceAndAtomicPublication(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	v, e := f.app.Start(ctx, 2, domain.DialogAdd, 0)
	check(t, e)
	selectStep := func(choice string) {
		t.Helper()
		var e error
		v, e = f.app.Apply(ctx, 2, v.ID, v.Revision, domain.DialogInput{Selection: &choice})
		check(t, e)
	}
	textStep := func(text string) {
		t.Helper()
		var e error
		v, e = f.app.Apply(ctx, 2, v.ID, v.Revision, domain.DialogInput{Text: &text})
		check(t, e)
	}
	selectStep(fmt.Sprint(f.subject.ID))
	selectStep("homework")
	textStep("Draft task")
	stale := v
	f.wire()
	current, e := f.app.Current(ctx, 2)
	check(t, e)
	if current == nil || current.ID != v.ID || current.Revision != v.Revision {
		t.Fatal("dialog did not survive service restart")
	}
	textStep("Description")
	text := "duplicate"
	_, e = f.app.Apply(ctx, 2, stale.ID, stale.Revision, domain.DialogInput{Text: &text})
	expect(t, e, domain.ErrConflict)
	textStep(f.now.Add(48 * time.Hour).In(time.FixedZone("test", 3*3600)).Format("02.01.2006 15:04"))
	selectStep("normal")
	textStep("-")
	file := domain.AttachmentInput{Kind: domain.Photo, FileID: "test-photo"}
	v, e = f.app.Apply(ctx, 2, v.ID, v.Revision, domain.DialogInput{Attachment: &file})
	check(t, e)
	selectStep("finish")
	if f.number(t, "SELECT count(*) FROM assignments") != 0 {
		t.Fatal("draft published before confirmation")
	}
	selectStep("confirm")
	if v.Step != "completed" || f.number(t, "SELECT count(*) FROM assignments") != 1 {
		t.Fatal("publish failed")
	}
	_, e = f.app.Apply(ctx, 2, stale.ID, stale.Revision, domain.DialogInput{Text: &text})
	expect(t, e, domain.ErrExpiredDialog)
	v, e = f.app.Start(ctx, 2, domain.DialogEdit, 1)
	check(t, e)
	check(t, f.app.SetEditor(ctx, 1, f.users[2].ID, false))
	_, e = f.app.Apply(ctx, 2, v.ID, v.Revision, domain.DialogInput{Text: &text})
	expect(t, e, domain.ErrForbidden)
	check(t, f.app.SetEditor(ctx, 1, f.users[2].ID, true))
	check(t, f.app.Cancel(ctx, 2))
	next, e := f.app.Start(ctx, 2, domain.DialogEdit, 1)
	check(t, e)
	if next.ID == v.ID {
		t.Fatal("dialog ID reused")
	}
	f.now = f.now.Add(31 * time.Minute)
	_, e = f.app.Apply(ctx, 2, next.ID, next.Revision, domain.DialogInput{Text: &text})
	expect(t, e, domain.ErrExpiredDialog)
	check(t, f.app.ExpireDialogs(ctx, f.now))
	current, e = f.app.Current(ctx, 2)
	check(t, e)
	if current != nil {
		t.Fatal("expired dialog active")
	}
}
func TestMVP_DatabaseReopen(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "restart.db")
	db, e := Open(ctx, path)
	check(t, e)
	check(t, Migrate(ctx, db))
	check(t, NewUsers(db).SyncAdmins(ctx, []int64{1}, time.Now()))
	tg := &fakeTelegram{}
	app := service.New(NewStore(db), tg, tg, service.Options{})
	_, e = app.Register(ctx, domain.TelegramUser{TelegramID: 1})
	check(t, e)
	v, e := app.Start(ctx, 1, domain.DialogJoin, 0)
	check(t, e)
	check(t, db.Close())
	db, e = Open(ctx, path)
	check(t, e)
	defer func() { check(t, db.Close()) }()
	app = service.New(NewStore(db), tg, tg, service.Options{})
	current, e := app.Current(ctx, 1)
	check(t, e)
	if current == nil || current.ID != v.ID {
		t.Fatal("draft lost after DB reopen")
	}
}
func TestMVP_ValidationRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	before := f.number(t, "SELECT count(*) FROM assignments")
	for _, in := range []domain.AssignmentInput{{SubjectID: f.subject.ID, Kind: domain.Homework, Title: strings.Repeat("x", 201), DeadlineAt: f.now.Add(time.Hour)}, {SubjectID: f.subject.ID, Kind: domain.Homework, Title: "Test", DeadlineAt: f.now.Add(-time.Hour)}, {SubjectID: f.subject.ID, Kind: domain.Homework, Title: "Test", DeadlineAt: f.now.Add(time.Hour), URL: "javascript:alert(1)"}} {
		_, e := f.app.CreateAssignment(ctx, 2, in)
		expect(t, e, domain.ErrInvalidInput)
	}
	if f.number(t, "SELECT count(*) FROM assignments") != before {
		t.Fatal("invalid data persisted")
	}
}
