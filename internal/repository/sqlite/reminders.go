package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

type reminders struct{ store }

func (r reminders) Schedule(ctx context.Context, plans []domain.ReminderPlan) error {
	for _, p := range plans {
		var offset any
		if p.Offset != nil {
			offset = p.Offset.Seconds()
		}
		if e := exec(ctx, r.q, "INSERT INTO reminders(assignment_id,user_id,assignment_version,source,offset_seconds,send_at) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING", p.AssignmentID, p.UserID, p.AssignmentVersion, p.Source, offset, stamp(p.SendAt)); e != nil {
			return e
		}
	}
	return nil
}
func (r reminders) ReplaceForAssignment(ctx context.Context, id int64, plans []domain.ReminderPlan, at time.Time) error {
	if e := exec(ctx, r.q, "UPDATE reminders SET cancelled_at=? WHERE assignment_id=? AND sent_at IS NULL AND cancelled_at IS NULL", stamp(at), id); e != nil {
		return e
	}
	return r.Schedule(ctx, plans)
}
func (r reminders) CancelForUser(ctx context.Context, uid int64, aid *int64, at time.Time) error {
	return exec(ctx, r.q, "UPDATE reminders SET cancelled_at=? WHERE user_id=? AND (? IS NULL OR assignment_id=?) AND sent_at IS NULL AND cancelled_at IS NULL", stamp(at), uid, aid, aid)
}
func (r reminders) Snooze(ctx context.Context, p domain.ReminderPlan, at time.Time) error {
	e := exec(ctx, r.q, "UPDATE reminders SET cancelled_at=? WHERE user_id=? AND assignment_id=? AND send_at<=? AND sent_at IS NULL AND cancelled_at IS NULL", stamp(at), p.UserID, p.AssignmentID, stamp(p.SendAt))
	if e != nil {
		return e
	}
	return r.Schedule(ctx, []domain.ReminderPlan{p})
}

const reminderColumns = "id,assignment_id,user_id,assignment_version,source,offset_seconds,send_at,sent_at,cancelled_at"

func scanReminder(row scanner) (domain.Reminder, error) {
	var r domain.Reminder
	var offset sql.NullFloat64
	var send string
	var sent, cancelled sql.NullString
	e := row.Scan(&r.ID, &r.AssignmentID, &r.UserID, &r.AssignmentVersion, &r.Source, &offset, &send, &sent, &cancelled)
	if offset.Valid {
		d := time.Duration(offset.Float64 * float64(time.Second))
		r.Offset = &d
	}
	r.SendAt = parse(send)
	r.SentAt = nullableTime(sent)
	r.CancelledAt = nullableTime(cancelled)
	return r, repoError(e)
}
func (r reminders) Due(ctx context.Context, now time.Time, limit int) (out []domain.Reminder, err error) {
	// Select the latest overdue standard interval across the whole backlog, before LIMIT.
	rows, e := r.q.QueryContext(ctx, `SELECT `+reminderColumns+` FROM reminders r WHERE sent_at IS NULL AND cancelled_at IS NULL AND send_at<=? AND (source<>'default' OR NOT EXISTS(SELECT 1 FROM reminders later WHERE later.user_id=r.user_id AND later.assignment_id=r.assignment_id AND later.assignment_version=r.assignment_version AND later.source='default' AND later.cancelled_at IS NULL AND later.send_at<=? AND (later.send_at>r.send_at OR (later.send_at=r.send_at AND later.id>r.id)))) ORDER BY send_at,id LIMIT ?`, stamp(now), stamp(now), limit)
	if e != nil {
		return nil, repoError(e)
	}
	defer closeRows(rows, &err)
	for rows.Next() {
		v, e := scanReminder(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, repoError(rows.Err())
}
func (r reminders) ByID(ctx context.Context, id int64) (domain.Reminder, error) {
	return scanReminder(r.q.QueryRowContext(ctx, "SELECT "+reminderColumns+" FROM reminders WHERE id=?", id))
}
func (r reminders) MarkSent(ctx context.Context, id int64, at time.Time) error {
	if e := exec(ctx, r.q, "UPDATE reminders SET sent_at=? WHERE id=?", stamp(at), id); e != nil {
		return e
	}
	return r.cancelOlder(ctx, id, at)
}
func (r reminders) Reschedule(ctx context.Context, id int64, at time.Time) error {
	return exec(ctx, r.q, "UPDATE reminders SET send_at=? WHERE id=? AND sent_at IS NULL AND cancelled_at IS NULL", stamp(at), id)
}
func (r reminders) Cancel(ctx context.Context, id int64, at time.Time) error {
	if e := r.cancelOlder(ctx, id, at); e != nil {
		return e
	}
	return exec(ctx, r.q, "UPDATE reminders SET cancelled_at=? WHERE id=? AND sent_at IS NULL AND cancelled_at IS NULL", stamp(at), id)
}
func (r reminders) cancelOlder(ctx context.Context, id int64, at time.Time) error {
	return exec(ctx, r.q, `UPDATE reminders SET cancelled_at=? WHERE source='default' AND sent_at IS NULL AND cancelled_at IS NULL AND id<>? AND EXISTS(SELECT 1 FROM reminders current WHERE current.id=? AND current.source='default' AND current.user_id=reminders.user_id AND current.assignment_id=reminders.assignment_id AND current.assignment_version=reminders.assignment_version AND (reminders.send_at<current.send_at OR (reminders.send_at=current.send_at AND reminders.id<current.id)))`, stamp(at), id, id)
}
