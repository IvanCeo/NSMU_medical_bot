-- +goose Up
CREATE TABLE outbox_new (
 id INTEGER PRIMARY KEY, event_key TEXT NOT NULL UNIQUE, kind TEXT NOT NULL CHECK(kind IN ('reminder','digest','assignment_created')),
 reminder_id INTEGER REFERENCES reminders(id), digest_run_id INTEGER REFERENCES digest_runs(id), group_id INTEGER NOT NULL REFERENCES groups(id),
 user_id INTEGER REFERENCES users(id), part INTEGER NOT NULL CHECK(part>=0), payload_json TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','sent','skipped','failed','cancelled')),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0), next_attempt_at TEXT NOT NULL, expires_at TEXT NOT NULL,
 lease_token TEXT, lease_until TEXT, sent_at TEXT, telegram_message_id INTEGER, last_error_code TEXT,
 assignment_id INTEGER REFERENCES assignments(id),
 CHECK((kind='reminder' AND reminder_id IS NOT NULL AND user_id IS NOT NULL AND digest_run_id IS NULL AND assignment_id IS NULL)
 OR (kind='digest' AND digest_run_id IS NOT NULL AND reminder_id IS NULL AND user_id IS NULL AND assignment_id IS NULL)
 OR (kind='assignment_created' AND assignment_id IS NOT NULL AND user_id IS NOT NULL AND reminder_id IS NULL AND digest_run_id IS NULL AND part=0)),
 CHECK((status='processing' AND lease_token IS NOT NULL AND lease_until IS NOT NULL) OR
 (status<>'processing' AND lease_token IS NULL AND lease_until IS NULL))
);
INSERT INTO outbox_new (id,event_key,kind,reminder_id,digest_run_id,group_id,user_id,part,payload_json,status,attempts,next_attempt_at,expires_at,lease_token,lease_until,sent_at,telegram_message_id,last_error_code)
 SELECT id,event_key,kind,reminder_id,digest_run_id,group_id,user_id,part,payload_json,status,attempts,next_attempt_at,expires_at,lease_token,lease_until,sent_at,telegram_message_id,last_error_code FROM outbox;
DROP TABLE outbox;
ALTER TABLE outbox_new RENAME TO outbox;
CREATE INDEX outbox_due ON outbox(status,next_attempt_at);
CREATE INDEX outbox_lease ON outbox(lease_until) WHERE status='processing';
CREATE INDEX outbox_user ON outbox(user_id,reminder_id);
CREATE INDEX outbox_digest ON outbox(digest_run_id);
CREATE INDEX outbox_assignment_created ON outbox(assignment_id) WHERE kind='assignment_created';
CREATE INDEX outbox_reminder ON outbox(reminder_id) WHERE kind='reminder';

-- +goose Down
-- Refuse a downgrade that would discard new events, including already sent ones.
CREATE TEMP TABLE outbox_downgrade_guard (event_count INTEGER CHECK(event_count=0));
INSERT INTO outbox_downgrade_guard SELECT count(*) FROM outbox WHERE kind='assignment_created';
DROP TABLE outbox_downgrade_guard;
CREATE TABLE outbox_old (
 id INTEGER PRIMARY KEY, event_key TEXT NOT NULL UNIQUE, kind TEXT NOT NULL CHECK(kind IN ('reminder','digest')),
 reminder_id INTEGER REFERENCES reminders(id), digest_run_id INTEGER REFERENCES digest_runs(id), group_id INTEGER NOT NULL REFERENCES groups(id),
 user_id INTEGER REFERENCES users(id), part INTEGER NOT NULL CHECK(part>=0), payload_json TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','sent','skipped','failed','cancelled')),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0), next_attempt_at TEXT NOT NULL, expires_at TEXT NOT NULL,
 lease_token TEXT, lease_until TEXT, sent_at TEXT, telegram_message_id INTEGER, last_error_code TEXT,
 CHECK((kind='reminder' AND reminder_id IS NOT NULL AND user_id IS NOT NULL AND digest_run_id IS NULL)
 OR (kind='digest' AND digest_run_id IS NOT NULL AND reminder_id IS NULL AND user_id IS NULL)),
 CHECK((status='processing' AND lease_token IS NOT NULL AND lease_until IS NOT NULL) OR
 (status<>'processing' AND lease_token IS NULL AND lease_until IS NULL))
);
INSERT INTO outbox_old (id,event_key,kind,reminder_id,digest_run_id,group_id,user_id,part,payload_json,status,attempts,next_attempt_at,expires_at,lease_token,lease_until,sent_at,telegram_message_id,last_error_code)
 SELECT id,event_key,kind,reminder_id,digest_run_id,group_id,user_id,part,payload_json,status,attempts,next_attempt_at,expires_at,lease_token,lease_until,sent_at,telegram_message_id,last_error_code FROM outbox;
DROP TABLE outbox;
ALTER TABLE outbox_old RENAME TO outbox;
CREATE INDEX outbox_due ON outbox(status,next_attempt_at);
CREATE INDEX outbox_lease ON outbox(lease_until) WHERE status='processing';
CREATE INDEX outbox_user ON outbox(user_id,reminder_id);
CREATE INDEX outbox_digest ON outbox(digest_run_id);
