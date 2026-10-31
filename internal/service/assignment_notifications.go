package service

import (
	"context"
	"fmt"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func (s *App) planAssignmentCreated(ctx context.Context, r Repositories, a domain.Assignment) error {
	p := domain.PageRequest{Limit: 20}
	for {
		members, e := r.Users.ListByGroup(ctx, a.GroupID, p)
		if e != nil {
			return e
		}
		for _, u := range members.Items {
			if e := s.enqueueAssignmentCreated(ctx, r, a, u); e != nil {
				return e
			}
		}
		if members.NextCursor == "" {
			return nil
		}
		p.Cursor = members.NextCursor
	}
}

func (s *App) enqueueAssignmentCreated(ctx context.Context, r Repositories, a domain.Assignment, u domain.User) error {
	if !u.PrivateChatAvailable {
		return nil
	}
	p, e := r.Settings.Get(ctx, u.ID)
	if e != nil || !p.RemindersEnabled {
		return e
	}
	at := s.afterQuiet(s.now(), p.QuietHours)
	if !at.Before(a.DeadlineAt) {
		return nil
	}
	return r.Deliveries.Enqueue(ctx, domain.DeliveryDraft{
		EventKey: fmt.Sprintf("assignment_created:%d:%d", a.ID, u.ID),
		Kind:     domain.AssignmentCreatedDelivery, AssignmentID: &a.ID,
		GroupID: a.GroupID, UserID: &u.ID,
		Payload:       domain.DeliveryPayload{AssignmentIDs: []int64{a.ID}},
		NextAttemptAt: at, ExpiresAt: a.DeadlineAt,
	})
}

func (s *App) assignmentCreatedMessage(ctx context.Context, r Repositories, d domain.Delivery, now time.Time) (*domain.Outgoing, error) {
	a, e := r.Assignments.ByID(ctx, *d.AssignmentID)
	if e != nil {
		return nil, e
	}
	u, e := r.Users.ByID(ctx, *d.UserID)
	if e != nil {
		return nil, e
	}
	if u.GroupID == nil || *u.GroupID != d.GroupID || a.GroupID != d.GroupID || !u.PrivateChatAvailable || a.Status != domain.Active || !a.DeadlineAt.After(now) {
		return nil, nil
	}
	p, e := r.Settings.Get(ctx, u.ID)
	if e != nil || !p.RemindersEnabled {
		return nil, e
	}
	done, e := r.Completions.IsDone(ctx, u.ID, a.ID)
	if e != nil || done {
		return nil, e
	}
	next := s.afterQuiet(now, p.QuietHours)
	if next.After(now) {
		if !next.Before(a.DeadlineAt) || !next.Before(d.ExpiresAt) {
			return nil, nil
		}
		if e := r.Deliveries.Retry(ctx, d.ID, d.LeaseToken, domain.DeliveryFailure{Code: "quiet_hours"}, next); e != nil {
			return nil, e
		}
		return nil, errDeferred
	}
	subject, e := r.Subjects.ByID(ctx, a.SubjectID)
	if e != nil {
		return nil, e
	}
	return &domain.Outgoing{ChatID: u.TelegramID, Kind: "private", EventKind: d.Kind,
		Text:    fmt.Sprintf("%s\n#%d %s\nСрок: %s", subject.Name, a.ID, a.Title, a.DeadlineAt.In(s.tz).Format("02.01.2006 15:04")),
		Buttons: []domain.OutgoingButton{{Label: a.Title, AssignmentID: a.ID}}}, nil
}
