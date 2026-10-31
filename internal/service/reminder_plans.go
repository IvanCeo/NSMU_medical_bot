package service

import (
	"context"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func (s *App) afterQuiet(at time.Time, q *domain.QuietHours) time.Time {
	if q == nil {
		return at
	}
	local := at.In(s.tz)
	clock := local.Format("15:04")
	overnight := q.Start > q.End
	inside := (clock >= q.Start && clock < q.End)
	if overnight {
		inside = clock >= q.Start || clock < q.End
	}
	if !inside {
		return at
	}
	end, _ := time.Parse("15:04", q.End)
	target := time.Date(local.Year(), local.Month(), local.Day(), end.Hour(), end.Minute(), 0, 0, s.tz)
	if overnight && clock >= q.Start {
		target = target.AddDate(0, 0, 1)
	}
	return target.UTC()
}
func (s *App) plans(ctx context.Context, r Repositories, u domain.User, a domain.Assignment) ([]domain.ReminderPlan, error) {
	if u.GroupID == nil || *u.GroupID != a.GroupID || !u.PrivateChatAvailable || a.Status != domain.Active {
		return nil, nil
	}
	p, e := r.Settings.Get(ctx, u.ID)
	if e != nil {
		return nil, e
	}
	if !p.RemindersEnabled {
		return nil, nil
	}
	done, e := r.Completions.IsDone(ctx, u.ID, a.ID)
	if e != nil || done {
		return nil, e
	}
	var out []domain.ReminderPlan
	for _, offset := range s.offsets {
		at := a.DeadlineAt.Add(-offset)
		if !at.After(s.now()) {
			continue
		}
		at = s.afterQuiet(at, p.QuietHours)
		if at.Before(a.DeadlineAt) {
			d := offset
			out = append(out, domain.ReminderPlan{AssignmentID: a.ID, UserID: u.ID, AssignmentVersion: a.Version, Source: domain.Standard, Offset: &d, SendAt: at})
		}
	}
	return out, nil
}
func (s *App) scheduleForUser(ctx context.Context, r Repositories, u domain.User, a domain.Assignment) error {
	p, e := s.plans(ctx, r, u, a)
	if e != nil {
		return e
	}
	return r.Reminders.Schedule(ctx, p)
}
func (s *App) scheduleUser(ctx context.Context, r Repositories, u domain.User) error {
	if u.GroupID == nil {
		return nil
	}
	p := domain.PageRequest{Limit: 20}
	for {
		list, e := r.Assignments.List(ctx, domain.AssignmentFilter{GroupID: *u.GroupID, DoneFilter: "pending"}, u.ID, p)
		if e != nil {
			return e
		}
		for _, a := range list.Items {
			if e := s.scheduleForUser(ctx, r, u, a.Assignment); e != nil {
				return e
			}
		}
		if list.NextCursor == "" {
			return nil
		}
		p.Cursor = list.NextCursor
	}
}
func (s *App) replanAssignment(ctx context.Context, r Repositories, a domain.Assignment) error {
	var plans []domain.ReminderPlan
	p := domain.PageRequest{Limit: 20}
	for {
		list, e := r.Users.ListByGroup(ctx, a.GroupID, p)
		if e != nil {
			return e
		}
		for _, u := range list.Items {
			v, e := s.plans(ctx, r, u, a)
			if e != nil {
				return e
			}
			plans = append(plans, v...)
		}
		if list.NextCursor == "" {
			break
		}
		p.Cursor = list.NextCursor
	}
	if e := r.Deliveries.CancelRemindersForAssignment(ctx, a.ID, s.now()); e != nil {
		return e
	}
	return r.Reminders.ReplaceForAssignment(ctx, a.ID, plans, s.now())
}
func (s *App) digest(ctx context.Context, r Repositories, gid int64, date time.Time) (domain.Digest, error) {
	g, e := r.Groups.ByID(ctx, gid)
	if e != nil {
		return domain.Digest{}, e
	}
	d := domain.Digest{Group: g, Date: date.In(s.tz)}
	start := time.Date(d.Date.Year(), d.Date.Month(), d.Date.Day(), 0, 0, 0, 0, s.tz)
	end := start.AddDate(0, 0, 1)
	p := domain.PageRequest{Limit: 20}
	for {
		v, e := r.Assignments.List(ctx, domain.AssignmentFilter{GroupID: gid, From: &start, Until: &end}, 0, p)
		if e != nil {
			return d, e
		}
		for _, a := range v.Items {
			d.Assignments = append(d.Assignments, a.Assignment)
		}
		if v.NextCursor == "" {
			return d, nil
		}
		p.Cursor = v.NextCursor
	}
}
