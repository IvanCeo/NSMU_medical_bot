package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

type digests struct{ store }

func (r digests) Ensure(ctx context.Context, gid int64, date string, binding int64, at time.Time) (domain.DigestRun, bool, error) {
	res, e := r.q.ExecContext(ctx, "INSERT INTO digest_runs(group_id,local_date,binding_version,status,created_at) VALUES(?,?,?,'planning',?) ON CONFLICT DO NOTHING", gid, date, binding, stamp(at))
	if e != nil {
		return domain.DigestRun{}, false, repoError(e)
	}
	n, e := res.RowsAffected()
	if e != nil {
		return domain.DigestRun{}, false, repoError(e)
	}
	d, e := scanDigest(r.q.QueryRowContext(ctx, "SELECT id,group_id,local_date,binding_version,status,created_at FROM digest_runs WHERE group_id=? AND local_date=?", gid, date))
	return d, n == 1, e
}
func scanDigest(row scanner) (domain.DigestRun, error) {
	var d domain.DigestRun
	var at string
	e := row.Scan(&d.ID, &d.GroupID, &d.LocalDate, &d.BindingVersion, &d.Status, &at)
	d.CreatedAt = parse(at)
	return d, repoError(e)
}
func (r digests) ByID(ctx context.Context, id int64) (domain.DigestRun, error) {
	return scanDigest(r.q.QueryRowContext(ctx, "SELECT id,group_id,local_date,binding_version,status,created_at FROM digest_runs WHERE id=?", id))
}
func (r digests) SetStatus(ctx context.Context, id int64, s domain.DigestStatus) error {
	return exec(ctx, r.q, "UPDATE digest_runs SET status=? WHERE id=?", s, id)
}

type deliveries struct{ store }

func (r deliveries) Enqueue(ctx context.Context, d domain.DeliveryDraft) error {
	p, e := json.Marshal(d.Payload)
	if e != nil {
		return repoError(e)
	}
	return exec(ctx, r.q, "INSERT INTO outbox(event_key,kind,reminder_id,digest_run_id,group_id,user_id,part,payload_json,next_attempt_at,expires_at,assignment_id) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(event_key) DO NOTHING", d.EventKey, d.Kind, d.ReminderID, d.DigestRunID, d.GroupID, d.UserID, d.Part, string(p), stamp(d.NextAttemptAt), stamp(d.ExpiresAt), d.AssignmentID)
}

const deliveryColumns = "id,event_key,kind,reminder_id,digest_run_id,group_id,user_id,part,payload_json,status,attempts,next_attempt_at,expires_at,lease_token,lease_until,sent_at,telegram_message_id,COALESCE(last_error_code,''),assignment_id"

