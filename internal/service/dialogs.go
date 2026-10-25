package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

type dialogDraft struct {
	EntityID, Version, GroupID, TargetUserID int64
	Action, Cursor                           string
	Assignment                               domain.AssignmentInput
	Subject                                  domain.SubjectInput
	SubjectVersion                           int64
	Preferences                              domain.Preferences
}

func draftOf(d domain.Dialog) (dialogDraft, error) {
	var p dialogDraft
	e := json.Unmarshal([]byte(d.Payload), &p)
	if e != nil {
		return p, fmt.Errorf("decode dialog: %w", domain.ErrStorage)
	}
	return p, nil
}

func (s *App) saveDialog(ctx context.Context, r Repositories, d domain.Dialog, p dialogDraft) (domain.Dialog, error) {
	b, e := json.Marshal(p)
	if e != nil {
		return d, fmt.Errorf("encode dialog: %w", e)
	}
	d.Payload = string(b)
	d.UpdatedAt = s.now()
	d.ExpiresAt = s.now().Add(30 * time.Minute)
	return r.Dialogs.Save(ctx, d, d.Revision)
}

func (s *App) Start(ctx context.Context, id int64, kind domain.DialogKind, entityID int64) (domain.DialogView, error) {
	var out domain.DialogView
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		if e := r.Dialogs.Finish(ctx, u.ID); e != nil {
			return e
		}
		d := domain.Dialog{UserID: u.ID, Kind: kind, Status: domain.DialogActive}
		p := dialogDraft{EntityID: entityID}
		switch kind {
		case domain.DialogJoin:
			d.Step = "invite"
		case domain.DialogLeave:
			if u.GroupID == nil {
				return domain.ErrNotInGroup
			}
			if u.Role == domain.Editor {
				return domain.ErrForbidden
			}
			d.Step = "leave_confirm"
		case domain.DialogAdmin:
			if e := requireAdmin(u); e != nil {
				return e
			}
			d.Step = "admin_groups"
		case domain.DialogSettings:
			var e error
			p.Preferences, e = r.Settings.Get(ctx, u.ID)
			if e != nil {
				return e
			}
			d.Step = "settings"
		case domain.DialogAdd, domain.DialogSubjects:
			if u.Role == domain.Admin {
				d.Step = "choose_group"
			} else {
				gid, e := groupID(u, 0)
				if e != nil {
					return e
				}
				if e := access(u, gid, true); e != nil {
					return e
				}
				p.GroupID = gid
				d.Step = "subject_menu"
				if kind == domain.DialogAdd {
					d.Step = "task_subject"
				}
			}
		case domain.DialogEdit, domain.DialogCancelTask:
			if u.Role != domain.Admin && u.Role != domain.Editor {
				return domain.ErrForbidden
			}
			d.Step = "task_id"
			if entityID > 0 {
				if e := s.loadTaskDraft(ctx, r, u, &d, &p, entityID); e != nil {
					return e
				}
			}
		default:
			return domain.ErrInvalidInput
		}
		var e error
		d, e = s.saveDialog(ctx, r, d, p)
		if e != nil {
			return e
		}
		out, e = s.dialogView(ctx, r, u, d, p)
		return e
	})
	return out, e
}

func (s *App) loadTaskDraft(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, id int64) error {
	a, e := task(ctx, r, u, id, true)
	if e != nil {
		return e
	}
	if a.Status != domain.Active {
		return domain.ErrConflict
	}
	p.EntityID = a.ID
	p.Version = a.Version
	p.GroupID = a.GroupID
	p.Assignment = domain.AssignmentInput{GroupID: a.GroupID, SubjectID: a.SubjectID, Kind: a.Kind, Title: a.Title, Description: a.Description, DeadlineAt: a.DeadlineAt, Importance: a.Importance, URL: a.URL}
	for _, f := range a.Attachments {
		p.Assignment.Attachments = append(p.Assignment.Attachments, f.AttachmentInput)
	}
	d.Step = "task_subject"
	if d.Kind == domain.DialogCancelTask {
		d.Step = "task_cancel_confirm"
	}
	return nil
}

func (s *App) Current(ctx context.Context, id int64) (*domain.DialogView, error) {
	var out *domain.DialogView
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		d, e := r.Dialogs.ByUser(ctx, u.ID)
		if e != nil || d == nil {
			return e
		}
		if !d.ExpiresAt.After(s.now()) {
			return r.Dialogs.Expire(ctx, s.now())
		}
		p, e := draftOf(*d)
		if e != nil {
			return e
		}
		if e := dialogAccess(u, *d, p); e != nil {
			return e
		}
		v, e := s.dialogView(ctx, r, u, *d, p)
		out = &v
		return e
	})
	return out, e
}

