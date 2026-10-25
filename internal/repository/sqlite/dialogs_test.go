package sqlite

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
	"github.com/IvanCeo/NSMU_medical_bot/internal/service"
)

type wizard struct {
	t     *testing.T
	f     *fixture
	actor int64
	view  domain.DialogView
}

func TestDialogs_ConcurrentRevision(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	w := f.wizard(t, 2, domain.DialogAdd, 0)
	value := fmt.Sprint(f.subject.ID)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			_, e := f.app.Apply(ctx, 2, w.view.ID, w.view.Revision, domain.DialogInput{Selection: &value})
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, domain.ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success/conflict=%d/%d", success, conflict)
	}
}

type failDialogSave struct{ service.DialogRepo }

func (failDialogSave) Save(context.Context, domain.Dialog, int64) (domain.Dialog, error) {
	return domain.Dialog{}, domain.ErrConflict
}

type failingDialogWork struct{ base service.UnitOfWork }

func (w failingDialogWork) WithinTx(ctx context.Context, fn func(service.Repositories) error) error {
	return w.base.WithinTx(ctx, func(r service.Repositories) error { r.Dialogs = failDialogSave{r.Dialogs}; return fn(r) })
}
func TestDialogs_PublicationRollsBackIfCompletionFails(t *testing.T) {
	f := newFixture(t)
	w := f.wizard(t, 2, domain.DialogAdd, 0)
	w.selectValue(fmt.Sprint(f.subject.ID))
	w.selectValue("homework")
	w.text("Atomic task")
	w.text("-")
	w.text(f.now.Add(48 * time.Hour).In(time.FixedZone("test", 3*3600)).Format("02.01.2006 15:04"))
	w.selectValue("normal")
	w.text("-")
	w.selectValue("finish")
	app := service.New(failingDialogWork{f.store}, f.tg, f.tg, service.Options{Now: func() time.Time { return f.now }, ReminderOffsets: []time.Duration{3 * time.Hour}})
	confirm := "confirm"
	_, e := app.Apply(t.Context(), 2, w.view.ID, w.view.Revision, domain.DialogInput{Selection: &confirm})
	expect(t, e, domain.ErrConflict)
	if f.number(t, "SELECT count(*) FROM assignments") != 0 || f.number(t, "SELECT count(*) FROM reminders") != 0 {
		t.Fatal("publication escaped failed transaction")
	}
	current, e := f.app.Current(t.Context(), 2)
	check(t, e)
	if current == nil || current.Revision != w.view.Revision {
		t.Fatal("dialog completion escaped failed transaction")
	}
}

