package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

type settings struct{ store }

func (r settings) Get(ctx context.Context, uid int64) (p domain.Preferences, err error) {
	p = domain.Preferences{RemindersEnabled: true}
	var start, end string
	rows, e := r.q.QueryContext(ctx, "SELECT key,value FROM settings WHERE user_id=?", uid)
	if e != nil {
		return p, repoError(e)
	}
	defer closeRows(rows, &err)
	for rows.Next() {
		var k, v string
		if e := rows.Scan(&k, &v); e != nil {
			return p, repoError(e)
		}
		switch k {
		case "reminders_enabled":
			p.RemindersEnabled = v == "true"
		case "quiet_start":
			start = v
		case "quiet_end":
			end = v
		}
	}
	if start != "" && end != "" {
		p.QuietHours = &domain.QuietHours{Start: start, End: end}
	}
	return p, repoError(rows.Err())
}
func (r settings) Save(ctx context.Context, uid int64, p domain.Preferences) error {
	enabled := "false"
	if p.RemindersEnabled {
		enabled = "true"
	}
	start, end := "", ""
	if p.QuietHours != nil {
		start = p.QuietHours.Start
		end = p.QuietHours.End
	}
	for _, v := range [][2]string{{"reminders_enabled", enabled}, {"quiet_start", start}, {"quiet_end", end}} {
		if e := exec(ctx, r.q, "INSERT INTO settings(user_id,key,value) VALUES(?,?,?) ON CONFLICT(user_id,key) DO UPDATE SET value=excluded.value", uid, v[0], v[1]); e != nil {
			return e
		}
	}
	return nil
}

type dialogs struct{ store }

func (r dialogs) ByUser(ctx context.Context, uid int64) (*domain.Dialog, error) {
	var d domain.Dialog
	var expiry, updated string
	e := r.q.QueryRowContext(ctx, "SELECT id,user_id,kind,step,payload_json,revision,status,expires_at,updated_at FROM dialog_states WHERE user_id=? AND status='active'", uid).Scan(&d.ID, &d.UserID, &d.Kind, &d.Step, &d.Payload, &d.Revision, &d.Status, &expiry, &updated)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	d.ExpiresAt = parse(expiry)
	d.UpdatedAt = parse(updated)
	return &d, repoError(e)
}
func (r dialogs) Save(ctx context.Context, d domain.Dialog, v int64) (domain.Dialog, error) {
	if d.ID == 0 {
		res, e := r.q.ExecContext(ctx, "INSERT INTO dialog_states(user_id,kind,step,payload_json,revision,status,expires_at,updated_at) VALUES(?,?,?,?,1,'active',?,?)", d.UserID, d.Kind, d.Step, d.Payload, stamp(d.ExpiresAt), stamp(d.UpdatedAt))
		if e != nil {
			return d, repoError(e)
		}
		d.ID, e = res.LastInsertId()
		d.Revision = 1
		d.Status = domain.DialogActive
		return d, repoError(e)
	}
	if d.Status != domain.DialogActive {
		d.Payload = "{}"
	}
	e := cas(ctx, r.q, "UPDATE dialog_states SET step=?,payload_json=?,revision=revision+1,status=?,expires_at=?,updated_at=? WHERE id=? AND user_id=? AND revision=? AND status='active'", d.Step, d.Payload, d.Status, stamp(d.ExpiresAt), stamp(d.UpdatedAt), d.ID, d.UserID, v)
	d.Revision = v + 1
	return d, e
}
func (r dialogs) Finish(ctx context.Context, uid int64) error {
	return exec(ctx, r.q, "UPDATE dialog_states SET status='cancelled',payload_json='{}',revision=revision+1 WHERE user_id=? AND status='active'", uid)
}
func (r dialogs) Expire(ctx context.Context, now time.Time) error {
	return exec(ctx, r.q, "UPDATE dialog_states SET status='expired',payload_json='{}',revision=revision+1 WHERE status='active' AND expires_at<=?", stamp(now))
}

type audit struct{ store }

func (r audit) Append(ctx context.Context, a domain.AuditEntry) error {
	return exec(ctx, r.q, "INSERT INTO audit_log(group_id,actor_id,action,entity,entity_id,payload_json,at) VALUES(?,?,?,?,?,?,?)", a.GroupID, a.ActorID, a.Action, a.Entity, a.EntityID, a.Payload, stamp(a.At))
}
func (r audit) List(ctx context.Context, gid int64, p domain.PageRequest) (out domain.AuditPage, err error) {
	n, id, e := page(p)
	if e != nil {
		return out, e
	}
	rows, e := r.q.QueryContext(ctx, "SELECT id,group_id,actor_id,action,entity,entity_id,payload_json,at FROM audit_log WHERE group_id=? AND id>? ORDER BY id LIMIT ?", gid, id, n+1)
	if e != nil {
		return out, repoError(e)
	}
	defer closeRows(rows, &err)
	for rows.Next() {
		var a domain.AuditEntry
		var g, u sql.NullInt64
		var at string
		if e := rows.Scan(&a.ID, &g, &u, &a.Action, &a.Entity, &a.EntityID, &a.Payload, &at); e != nil {
			return out, repoError(e)
		}
		if g.Valid {
			a.GroupID = &g.Int64
		}
		if u.Valid {
			a.ActorID = &u.Int64
		}
		a.At = parse(at)
		out.Items = append(out.Items, a)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		out.NextCursor = cursor(out.Items[n-1].ID)
	}
	return out, repoError(rows.Err())
}
