package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func (s *App) ExpireDialogs(ctx context.Context, now time.Time) error {
	return s.work.WithinTx(ctx, func(r Repositories) error { return r.Dialogs.Expire(ctx, now) })
}
func (s *App) Plan(ctx context.Context, now time.Time) error {
	return s.work.WithinTx(ctx, func(r Repositories) error {
		if e := r.Deliveries.Expire(ctx, now); e != nil {
			return e
		}
		due, e := r.Reminders.Due(ctx, now, 1000)
		if e != nil {
			return e
		}
		for _, rem := range due {
			a, e := r.Assignments.ByID(ctx, rem.AssignmentID)
			if e != nil {
				return e
			}
			if !a.DeadlineAt.After(now) {
				if e := r.Reminders.Cancel(ctx, rem.ID, now); e != nil {
					return e
				}
				continue
			}
			d := domain.DeliveryDraft{EventKey: fmt.Sprintf("reminder:%d", rem.ID), Kind: domain.ReminderDelivery, ReminderID: &rem.ID, GroupID: a.GroupID, UserID: &rem.UserID, Payload: domain.DeliveryPayload{AssignmentIDs: []int64{a.ID}, Version: rem.AssignmentVersion}, NextAttemptAt: now, ExpiresAt: a.DeadlineAt}
			if e := r.Deliveries.Enqueue(ctx, d); e != nil {
				return e
			}
		}
		local := now.In(s.tz)
		if local.Format("15:04") < s.digestTime {
			return nil
		}
		p := domain.PageRequest{Limit: 20}
		for {
			gs, e := r.Groups.List(ctx, p)
			if e != nil {
				return e
			}
			for _, g := range gs.Items {
				if g.DigestEnabled && g.TelegramChatID != nil {
					if e := s.planDigest(ctx, r, g, now); e != nil {
						return e
					}
				}
			}
			if gs.NextCursor == "" {
				return nil
			}
			p.Cursor = gs.NextCursor
		}
	})
}
func (s *App) planDigest(ctx context.Context, r Repositories, g domain.Group, now time.Time) error {
	date := now.In(s.tz).Format(time.DateOnly)
	run, created, e := r.Digests.Ensure(ctx, g.ID, date, g.ChatBindingVersion, now)
	if e != nil || !created {
		return e
	}
	d, e := s.digest(ctx, r, g.ID, now)
	if e != nil {
		return e
	}
	if len(d.Assignments) == 0 {
		return r.Digests.SetStatus(ctx, run.ID, domain.DigestEmpty)
	}
	end := time.Date(d.Date.Year(), d.Date.Month(), d.Date.Day(), 0, 0, 0, 0, s.tz).AddDate(0, 0, 1)
	for start := 0; start < len(d.Assignments); start += 8 {
		stop := min(start+8, len(d.Assignments))
		ids := make([]int64, 0, stop-start)
		for _, a := range d.Assignments[start:stop] {
			ids = append(ids, a.ID)
		}
		part := start / 8
		draft := domain.DeliveryDraft{EventKey: fmt.Sprintf("digest:%d:%d", run.ID, part), Kind: domain.DigestDelivery, DigestRunID: &run.ID, GroupID: g.ID, Part: part, Payload: domain.DeliveryPayload{AssignmentIDs: ids, BindingVersion: g.ChatBindingVersion, LocalDate: date}, NextAttemptAt: now, ExpiresAt: end}
		if e := r.Deliveries.Enqueue(ctx, draft); e != nil {
			return e
		}
	}
	return r.Digests.SetStatus(ctx, run.ID, domain.DigestQueued)
}
func (s *App) DispatchOne(ctx context.Context, now time.Time) (bool, error) {
	token, e := s.token()
	if e != nil {
		return false, e
	}
	var d *domain.Delivery
	var message *domain.Outgoing
	e = s.work.WithinTx(ctx, func(r Repositories) error {
		if e := r.Deliveries.Expire(ctx, now); e != nil {
			return e
		}
		var e error
		d, e = r.Deliveries.Claim(ctx, now, now.Add(2*time.Minute), token)
		if e != nil || d == nil {
			return e
		}
		message, e = s.outgoing(ctx, r, *d, now)
		if errors.Is(e, errDeferred) {
			return nil
		}
		if e != nil {
			return e
		}
		if message == nil {
			return s.finishDelivery(ctx, r, *d, domain.Skipped, "obsolete", now)
		}
		return nil
	})
	if e != nil || d == nil {
		return false, e
	}
	if message == nil {
		return true, nil
	}
	receipt, sendErr := s.sender.SendNotification(ctx, *message)
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	e = s.work.WithinTx(ctx, func(r Repositories) error {
		at := s.now()
		if sendErr != nil {
			return s.deliveryFailed(ctx, r, *d, sendErr, at)
		}
		if e := r.Deliveries.Complete(ctx, d.ID, d.LeaseToken, receipt, at); e != nil {
			return e
		}
		if d.ReminderID != nil {
			if e := r.Reminders.MarkSent(ctx, *d.ReminderID, at); e != nil {
				return e
			}
		}
		return digestProgress(ctx, r, d.DigestRunID)
	})
	if errors.Is(e, domain.ErrConflict) {
		return true, nil
	}
	return true, e
}
func (s *App) outgoing(ctx context.Context, r Repositories, d domain.Delivery, now time.Time) (*domain.Outgoing, error) {
	if d.Attempts > 5 {
		return nil, nil
	}
	g, e := r.Groups.ByID(ctx, d.GroupID)
	if e != nil {
		return nil, e
	}
	if d.Kind == domain.ReminderDelivery {
		return s.reminderMessage(ctx, r, d, g, now)
	}
	if d.Kind == domain.AssignmentCreatedDelivery {
		return s.assignmentCreatedMessage(ctx, r, d, now)
	}
	if d.Kind != domain.DigestDelivery {
		return nil, nil
	}
	if !g.DigestEnabled || g.TelegramChatID == nil || g.ChatBindingVersion != d.Payload.BindingVersion || now.In(s.tz).Format(time.DateOnly) != d.Payload.LocalDate {
		return nil, nil
	}
	msg := &domain.Outgoing{ChatID: *g.TelegramChatID, Kind: "group", Text: d.Payload.LocalDate + " · " + g.Title + "\n"}
	for _, id := range d.Payload.AssignmentIDs {
		a, e := r.Assignments.ByID(ctx, id)
		if e != nil {
			return nil, e
		}
		if a.GroupID != g.ID || a.Status != domain.Active || a.DeadlineAt.In(s.tz).Format(time.DateOnly) != d.Payload.LocalDate {
			continue
		}
		msg.Text += fmt.Sprintf("\n%s · #%d %s", a.DeadlineAt.In(s.tz).Format("15:04"), a.ID, a.Title)
		msg.Buttons = append(msg.Buttons, domain.OutgoingButton{Label: a.Title, AssignmentID: id})
	}
	if len(msg.Buttons) == 0 {
		return nil, nil
	}
	return msg, nil
}
func (s *App) reminderMessage(ctx context.Context, r Repositories, d domain.Delivery, g domain.Group, now time.Time) (*domain.Outgoing, error) {
	rem, e := r.Reminders.ByID(ctx, *d.ReminderID)
	if e != nil {
		return nil, e
	}
	if rem.CancelledAt != nil || rem.SentAt != nil {
		return nil, nil
	}
	a, e := r.Assignments.ByID(ctx, rem.AssignmentID)
	if e != nil {
		return nil, e
	}
	u, e := r.Users.ByID(ctx, rem.UserID)
	if e != nil {
		return nil, e
	}
	if u.GroupID == nil || *u.GroupID != g.ID || !u.PrivateChatAvailable || a.Status != domain.Active || a.Version != rem.AssignmentVersion || !a.DeadlineAt.After(now) {
		return nil, nil
	}
	p, e := r.Settings.Get(ctx, u.ID)
	if e != nil {
		return nil, e
	}
	done, e := r.Completions.IsDone(ctx, u.ID, a.ID)
	if e != nil {
		return nil, e
	}
	if done || !p.RemindersEnabled {
		return nil, nil
	}
	if rem.Source == domain.Standard {
		due, e := r.Reminders.Due(ctx, now, 1000000)
		if e != nil {
			return nil, e
		}
		found := false
		for _, v := range due {
			if v.ID == rem.ID {
				found = true
				break
			}
		}
		if !found {
			return nil, nil
		}
	}
	next := s.afterQuiet(now, p.QuietHours)
	if next.After(now) {
		if next.Before(a.DeadlineAt) {
			if e := r.Reminders.Reschedule(ctx, rem.ID, next); e != nil {
				return nil, e
			}
			failure := domain.DeliveryFailure{Code: "quiet_hours"}
			if e := r.Deliveries.Retry(ctx, d.ID, d.LeaseToken, failure, next); e != nil {
				return nil, e
			}
			return nil, errDeferred
		}
		return nil, nil
	}
	msg := &domain.Outgoing{ChatID: u.TelegramID, Kind: "private", EventKind: d.Kind, Text: fmt.Sprintf("%s\n#%d %s\n%s", g.Title, a.ID, a.Title, a.DeadlineAt.In(s.tz).Format("02.01.2006 15:04")), Buttons: []domain.OutgoingButton{{Label: a.Title, AssignmentID: a.ID}}}
	return msg, nil
}