func (f *fixture) wizard(t *testing.T, actor int64, kind domain.DialogKind, entity int64) *wizard {
	t.Helper()
	v, e := f.app.Start(t.Context(), actor, kind, entity)
	check(t, e)
	return &wizard{t, f, actor, v}
}
func (w *wizard) selectValue(v string) {
	w.t.Helper()
	next, e := w.f.app.Apply(w.t.Context(), w.actor, w.view.ID, w.view.Revision, domain.DialogInput{Selection: &v})
	check(w.t, e)
	w.view = next
}
func (w *wizard) text(v string) {
	w.t.Helper()
	next, e := w.f.app.Apply(w.t.Context(), w.actor, w.view.ID, w.view.Revision, domain.DialogInput{Text: &v})
	check(w.t, e)
	w.view = next
}
func (w *wizard) step(want string) {
	w.t.Helper()
	if w.view.Step != want {
		w.t.Fatalf("step=%s, want %s", w.view.Step, want)
	}
}
func TestDialogs_AdminCompleteFlow(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	w := f.wizard(t, 1, domain.DialogAdmin, 0)
	w.selectValue("create")
	w.text("Created through wizard")
	w.step("admin_group")
	w.selectValue("rename")
	w.text("Renamed through wizard")
	w.selectValue("rotate")
	w.selectValue("digest_toggle")
	w.selectValue("subjects")
	w.selectValue("create_subject")
	w.text("Histology")
	w.text("-")
	w.step("subject_menu")
	w.selectValue("edit_subject")
	w.step("subject_list")
	id := w.view.Options[0].Value
	w.selectValue(id)
	w.selectValue("rename_subject")
	w.text("Histology II")
	w.selectValue("edit_subject")
	w.selectValue(id)
	w.selectValue("teacher")
	w.text("Test teacher")
	w.selectValue("edit_subject")
	w.selectValue(id)
	w.selectValue("archive")
	w.selectValue("edit_subject")
	w.selectValue(id)
	w.selectValue("restore")
	w = f.wizard(t, 1, domain.DialogAdmin, 0)
	w.selectValue(fmt.Sprint(f.group.ID))
	w.selectValue("members")
	w.selectValue(fmt.Sprint(f.users[3].ID))
	w.selectValue("grant_editor")
	w.selectValue("members")
	w.selectValue(fmt.Sprint(f.users[3].ID))
	w.selectValue("revoke_editor")
	w.selectValue("members")
	w.selectValue(fmt.Sprint(f.users[3].ID))
	w.selectValue("remove_member")
	w.selectValue("confirm")
	w.selectValue("digest_preview")
	w.selectValue("back")
	w.selectValue("audit")
	for _, o := range w.view.Options {
		if o.Value == "next" {
			w.selectValue("next")
			break
		}
	}
	w.selectValue("back")
	check(t, f.app.Cancel(ctx, 1))
	if f.number(t, "SELECT count(*) FROM dialog_states WHERE status='active'") != 0 {
		t.Fatal("cancel left active dialog")
	}
}
func TestDialogs_JoinLeaveAndSettings(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	w := f.wizard(t, 4, domain.DialogJoin, 0)
	w.text(f.group.InviteCode)
	w.step("completed")
	w = f.wizard(t, 4, domain.DialogLeave, 0)
	w.selectValue("confirm")
	w.step("completed")
	w = f.wizard(t, 3, domain.DialogSettings, 0)
	w.selectValue("toggle")
	p, e := f.app.Preferences(ctx, 3)
	check(t, e)
	if p.RemindersEnabled {
		t.Fatal("toggle not saved")
	}
	w.selectValue("toggle")
	w.selectValue("quiet")
	w.text("22:30")
	w.text("07:30")
	w.step("settings")
	w.selectValue("quiet_off")
	_, e = f.app.Start(ctx, 3, domain.DialogAdmin, 0)
	expect(t, e, domain.ErrForbidden)
	_, e = f.app.Start(ctx, 3, domain.DialogAdd, 0)
	expect(t, e, domain.ErrForbidden)
	_, e = f.app.Start(ctx, 4, domain.DialogLeave, 0)
	expect(t, e, domain.ErrNotInGroup)
	_, e = f.app.Start(ctx, 4, domain.DialogAdd, 0)
	expect(t, e, domain.ErrNotInGroup)
	_, e = f.app.Start(ctx, 3, domain.DialogKind("unknown"), 0)
	expect(t, e, domain.ErrInvalidInput)
}
func TestDialogs_EditCancelAndAdminPublication(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	a := f.assignment(t, 48*time.Hour)
	w := f.wizard(t, 2, domain.DialogEdit, 0)
	w.text(fmt.Sprint(a.ID))
	w.selectValue("keep")
	w.selectValue("exam")
	w.text("Edited title")
	w.selectValue("keep")
	w.selectValue("keep")
	w.selectValue("high")
	w.selectValue("keep")
	w.selectValue("clear")
	w.selectValue("finish")
	w.selectValue("confirm")
	w.step("completed")
	v, e := f.app.Task(ctx, 3, a.ID)
	check(t, e)
	if v.Title != "Edited title" || len(v.Attachments) != 0 || v.Version != 2 {
		t.Fatal("edit lost changes")
	}
	w = f.wizard(t, 2, domain.DialogCancelTask, a.ID)
	w.selectValue("confirm")
	w.step("completed")
	w = f.wizard(t, 1, domain.DialogAdd, 0)
	w.selectValue(fmt.Sprint(f.group.ID))
	w.selectValue(fmt.Sprint(f.subject.ID))
	w.selectValue("practice")
	w.text("Admin task")
	w.text("-")
	w.text(f.now.Add(50 * time.Hour).In(time.FixedZone("test", 3*3600)).Format("02.01.2006 15:04"))
	w.selectValue("low")
	w.text("https://example.com/test")
	w.selectValue("finish")
	w.selectValue("confirm")
	w = f.wizard(t, 2, domain.DialogSubjects, 0)
	w.selectValue("create_subject")
	w.text("Physiology")
	w.text("Test teacher")
	w = f.wizard(t, 1, domain.DialogSubjects, 0)
	w.selectValue(fmt.Sprint(f.group.ID))
	w.step("subject_menu")
}
func TestDialogs_ForeignAndExpiredButtons(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	w := f.wizard(t, 2, domain.DialogAdd, 0)
	value := fmt.Sprint(f.subject.ID)
	_, e := f.app.Apply(ctx, 3, w.view.ID, w.view.Revision, domain.DialogInput{Selection: &value})
	expect(t, e, domain.ErrExpiredDialog)
	bad := "999999"
	_, e = f.app.Apply(ctx, 2, w.view.ID, w.view.Revision, domain.DialogInput{Selection: &bad})
	expect(t, e, domain.ErrInvalidInput)
	_, e = f.app.Apply(ctx, 2, w.view.ID, w.view.Revision, domain.DialogInput{Selection: &value, Text: &value})
	expect(t, e, domain.ErrInvalidInput)
	before := w.view.ID
	w = f.wizard(t, 2, domain.DialogAdd, 0)
	if w.view.ID == before {
		t.Fatal("dialog ID reused")
	}
	_, e = f.app.Apply(ctx, 2, before, 1, domain.DialogInput{Selection: &value})
	expect(t, e, domain.ErrExpiredDialog)
	f.now = f.now.Add(time.Hour)
	v, e := f.app.Current(ctx, 2)
	check(t, e)
	if v != nil {
		t.Fatal("expired current dialog not removed")
	}
}