func dialogAccess(u domain.User, d domain.Dialog, p dialogDraft) error {
	if d.Kind == domain.DialogAdmin {
		return requireAdmin(u)
	}
	if d.Kind == domain.DialogAdd || d.Kind == domain.DialogEdit || d.Kind == domain.DialogCancelTask || d.Kind == domain.DialogSubjects {
		if p.GroupID > 0 {
			return access(u, p.GroupID, true)
		}
		if u.Role != domain.Admin && u.Role != domain.Editor {
			return domain.ErrForbidden
		}
	}
	return nil
}

func (s *App) Cancel(ctx context.Context, id int64) error {
	return s.actor(ctx, id, func(r Repositories, u domain.User) error { return r.Dialogs.Finish(ctx, u.ID) })
}

func (s *App) Apply(ctx context.Context, id, did, v int64, input domain.DialogInput) (domain.DialogView, error) {
	var out domain.DialogView
	count := 0
	if input.Text != nil {
		count++
	}
	if input.Selection != nil {
		count++
	}
	if input.Attachment != nil {
		count++
	}
	if count != 1 {
		return out, domain.ErrInvalidInput
	}
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		d, e := r.Dialogs.ByUser(ctx, u.ID)
		if e != nil {
			return e
		}
		if d == nil || d.ID != did {
			return domain.ErrExpiredDialog
		}
		if !d.ExpiresAt.After(s.now()) {
			return domain.ErrExpiredDialog
		}
		if d.Revision != v {
			return domain.ErrConflict
		}
		p, e := draftOf(*d)
		if e != nil {
			return e
		}
		if e := dialogAccess(u, *d, p); e != nil {
			return e
		}
		if !validDialogInput(d.Step, input) {
			return domain.ErrInvalidInput
		}
		if input.Selection != nil {
			view, e := s.dialogView(ctx, r, u, *d, p)
			if e != nil {
				return e
			}
			valid := false
			for _, o := range view.Options {
				if o.Value == *input.Selection {
					valid = true
				}
			}
			if !valid {
				return domain.ErrInvalidInput
			}
		}
		if e := s.advance(ctx, r, u, d, &p, input); e != nil {
			return e
		}
		saved, e := s.saveDialog(ctx, r, *d, p)
		if e != nil {
			return e
		}
		out, e = s.dialogView(ctx, r, u, saved, p)
		return e
	})
	return out, e
}

func validDialogInput(step string, in domain.DialogInput) bool {
	switch step {
	case "invite", "group_title", "subject_name", "subject_teacher", "task_id", "quiet_start", "quiet_end":
		return in.Text != nil
	case "task_title", "task_description", "task_deadline", "task_url":
		return in.Text != nil || in.Selection != nil
	case "task_attachments":
		return in.Attachment != nil || in.Selection != nil
	default:
		return in.Selection != nil
	}
}

func (s *App) advance(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, in domain.DialogInput) error {
	text, choice := "", ""
	if in.Text != nil {
		text = strings.TrimSpace(*in.Text)
	}
	if in.Selection != nil {
		choice = *in.Selection
	}
	if strings.HasPrefix(d.Step, "task_") {
		return s.advanceTask(ctx, r, u, d, p, in)
	}
	if strings.HasPrefix(d.Step, "admin_") || strings.HasPrefix(d.Step, "member_") || strings.HasPrefix(d.Step, "group_") {
		return s.advanceAdmin(ctx, r, u, d, p, text, choice)
	}
	if strings.HasPrefix(d.Step, "subject_") {
		return s.advanceSubject(ctx, r, u, d, p, text, choice)
	}
	switch d.Step {
	case "invite":
		return s.advanceDialogInvite(ctx, r, u, d, in, text)
	case "leave_confirm":
		return s.advanceDialogLeaveConfirm(ctx, r, u, d, choice)
	case "choose_group":
		return s.advanceDialogChooseGroup(ctx, r, u, d, p, choice)
	case "settings":
		return s.advanceDialogSettings(ctx, r, u, d, p, choice)
	case "quiet_start":
		if !validClock(text) {
			return domain.ErrInvalidInput
		}
		p.Preferences.QuietHours = &domain.QuietHours{Start: text}
		d.Step = "quiet_end"
	case "quiet_end":
		return s.advanceDialogQuietEnd(ctx, r, u, d, p, text)
	default:
		return domain.ErrInvalidInput
	}
	return nil
}

