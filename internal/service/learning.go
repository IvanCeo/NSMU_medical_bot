package service

import (
	"context"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func (s *App) Subjects(ctx context.Context, id, gid int64, archived bool, p domain.PageRequest) (domain.SubjectPage, error) {
	var out domain.SubjectPage
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		var e error
		gid, e = groupID(u, gid)
		if e != nil {
			return e
		}
		if archived {
			if e := access(u, gid, true); e != nil {
				return e
			}
		}
		out, e = r.Subjects.List(ctx, gid, archived, p)
		return e
	})
	return out, e
}
func (s *App) createSubject(ctx context.Context, r Repositories, u domain.User, in domain.SubjectInput) (domain.Subject, error) {
	var a domain.Subject
	gid, e := groupID(u, in.GroupID)
	if e != nil {
		return a, e
	}
	if e := access(u, gid, true); e != nil {
		return a, e
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Teacher = strings.TrimSpace(in.Teacher)
	if !validText(in.Name, 200) || utf8.RuneCountInString(in.Teacher) > 200 {
		return a, domain.ErrInvalidInput
	}
	a, e = r.Subjects.Create(ctx, domain.Subject{GroupID: gid, Name: in.Name, Teacher: in.Teacher, CreatedAt: s.now(), UpdatedAt: s.now()})
	if e != nil {
		return a, e
	}
	return a, s.log(ctx, r, u, gid, "create", "subject", a.ID)
}
func (s *App) CreateSubject(ctx context.Context, id int64, in domain.SubjectInput) (domain.Subject, error) {
	var out domain.Subject
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		var e error
		out, e = s.createSubject(ctx, r, u, in)
		return e
	})
	return out, e
}
func (s *App) updateSubject(ctx context.Context, r Repositories, u domain.User, id, v int64, p domain.SubjectPatch) (domain.Subject, error) {
	a, e := r.Subjects.ByID(ctx, id)
	if e != nil {
		return a, e
	}
	if e := access(u, a.GroupID, true); e != nil {
		return a, e
	}
	if a.Version != v {
		return a, domain.ErrConflict
	}
	if p.Name != nil {
		a.Name = strings.TrimSpace(*p.Name)
	}
	if p.Teacher != nil {
		a.Teacher = strings.TrimSpace(*p.Teacher)
	}
	if !validText(a.Name, 200) || utf8.RuneCountInString(a.Teacher) > 200 {
		return a, domain.ErrInvalidInput
	}
	if p.Archived != nil {
		a.ArchivedAt = nil
		if *p.Archived {
			at := s.now()
			a.ArchivedAt = &at
		}
	}
	a.UpdatedAt = s.now()
	if e := r.Subjects.Update(ctx, a, v); e != nil {
		return a, e
	}
	a.Version++
	return a, s.log(ctx, r, u, a.GroupID, "update", "subject", id)
}
func (s *App) UpdateSubject(ctx context.Context, id, sid, v int64, p domain.SubjectPatch) (domain.Subject, error) {
	var out domain.Subject
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		var e error
		out, e = s.updateSubject(ctx, r, u, sid, v, p)
		return e
	})
	return out, e
}
func (s *App) resolveFilter(u domain.User, f domain.AssignmentFilter) (domain.AssignmentFilter, error) {
	var e error
	f.GroupID, e = groupID(u, f.GroupID)
	if e != nil {
		return f, e
	}
	f.From = nil
	f.Until = nil
	f.Before = nil
	now := s.now().In(s.tz)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.tz)
	switch f.Period {
	case "", "all":
	case "day":
		end := start.AddDate(0, 0, 1)
		f.From = &start
		f.Until = &end
	case "week":
		end := start.AddDate(0, 0, 7)
		f.From = &start
		f.Until = &end
	case "overdue":
		f.Before = &now
	default:
		return f, domain.ErrInvalidInput
	}
	switch f.DoneFilter {
	case "", "all", "pending", "done":
	default:
		return f, domain.ErrInvalidInput
	}
	return f, nil
}
func (s *App) Tasks(ctx context.Context, id int64, f domain.AssignmentFilter, p domain.PageRequest) (domain.AssignmentPage, error) {
	var out domain.AssignmentPage
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		if f.GroupID == 0 && f.SubjectID != nil {
			subject, e := r.Subjects.ByID(ctx, *f.SubjectID)
			if e != nil {
				return e
			}
			f.GroupID = subject.GroupID
		}
		f, e := s.resolveFilter(u, f)
		if e != nil {
			return e
		}
		if f.SubjectID != nil {
			a, e := r.Subjects.ByID(ctx, *f.SubjectID)
			if e != nil {
				return e
			}
			if a.GroupID != f.GroupID {
				return domain.ErrForbidden
			}
		}
		out, e = r.Assignments.List(ctx, f, u.ID, p)
		return e
	})
	return out, e
}
func task(ctx context.Context, r Repositories, u domain.User, id int64, write bool) (domain.Assignment, error) {
	a, e := r.Assignments.ByID(ctx, id)
	if e != nil {
		return a, e
	}
	return a, access(u, a.GroupID, write)
}
func (s *App) Task(ctx context.Context, id, aid int64) (domain.AssignmentView, error) {
	var out domain.AssignmentView
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		a, e := task(ctx, r, u, aid, false)
		if e != nil {
			return e
		}
		done, e := r.Completions.IsDone(ctx, u.ID, aid)
		out = domain.AssignmentView{Assignment: a, Done: done}
		return e
	})
	return out, e
}
func validAssignment(a domain.Assignment) error {
	if !validText(a.Title, 200) || utf8.RuneCountInString(a.Description) > 3000 || len(a.URL) > 2048 || len(a.Attachments) > 10 {
		return domain.ErrInvalidInput
	}
	switch a.Kind {
	case domain.Homework, domain.Test, domain.Exam, domain.Practice, domain.Other:
	default:
		return domain.ErrInvalidInput
	}
	switch a.Importance {
	case domain.Low, domain.Normal, domain.High:
	default:
		return domain.ErrInvalidInput
	}
	if a.URL != "" {
		u, e := url.Parse(a.URL)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return domain.ErrInvalidInput
		}
	}
	for _, f := range a.Attachments {
		if (f.Kind != domain.Document && f.Kind != domain.Photo) || f.FileID == "" || len(f.FileID) > 512 || len(f.FileName) > 512 || len(f.MIME) > 200 {
			return domain.ErrInvalidInput
		}
	}
	return nil
}
func validateSubject(ctx context.Context, r Repositories, a domain.Assignment) error {
	s, e := r.Subjects.ByID(ctx, a.SubjectID)
	if e != nil {
		return e
	}
	if s.GroupID != a.GroupID {
		return domain.ErrForbidden
	}
	if s.ArchivedAt != nil {
		return domain.ErrInvalidInput
	}
	return nil
}
func assignmentFromInput(in domain.AssignmentInput) domain.Assignment {
	a := domain.Assignment{GroupID: in.GroupID, SubjectID: in.SubjectID, Kind: in.Kind, Title: strings.TrimSpace(in.Title), Description: in.Description, DeadlineAt: in.DeadlineAt.UTC(), Importance: in.Importance, URL: strings.TrimSpace(in.URL), Status: domain.Active}
	if a.Importance == "" {
		a.Importance = domain.Normal
	}
	for _, f := range in.Attachments {
		a.Attachments = append(a.Attachments, domain.Attachment{AttachmentInput: f})
	}
	return a
}
func (s *App) createAssignment(ctx context.Context, r Repositories, u domain.User, in domain.AssignmentInput) (domain.Assignment, error) {
	a := assignmentFromInput(in)
	gid, e := groupID(u, a.GroupID)
	if e != nil {
		return a, e
	}
	a.GroupID = gid
	if e := access(u, gid, true); e != nil {
		return a, e
	}
	if !a.DeadlineAt.After(s.now()) {
		return a, domain.ErrInvalidInput
	}
	if e := validAssignment(a); e != nil {
		return a, e
	}
	if e := validateSubject(ctx, r, a); e != nil {
		return a, e
	}
	a.CreatedBy = u.ID
	a.CreatedAt = s.now()
	a.UpdatedAt = s.now()
	a, e = r.Assignments.Create(ctx, a)
	if e != nil {
		return a, e
	}
	if e := s.replanAssignment(ctx, r, a); e != nil {
		return a, e
	}
	return a, s.log(ctx, r, u, gid, "create", "assignment", a.ID)
}
func (s *App) CreateAssignment(ctx context.Context, id int64, in domain.AssignmentInput) (domain.Assignment, error) {
	var out domain.Assignment
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		var e error
		out, e = s.createAssignment(ctx, r, u, in)
		return e
	})
	return out, e
}
func applyPatch(a *domain.Assignment, p domain.AssignmentPatch) {
	if p.SubjectID != nil {
		a.SubjectID = *p.SubjectID
	}
	if p.Kind != nil {
		a.Kind = *p.Kind
	}
	if p.Title != nil {
		a.Title = strings.TrimSpace(*p.Title)
	}
	if p.Description != nil {
		a.Description = *p.Description
	}
	if p.DeadlineAt != nil {
		a.DeadlineAt = p.DeadlineAt.UTC()
	}
	if p.Importance != nil {
		a.Importance = *p.Importance
	}
	if p.URL != nil {
		a.URL = strings.TrimSpace(*p.URL)
	}
	if p.Attachments != nil {
		a.Attachments = nil
		for _, f := range *p.Attachments {
			a.Attachments = append(a.Attachments, domain.Attachment{AttachmentInput: f})
		}
	}
}
func (s *App) updateAssignment(ctx context.Context, r Repositories, u domain.User, id, v int64, p domain.AssignmentPatch) (domain.Assignment, error) {
	a, e := task(ctx, r, u, id, true)
	if e != nil {
		return a, e
	}
	if a.Version != v {
		return a, domain.ErrConflict
	}
	if a.Status != domain.Active {
		return a, domain.ErrConflict
	}
	oldSubject := a.SubjectID
	applyPatch(&a, p)
	if p.DeadlineAt != nil && !a.DeadlineAt.After(s.now()) {
		return a, domain.ErrInvalidInput
	}
	if e := validAssignment(a); e != nil {
		return a, e
	}
	if oldSubject != a.SubjectID {
		if e := validateSubject(ctx, r, a); e != nil {
			return a, e
		}
	}
	a.UpdatedAt = s.now()
	if e := r.Assignments.Update(ctx, a, v); e != nil {
		return a, e
	}
	a.Version++
	if e := s.replanAssignment(ctx, r, a); e != nil {
		return a, e
	}
	return a, s.log(ctx, r, u, a.GroupID, "update", "assignment", id)
}
func (s *App) UpdateAssignment(ctx context.Context, id, aid, v int64, p domain.AssignmentPatch) (domain.Assignment, error) {
	var out domain.Assignment
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		var e error
		out, e = s.updateAssignment(ctx, r, u, aid, v, p)
		return e
	})
	return out, e
}
func (s *App) cancelAssignment(ctx context.Context, r Repositories, u domain.User, id, v int64) error {
	a, e := task(ctx, r, u, id, true)
	if e != nil {
		return e
	}
	if a.Version != v {
		return domain.ErrConflict
	}
	if a.Status == domain.Cancelled {
		return nil
	}
	a.Status = domain.Cancelled
	a.UpdatedAt = s.now()
	if e := r.Assignments.Update(ctx, a, v); e != nil {
		return e
	}
	if e := r.Reminders.ReplaceForAssignment(ctx, id, nil, s.now()); e != nil {
		return e
	}
	if e := r.Deliveries.CancelForAssignment(ctx, id, s.now()); e != nil {
		return e
	}
	return s.log(ctx, r, u, a.GroupID, "cancel", "assignment", id)
}
func (s *App) CancelAssignment(ctx context.Context, id, aid, v int64) error {
	return s.actor(ctx, id, func(r Repositories, u domain.User) error { return s.cancelAssignment(ctx, r, u, aid, v) })
}
func (s *App) SetComplete(ctx context.Context, id, aid, v int64, done bool) (domain.AssignmentView, error) {
	var out domain.AssignmentView
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		a, e := task(ctx, r, u, aid, false)
		if e != nil {
			return e
		}
		if a.Version != v || a.Status != domain.Active {
			return domain.ErrConflict
		}
		if u.GroupID == nil || *u.GroupID != a.GroupID {
			return domain.ErrNotInGroup
		}
		var at *time.Time
		if done {
			t := s.now()
			at = &t
		}
		if e := r.Completions.Set(ctx, u.ID, aid, at); e != nil {
			return e
		}
		if e := s.cancelUser(ctx, r, u.ID, &aid); e != nil {
			return e
		}
		if !done {
			if e := s.scheduleForUser(ctx, r, u, a); e != nil {
				return e
			}
		}
		out = domain.AssignmentView{Assignment: a, Done: done}
		return s.log(ctx, r, u, a.GroupID, "completion", "assignment", aid)
	})
	return out, e
}
func (s *App) Snooze(ctx context.Context, id, aid, v int64, delay time.Duration) error {
	if delay != 3*time.Hour {
		return domain.ErrInvalidInput
	}
	return s.actor(ctx, id, func(r Repositories, u domain.User) error {
		a, e := task(ctx, r, u, aid, false)
		if e != nil {
			return e
		}
		if a.Version != v || a.Status != domain.Active {
			return domain.ErrConflict
		}
		if u.GroupID == nil || *u.GroupID != a.GroupID || !u.PrivateChatAvailable {
			return domain.ErrNotInGroup
		}
		p, e := r.Settings.Get(ctx, u.ID)
		if e != nil {
			return e
		}
		done, e := r.Completions.IsDone(ctx, u.ID, aid)
		if e != nil {
			return e
		}
		if done || !p.RemindersEnabled {
			return domain.ErrInvalidInput
		}
		at := s.afterQuiet(s.now().Add(delay), p.QuietHours)
		if !at.Before(a.DeadlineAt) {
			return domain.ErrInvalidInput
		}
		return r.Reminders.Snooze(ctx, domain.ReminderPlan{AssignmentID: aid, UserID: u.ID, AssignmentVersion: v, Source: domain.Snoozed, SendAt: at}, s.now())
	})
}
func (s *App) PreviewDigest(ctx context.Context, id, gid int64, date time.Time) (domain.Digest, error) {
	var out domain.Digest
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		if e := requireAdmin(u); e != nil {
			return e
		}
		var e error
		out, e = s.digest(ctx, r, gid, date)
		return e
	})
	return out, e
}
func (s *App) Audit(ctx context.Context, id, gid int64, p domain.PageRequest) (domain.AuditPage, error) {
	var out domain.AuditPage
	e := s.actor(ctx, id, func(r Repositories, u domain.User) error {
		if e := requireAdmin(u); e != nil {
			return e
		}
		var e error
		out, e = r.Audit.List(ctx, gid, p)
		return e
	})
	return out, e
}
