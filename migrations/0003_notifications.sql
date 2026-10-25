-- +goose Up
CREATE TABLE reminders (
 id INTEGER PRIMARY KEY, assignment_id INTEGER NOT NULL REFERENCES assignments(id), user_id INTEGER NOT NULL REFERENCES users(id),
 assignment_version INTEGER NOT NULL CHECK(assignment_version>0), source TEXT NOT NULL CHECK(source IN ('default','snooze')),
 offset_seconds REAL, send_at TEXT NOT NULL, sent_at TEXT, cancelled_at TEXT,
 CHECK((source='default' AND offset_seconds>0) OR (source='snooze' AND offset_seconds IS NULL))
);
CREATE UNIQUE INDEX reminders_standard ON reminders(user_id,assignment_id,assignment_version,offset_seconds) WHERE source='default' AND cancelled_at IS NULL;
CREATE INDEX reminders_due ON reminders(send_at) WHERE sent_at IS NULL AND cancelled_at IS NULL;
CREATE INDEX reminders_user_assignment ON reminders(user_id,assignment_id);
CREATE TABLE digest_runs (
 id INTEGER PRIMARY KEY, group_id INTEGER NOT NULL REFERENCES groups(id), local_date TEXT NOT NULL,
 binding_version INTEGER NOT NULL, status TEXT NOT NULL CHECK(status IN ('planning','queued','sent','empty','partial','failed','cancelled')),
 created_at TEXT NOT NULL, UNIQUE(group_id,local_date)
);
CREATE TABLE outbox (
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
CREATE INDEX outbox_due ON outbox(status,next_attempt_at);
CREATE INDEX outbox_lease ON outbox(lease_until) WHERE status='processing';
CREATE INDEX outbox_user ON outbox(user_id,reminder_id);
CREATE INDEX outbox_digest ON outbox(digest_run_id);

-- +goose Down
DROP TABLE outbox;
DROP TABLE digest_runs;
DROP TABLE reminders;