func (s *App) dialogView(ctx context.Context, r Repositories, u domain.User, d domain.Dialog, p dialogDraft) (domain.DialogView, error) {
	v := domain.DialogView{ID: d.ID, Revision: d.Revision, Step: d.Step, PromptKey: d.Step}
	if d.Status != domain.DialogActive {
		v.PromptKey = "completed"
		v.Step = "completed"
		return v, nil
	}
	switch d.Step {
	case "choose_group", "admin_groups":
		return s.dialogOptionsChooseGroup(ctx, r, d, p, v)
	case "task_subject", "subject_list":
		return s.dialogOptionsTaskSubject(ctx, r, d, p, v)
	case "task_kind":
		v.Options = enumOptions("homework", "test", "exam", "practice", "other")
	case "task_importance":
		v.Options = enumOptions("low", "normal", "high")
	case "task_title", "task_description", "task_deadline", "task_url":
		if d.Kind == domain.DialogEdit {
			v.Options = enumOptions("keep")
		}
	case "task_attachments":
		v.Options = enumOptions("finish", "clear")
		v.Preview = strconv.Itoa(len(p.Assignment.Attachments))
	case "task_preview", "task_cancel_confirm", "leave_confirm", "member_remove_confirm":
		return s.dialogOptionsTaskPreview(d, p, v)
	case "settings":
		return s.dialogOptionsSettings(p, v)
	case "subject_menu":
		v.Options = enumOptions("create_subject", "edit_subject")
	case "subject_action":
		v.Options = enumOptions("rename_subject", "teacher", "archive", "restore")
	case "admin_group":
		return s.dialogOptionsAdminGroup(ctx, r, p, v)
	case "member_list":
		return s.dialogOptionsMemberList(ctx, r, p, v)
	case "member_action":
		v.Options = enumOptions("grant_editor", "revoke_editor", "remove_member")
	case "admin_digest":
		return s.dialogOptionsAdminDigest(ctx, r, p, v)
	case "admin_audit":
		return s.dialogOptionsAdminAudit(ctx, r, p, v)

	}
	return v, nil
}

func enumOptions(values ...string) []domain.Option {
	out := make([]domain.Option, 0, len(values))
	for _, v := range values {
		out = append(out, domain.Option{Label: v, Value: v})
	}
	return out
}

func (s *App) advanceDialogChooseGroup(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, choice string) error {
	if choice == "next" {
		gs, e := r.Groups.List(ctx, domain.PageRequest{Cursor: p.Cursor, Limit: 20})
		if e != nil {
			return e
		}
		p.Cursor = gs.NextCursor
		return nil
	}
	gid, e := parseID(choice)
	if e != nil {
		return e
	}
	if e := access(u, gid, true); e != nil {
		return e
	}
	p.GroupID = gid
	p.Cursor = ""
	d.Step = "subject_menu"
	if d.Kind == domain.DialogAdd {
		d.Step = "task_subject"
	}
	return nil
}

func (s *App) advanceDialogSettings(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, choice string) error {
	switch choice {
	case "toggle":
		p.Preferences.RemindersEnabled = !p.Preferences.RemindersEnabled
		return s.savePreferences(ctx, r, u, p.Preferences)
	case "quiet":
		d.Step = "quiet_start"
	case "quiet_off":
		p.Preferences.QuietHours = nil
		return s.savePreferences(ctx, r, u, p.Preferences)
	default:
		return domain.ErrInvalidInput
	}
	return nil
}

func (s *App) dialogOptionsChooseGroup(ctx context.Context, r Repositories, d domain.Dialog, p dialogDraft, v domain.DialogView) (domain.DialogView, error) {

	gs, e := r.Groups.List(ctx, domain.PageRequest{Cursor: p.Cursor, Limit: 20})
	if e != nil {
		return v, e
	}
	for _, g := range gs.Items {
		v.Options = append(v.Options, domain.Option{Label: fmt.Sprintf("#%d %s", g.ID, g.Title), Value: strconv.FormatInt(g.ID, 10)})
	}
	if gs.NextCursor != "" {
		v.Options = append(v.Options, domain.Option{Label: "next", Value: "next"})
	}
	if d.Step == "admin_groups" {
		v.Options = append(v.Options, domain.Option{Label: "create_group", Value: "create"})
	}
	return v, nil
}

func (s *App) dialogOptionsTaskSubject(ctx context.Context, r Repositories, d domain.Dialog, p dialogDraft, v domain.DialogView) (domain.DialogView, error) {

	subjects, e := r.Subjects.List(ctx, p.GroupID, d.Step == "subject_list", domain.PageRequest{Cursor: p.Cursor, Limit: 20})
	if e != nil {
		return v, e
	}
	for _, a := range subjects.Items {
		label := a.Name
		if a.ArchivedAt != nil {
			label += " [archive]"
		}
		v.Options = append(v.Options, domain.Option{Label: label, Value: strconv.FormatInt(a.ID, 10)})
	}
	if subjects.NextCursor != "" {
		v.Options = append(v.Options, domain.Option{Label: "next", Value: "next"})
	}
	if d.Step == "task_subject" && d.Kind == domain.DialogEdit {
		v.Options = append(v.Options, domain.Option{Label: "keep", Value: "keep"})
	}
	return v, nil
}

