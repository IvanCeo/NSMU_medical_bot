package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func (s *App) advanceTask(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, in domain.DialogInput) error {
	text, choice := "", ""
	if in.Text != nil {
		text = *in.Text
	}
	if in.Selection != nil {
		choice = *in.Selection
	}
	a := &p.Assignment
	switch d.Step {
	case "task_id":
		id, e := parseID(strings.TrimSpace(text))
		if e != nil {
			return e
		}
		return s.loadTaskDraft(ctx, r, u, d, p, id)
	case "task_subject":
		return s.advanceTaskSubject(ctx, r, d, p, choice, a)
	case "task_kind":
		a.Kind = domain.AssignmentKind(choice)
		d.Step = "task_title"
	case "task_title":
		return s.advanceTaskTitle(d, text, choice, a)
	case "task_description":
		return s.advanceTaskDescription(d, text, choice, a)
	case "task_deadline":
		return s.advanceTaskDeadline(d, text, choice, a)
	case "task_importance":
		a.Importance = domain.Importance(choice)
		d.Step = "task_url"
	case "task_url":
		return s.advanceTaskUrl(d, text, choice, a)
	case "task_attachments":
		return s.advanceTaskAttachments(d, in, choice, a)
	case "task_preview":
		return s.advanceTaskPreview(ctx, r, u, d, p, choice, a)
	case "task_cancel_confirm":
		return s.advanceTaskCancelConfirm(ctx, r, u, d, p, choice)
	default:
		return domain.ErrInvalidInput
	}
	return nil
}

func (s *App) advanceTaskSubject(ctx context.Context, r Repositories, d *domain.Dialog, p *dialogDraft, choice string,
	a *domain.AssignmentInput) error {
	if choice == "next" {
		v, e := r.Subjects.List(ctx, p.GroupID, false, domain.PageRequest{Cursor: p.Cursor, Limit: 20})
		if e != nil {
			return e
		}
		p.Cursor = v.NextCursor
		return nil
	}
	if choice != "keep" {
		id, e := parseID(choice)
		if e != nil {
			return e
		}
		a.SubjectID = id
	}
	a.GroupID = p.GroupID
	p.Cursor = ""
	d.Step = "task_kind"
	return nil
}

func (s *App) advanceTaskAttachments(d *domain.Dialog, in domain.DialogInput, choice string,
	a *domain.AssignmentInput) error {
	switch choice {
	case "finish":
		d.Step = "task_preview"
	case "clear":
		a.Attachments = nil
	default:
		if in.Attachment == nil || len(a.Attachments) >= 10 {
			return domain.ErrInvalidInput
		}
		candidate := assignmentFromInput(*a)
		candidate.Attachments = append(candidate.Attachments, domain.Attachment{AttachmentInput: *in.Attachment})
		if e := validAssignment(candidate); e != nil {
			return e
		}
		a.Attachments = append(a.Attachments, *in.Attachment)
	}
	return nil
}

func (s *App) advanceTaskPreview(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, choice string,
	a *domain.AssignmentInput) error {
	if choice != "confirm" {
		return domain.ErrInvalidInput
	}
	var e error
	if d.Kind == domain.DialogEdit {
		patch := domain.AssignmentPatch{SubjectID: &a.SubjectID, Kind: &a.Kind, Title: &a.Title, Description: &a.Description, DeadlineAt: &a.DeadlineAt, Importance: &a.Importance, URL: &a.URL, Attachments: &a.Attachments}
		old, err := task(ctx, r, u, p.EntityID, true)
		if err != nil {
			return err
		}
		if old.DeadlineAt.Equal(a.DeadlineAt) {
			patch.DeadlineAt = nil
		}
		_, e = s.updateAssignment(ctx, r, u, p.EntityID, p.Version, patch)
	} else {
		_, e = s.createAssignment(ctx, r, u, *a)
	}
	if e != nil {
		return e
	}
	d.Status = domain.DialogCompleted
	return nil
}

func (s *App) advanceTaskTitle(d *domain.Dialog, text, choice string,
	a *domain.AssignmentInput) error {
	if choice != "keep" {
		text = strings.TrimSpace(text)
		if !validText(text, 200) {
			return domain.ErrInvalidInput
		}
		a.Title = text
	}
	d.Step = "task_description"
	return nil
}

func (s *App) advanceTaskDescription(d *domain.Dialog, text, choice string,
	a *domain.AssignmentInput) error {
	if choice != "keep" {
		if utf8.RuneCountInString(text) > 3000 {
			return domain.ErrInvalidInput
		}
		a.Description = text
		if text == "-" {
			a.Description = ""
		}
	}
	d.Step = "task_deadline"
	return nil
}

func (s *App) advanceTaskDeadline(d *domain.Dialog, text, choice string,
	a *domain.AssignmentInput) error {
	if choice != "keep" {
		at, e := ParseDeadline(text, s.tz)
		if e != nil || !at.After(s.now()) {
			return domain.ErrInvalidInput
		}
		a.DeadlineAt = at
	}
	d.Step = "task_importance"
	return nil
}

func (s *App) advanceTaskUrl(d *domain.Dialog, text, choice string,
	a *domain.AssignmentInput) error {
	if choice != "keep" {
		a.URL = strings.TrimSpace(text)
		if a.URL == "-" {
			a.URL = ""
		}
		candidate := assignmentFromInput(*a)
		if e := validAssignment(candidate); e != nil {
			return e
		}
	}
	d.Step = "task_attachments"
	return nil
}

func (s *App) advanceTaskCancelConfirm(ctx context.Context, r Repositories, u domain.User, d *domain.Dialog, p *dialogDraft, choice string,

) error {
	if choice != "confirm" {
		return domain.ErrInvalidInput
	}
	if e := s.cancelAssignment(ctx, r, u, p.EntityID, p.Version); e != nil {
		return e
	}
	d.Status = domain.DialogCompleted
	return nil
}