var errDeferred = errors.New("delivery deferred")

func (s *App) finishDelivery(ctx context.Context, r Repositories, d domain.Delivery, status domain.DeliveryStatus, code string, at time.Time) error {
	if e := r.Deliveries.Fail(ctx, d.ID, d.LeaseToken, status, code, at); e != nil {
		return e
	}
	if d.ReminderID != nil {
		if e := r.Reminders.Cancel(ctx, *d.ReminderID, at); e != nil {
			return e
		}
	}
	return digestProgress(ctx, r, d.DigestRunID)
}
func (s *App) deliveryFailed(ctx context.Context, r Repositories, d domain.Delivery, err error, at time.Time) error {
	if d.LeaseUntil == nil || !d.LeaseUntil.After(at) {
		return domain.ErrConflict
	}
	f := domain.DeliveryFailure{Code: "unavailable"}
	var detail *domain.DeliveryFailure
	if errors.As(err, &detail) {
		f = *detail
	}
	if f.Code == "403" && d.UserID != nil {
		if e := s.finishDelivery(ctx, r, d, domain.Failed, f.Code, at); e != nil {
			return e
		}
		if e := r.Users.SetPrivateChatAvailable(ctx, *d.UserID, false); e != nil {
			return e
		}
		return s.cancelUser(ctx, r, *d.UserID, nil)
	}
	if f.Permanent || d.Attempts >= 5 {
		return s.finishDelivery(ctx, r, d, domain.Failed, f.Code, at)
	}
	delay := max(15*time.Second*time.Duration(1<<uint(min(d.Attempts-1, 3))), f.RetryAfter)
	next := at.Add(delay)
	if !next.Before(d.ExpiresAt) {
		return s.finishDelivery(ctx, r, d, domain.Skipped, "expired", at)
	}
	return r.Deliveries.Retry(ctx, d.ID, d.LeaseToken, f, next)
}
func digestProgress(ctx context.Context, r Repositories, id *int64) error {
	if id == nil {
		return nil
	}
	p, e := r.Deliveries.Progress(ctx, *id)
	if e != nil {
		return e
	}
	if p.Pending > 0 {
		return nil
	}
	status := domain.DigestSent
	switch {
	case p.Sent > 0 && p.Sent < p.Total:
		status = domain.DigestPartial
	case p.Failed > 0:
		status = domain.DigestFailed
	case p.Cancelled > 0:
		status = domain.DigestCancelled
	case p.Sent == 0:
		status = domain.DigestEmpty
	}
	return r.Digests.SetStatus(ctx, *id, status)
}
func ParseDeadline(value string, location *time.Location) (time.Time, error) {
	const layout = "02.01.2006 15:04"
	t, e := time.ParseInLocation(layout, strings.TrimSpace(value), location)
	if e != nil || t.Format(layout) != strings.TrimSpace(value) {
		return time.Time{}, domain.ErrInvalidInput
	}
	// Reject both occurrences of an ambiguous wall clock time, including half-hour DST shifts.
	for delta := -3 * time.Hour; delta <= 3*time.Hour; delta += 15 * time.Minute {
		if delta != 0 && t.Add(delta).In(location).Format(layout) == t.Format(layout) {
			return time.Time{}, domain.ErrInvalidInput
		}
	}
	return t.UTC(), nil
}
func parseID(value string) (int64, error) {
	id, e := strconv.ParseInt(value, 10, 64)
	if e != nil || id <= 0 {
		return 0, domain.ErrInvalidInput
	}
	return id, nil
}