func (s *App) advanceDialogInvite(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, in domain.DialogInput, text string) error {
	if in.Text == nil {
		return domain.ErrInvalidInput
	}
	if _, e := s.join(ctx, r, u, text); e != nil {
		return e
	}
	d.Status = domain.DialogCompleted
	return nil
}

func (s *App) advanceDialogLeaveConfirm(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, choice string) error {
	if choice != "confirm" {
		return domain.ErrInvalidInput
	}
	if e := s.leave(ctx, r, u); e != nil {
		return e
	}
	d.Status = domain.DialogCompleted
	return nil
}

func (s *App) advanceDialogQuietEnd(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, text string) error {
	if p.Preferences.QuietHours == nil || !validClock(text) || text == p.Preferences.QuietHours.Start {
		return domain.ErrInvalidInput
	}
	p.Preferences.QuietHours.End = text
	if e := s.savePreferences(ctx, r, u, p.Preferences); e != nil {
		return e
	}
	d.Step = "settings"
	return nil
}

func (s *App) dialogOptionsTaskPreview(d domain.Dialog, p dialogDraft, v domain.DialogView) (domain.DialogView, error) {

	v.Options = enumOptions("confirm")
	if d.Step == "task_preview" {
		b, e := json.Marshal(p.Assignment)
		if e != nil {
			return v, e
		}
		v.Preview = string(b)
	}
	return v, nil
}

func (s *App) dialogOptionsSettings(p dialogDraft, v domain.DialogView) (domain.DialogView, error) {

	v.Options = enumOptions("toggle", "quiet", "quiet_off")
	b, e := json.Marshal(p.Preferences)
	if e != nil {
		return v, e
	}
	v.Preview = string(b)
	return v, nil
}

func (s *App) dialogOptionsAdminGroup(ctx context.Context, r Repositories, p dialogDraft, v domain.DialogView) (domain.DialogView, error) {

	g, e := r.Groups.ByID(ctx, p.GroupID)
	if e != nil {
		return v, e
	}
	b, e := json.Marshal(g)
	if e != nil {
		return v, e
	}
	v.Preview = string(b)
	v.Options = enumOptions("rename", "rotate", "digest_toggle", "members", "subjects", "add", "digest_preview", "audit")
	return v, nil
}

func (s *App) dialogOptionsMemberList(ctx context.Context, r Repositories, p dialogDraft, v domain.DialogView) (domain.DialogView, error) {

	members, e := r.Users.ListByGroup(ctx, p.GroupID, domain.PageRequest{Cursor: p.Cursor, Limit: 20})
	if e != nil {
		return v, e
	}
	for _, m := range members.Items {
		v.Options = append(v.Options, domain.Option{Label: fmt.Sprintf("#%d %s (%s)", m.ID, m.DisplayName, m.Role), Value: strconv.FormatInt(m.ID, 10)})
	}
	if members.NextCursor != "" {
		v.Options = append(v.Options, domain.Option{Label: "next", Value: "next"})
	}
	return v, nil
}

func (s *App) dialogOptionsAdminDigest(ctx context.Context, r Repositories, p dialogDraft, v domain.DialogView) (domain.DialogView, error) {

	digest, e := s.digest(ctx, r, p.GroupID, s.now())
	if e != nil {
		return v, e
	}
	for _, a := range digest.Assignments {
		v.Preview += fmt.Sprintf("#%d %s · %s\n", a.ID, a.Title, a.DeadlineAt.In(s.tz).Format("15:04"))
	}
	v.Options = enumOptions("back")
	return v, nil
}

func (s *App) dialogOptionsAdminAudit(ctx context.Context, r Repositories, p dialogDraft, v domain.DialogView) (domain.DialogView, error) {

	entries, e := r.Audit.List(ctx, p.GroupID, domain.PageRequest{Cursor: p.Cursor, Limit: 10})
	if e != nil {
		return v, e
	}
	b, e := json.Marshal(entries.Items)
	if e != nil {
		return v, e
	}
	v.Preview = string(b)
	v.Options = enumOptions("back")
	if entries.NextCursor != "" {
		v.Options = append(v.Options, domain.Option{Label: "next", Value: "next"})
	}
	return v, nil
}