func scanDelivery(row scanner) (*domain.Delivery, error) {
	var d domain.Delivery
	var rem, run, user, msg, assignment sql.NullInt64
	var payload, next, expiry string
	var token, lease, sent sql.NullString
	e := row.Scan(&d.ID, &d.EventKey, &d.Kind, &rem, &run, &d.GroupID, &user, &d.Part, &payload, &d.Status, &d.Attempts, &next, &expiry, &token, &lease, &sent, &msg, &d.LastErrorCode, &assignment)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, repoError(e)
	}
	if rem.Valid {
		d.ReminderID = &rem.Int64
	}
	if assignment.Valid {
		d.AssignmentID = &assignment.Int64
	}
	if run.Valid {
		d.DigestRunID = &run.Int64
	}
	if user.Valid {
		d.UserID = &user.Int64
	}
	if msg.Valid {
		v := int(msg.Int64)
		d.TelegramMessageID = &v
	}
	d.NextAttemptAt = parse(next)
	d.ExpiresAt = parse(expiry)
	d.LeaseToken = token.String
	d.LeaseUntil = nullableTime(lease)
	d.SentAt = nullableTime(sent)
	if e := json.Unmarshal([]byte(payload), &d.Payload); e != nil {
		return nil, repoError(e)
	}
	return &d, nil
}
func (r deliveries) Claim(ctx context.Context, now, until time.Time, token string) (*domain.Delivery, error) {
	return scanDelivery(r.q.QueryRowContext(ctx, `UPDATE outbox SET status='processing',lease_token=?,lease_until=?,attempts=attempts+1 WHERE id=(SELECT id FROM outbox WHERE ((status='pending' AND next_attempt_at<=?) OR (status='processing' AND lease_until<=?)) AND expires_at>? ORDER BY next_attempt_at,id LIMIT 1) RETURNING `+deliveryColumns, token, stamp(until), stamp(now), stamp(now), stamp(now)))
}
func (r deliveries) Complete(ctx context.Context, id int64, token string, receipt domain.MessageReceipt, at time.Time) error {
	return cas(ctx, r.q, "UPDATE outbox SET status='sent',sent_at=?,telegram_message_id=?,lease_token=NULL,lease_until=NULL WHERE id=? AND status='processing' AND lease_token=? AND lease_until>?", stamp(at), receipt.MessageID, id, token, stamp(at))
}
func (r deliveries) Retry(ctx context.Context, id int64, token string, f domain.DeliveryFailure, at time.Time) error {
	return cas(ctx, r.q, "UPDATE outbox SET status='pending',next_attempt_at=?,last_error_code=?,attempts=attempts-CASE WHEN ?='quiet_hours' THEN 1 ELSE 0 END,lease_token=NULL,lease_until=NULL WHERE id=? AND status='processing' AND lease_token=?", stamp(at), f.Code, f.Code, id, token)
}
func (r deliveries) Fail(ctx context.Context, id int64, token string, status domain.DeliveryStatus, code string, at time.Time) error {
	return cas(ctx, r.q, "UPDATE outbox SET status=?,last_error_code=?,lease_token=NULL,lease_until=NULL WHERE id=? AND status='processing' AND lease_token=? AND lease_until>?", status, code, id, token, stamp(at))
}
func (r deliveries) CancelForAssignment(ctx context.Context, id int64, at time.Time) error {
	if e := exec(ctx, r.q, "UPDATE outbox SET status='cancelled',lease_token=NULL,lease_until=NULL WHERE kind='assignment_created' AND assignment_id=? AND status IN ('pending','processing')", id); e != nil {
		return e
	}
	return r.CancelRemindersForAssignment(ctx, id, at)
}
func (r deliveries) CancelRemindersForAssignment(ctx context.Context, id int64, at time.Time) error {
	return exec(ctx, r.q, "UPDATE outbox SET status='cancelled',lease_token=NULL,lease_until=NULL WHERE kind='reminder' AND reminder_id IN (SELECT id FROM reminders WHERE assignment_id=?) AND status IN ('pending','processing')", id)
}
func (r deliveries) CancelForUser(ctx context.Context, uid int64, aid *int64, at time.Time) error {
	if e := exec(ctx, r.q, "UPDATE outbox SET status='cancelled',lease_token=NULL,lease_until=NULL WHERE user_id=? AND kind='assignment_created' AND (? IS NULL OR assignment_id=?) AND status IN ('pending','processing')", uid, aid, aid); e != nil {
		return e
	}
	return r.CancelRemindersForUser(ctx, uid, aid, at)
}
func (r deliveries) CancelRemindersForUser(ctx context.Context, uid int64, aid *int64, at time.Time) error {
	return exec(ctx, r.q, "UPDATE outbox SET status='cancelled',lease_token=NULL,lease_until=NULL WHERE kind='reminder' AND user_id=? AND (? IS NULL OR reminder_id IN (SELECT id FROM reminders WHERE assignment_id=?)) AND status IN ('pending','processing')", uid, aid, aid)
}
func (r deliveries) CancelForGroup(ctx context.Context, gid int64, at time.Time) error {
	if e := exec(ctx, r.q, "UPDATE outbox SET status='cancelled',lease_token=NULL,lease_until=NULL WHERE group_id=? AND kind='digest' AND status IN ('pending','processing')", gid); e != nil {
		return e
	}
	return r.refreshDigests(ctx)
}
func (r deliveries) Expire(ctx context.Context, now time.Time) error {
	if e := exec(ctx, r.q, "UPDATE outbox SET status='skipped',last_error_code='expired',lease_token=NULL,lease_until=NULL WHERE expires_at<=? AND status IN ('pending','processing')", stamp(now)); e != nil {
		return e
	}
	return r.refreshDigests(ctx)
}
func (r deliveries) refreshDigests(ctx context.Context) error {
	return exec(ctx, r.q, `UPDATE digest_runs SET status=CASE
 WHEN EXISTS(SELECT 1 FROM outbox WHERE digest_run_id=digest_runs.id AND status='sent') THEN
 CASE WHEN EXISTS(SELECT 1 FROM outbox WHERE digest_run_id=digest_runs.id AND status<>'sent') THEN 'partial' ELSE 'sent' END
 WHEN EXISTS(SELECT 1 FROM outbox WHERE digest_run_id=digest_runs.id AND status='failed') THEN 'failed'
 WHEN EXISTS(SELECT 1 FROM outbox WHERE digest_run_id=digest_runs.id AND status='cancelled') THEN 'cancelled' ELSE 'empty' END
 WHERE status='queued' AND NOT EXISTS(SELECT 1 FROM outbox WHERE digest_run_id=digest_runs.id AND status IN ('pending','processing'))`)
}
func (r deliveries) Progress(ctx context.Context, id int64) (domain.DeliveryProgress, error) {
	var p domain.DeliveryProgress
	e := r.q.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(status IN ('pending','processing')),0),COALESCE(SUM(status='sent'),0),COALESCE(SUM(status='skipped'),0),COALESCE(SUM(status='failed'),0),COALESCE(SUM(status='cancelled'),0) FROM outbox WHERE digest_run_id=?`, id).Scan(&p.Total, &p.Pending, &p.Sent, &p.Skipped, &p.Failed, &p.Cancelled)
	return p, repoError(e)
}
