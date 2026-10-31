package sqlite

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

type legacyDelivery struct {
	ID, GroupID, Part, Attempts                    int64
	EventKey, Kind, Payload, Status, Next, Expires string
	ReminderID, DigestID, UserID, MessageID        sql.NullInt64
	Token, Lease, Sent, Error                      sql.NullString
}

func legacyQueue(t *testing.T, db *sql.DB) []legacyDelivery {
	t.Helper()
	rows, e := db.QueryContext(t.Context(), "SELECT id,event_key,kind,reminder_id,digest_run_id,group_id,user_id,part,payload_json,status,attempts,next_attempt_at,expires_at,lease_token,lease_until,sent_at,telegram_message_id,last_error_code FROM outbox ORDER BY id")
	check(t, e)
	var out []legacyDelivery
	for rows.Next() {
		var d legacyDelivery
		check(t, rows.Scan(&d.ID, &d.EventKey, &d.Kind, &d.ReminderID, &d.DigestID, &d.GroupID, &d.UserID, &d.Part, &d.Payload, &d.Status, &d.Attempts, &d.Next, &d.Expires, &d.Token, &d.Lease, &d.Sent, &d.MessageID, &d.Error))
		out = append(out, d)
	}
	check(t, rows.Err())
	check(t, rows.Close())
	return out
}

func TestMigrationAssignmentNotifications_PreservesAllLegacyColumnsAndQueue(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	p, e := migrationProvider(db)
	check(t, e)
	_, e = p.UpTo(ctx, 3)
	check(t, e)
	for _, query := range []string{
		"INSERT INTO groups(id,title,invite_code,created_at) VALUES(1,'Test','test','2026-01-01T00:00:00Z')",
		"INSERT INTO users(id,telegram_id,display_name,group_id,role,created_at) VALUES(1,1,'Test',1,'student','2026-01-01T00:00:00Z')",
		"INSERT INTO subjects(id,group_id,name,created_at,updated_at) VALUES(1,1,'Test','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')",
		"INSERT INTO assignments(id,group_id,subject_id,kind,title,deadline_at,created_by,created_at,updated_at) VALUES(1,1,1,'homework','Test','2026-12-01T00:00:00Z',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')",
		"INSERT INTO reminders(id,assignment_id,user_id,assignment_version,source,offset_seconds,send_at) VALUES(1,1,1,1,'default',3600,'2026-11-30T23:00:00Z')",
		"INSERT INTO digest_runs(id,group_id,local_date,binding_version,status,created_at) VALUES(1,1,'2026-12-01',3,'queued','2026-01-01T00:00:00Z')",
	} {
		_, e := db.ExecContext(ctx, query)
		check(t, e)
	}
	for _, kind := range []string{"reminder", "digest"} {
		for i, status := range []string{"pending", "processing", "sent", "skipped", "failed", "cancelled"} {
			var rem, run, user, message any
			var token, lease, sent, code any
			part := 0
			if kind == "reminder" {
				rem, user = 1, 1
			} else {
				run, part = 1, i
			}
			if status == "processing" {
				token, lease = "test-lease", "2026-12-01T00:00:00Z"
			}
			if status == "sent" {
				sent, message = "2026-11-30T23:30:00Z", 12345
			}
			if i > 2 {
				code = "test-code"
			}
			_, e := db.ExecContext(ctx, `INSERT INTO outbox(event_key,kind,reminder_id,digest_run_id,group_id,user_id,part,payload_json,status,attempts,next_attempt_at,expires_at,lease_token,lease_until,sent_at,telegram_message_id,last_error_code) VALUES(?,?,?,?,1,?,?,?,?,?,?,?,?,?,?,?,?)`, fmt.Sprintf("%s:%s", kind, status), kind, rem, run, user, part, `{"AssignmentIDs":[1],"Version":1,"BindingVersion":3,"LocalDate":"2026-12-01"}`, status, i, "2026-11-30T23:00:00Z", "2026-12-02T00:00:00Z", token, lease, sent, message, code)
			check(t, e)
		}
	}
	before := legacyQueue(t, db)
	check(t, Migrate(ctx, db))
	if after := legacyQueue(t, db); !reflect.DeepEqual(before, after) {
		t.Fatalf("legacy values changed:\nbefore=%+v\nafter=%+v", before, after)
	}
	var count int
	check(t, db.QueryRowContext(ctx, "SELECT count(*) FROM outbox WHERE assignment_id IS NOT NULL").Scan(&count))
	if count != 0 {
		t.Fatal("legacy events were backfilled")
	}
	check(t, db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='index' AND name LIKE 'outbox_%'").Scan(&count))
	if count != 6 {
		t.Fatalf("outbox indexes=%d", count)
	}
	rows, e := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	check(t, e)
	if rows.Next() {
		t.Fatal("foreign key integrity changed")
	}
	check(t, rows.Err())
	check(t, rows.Close())
	_, e = p.Down(ctx)
	check(t, e)
	if !reflect.DeepEqual(before, legacyQueue(t, db)) {
		t.Fatal("downgrade changed legacy values")
	}
	check(t, Migrate(ctx, db))
	at := time.Date(2026, 11, 30, 23, 30, 0, 0, time.UTC)
	r := NewStore(db).Repositories()
	d, e := r.Deliveries.Claim(ctx, at, at.Add(time.Minute), "new-lease")
	check(t, e)
	if d == nil || d.EventKey != "reminder:pending" || d.AssignmentID != nil {
		t.Fatalf("legacy queue cannot resume: %+v", d)
	}
	check(t, r.Deliveries.Complete(ctx, d.ID, d.LeaseToken, domain.MessageReceipt{MessageID: 54321}, at))
	aid, uid := int64(1), int64(1)
	check(t, r.Deliveries.Enqueue(ctx, domain.DeliveryDraft{EventKey: "assignment_created:1:1", Kind: domain.AssignmentCreatedDelivery, AssignmentID: &aid, GroupID: 1, UserID: &uid, NextAttemptAt: at, ExpiresAt: at.Add(time.Hour)}))
	if _, e = p.Down(ctx); e == nil {
		t.Fatal("downgrade allowed loss of creation events")
	}
	_, e = db.ExecContext(ctx, "UPDATE outbox SET assignment_id=999 WHERE kind='assignment_created'")
	if e == nil {
		t.Fatal("assignment foreign key missing")
	}
	_, e = db.ExecContext(ctx, "UPDATE outbox SET reminder_id=1 WHERE kind='assignment_created'")
	if e == nil {
		t.Fatal("event shape constraint missing")
	}
}
