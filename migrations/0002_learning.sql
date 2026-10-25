-- +goose Up
ALTER TABLE groups ADD COLUMN telegram_chat_id INTEGER CHECK(telegram_chat_id<0);
ALTER TABLE groups ADD COLUMN digest_enabled INTEGER NOT NULL DEFAULT 1 CHECK(digest_enabled IN (0,1));
ALTER TABLE groups ADD COLUMN version INTEGER NOT NULL DEFAULT 1 CHECK(version > 0);
ALTER TABLE groups ADD COLUMN chat_binding_version INTEGER NOT NULL DEFAULT 0 CHECK(chat_binding_version >= 0);
CREATE UNIQUE INDEX groups_chat ON groups(telegram_chat_id) WHERE telegram_chat_id IS NOT NULL;
ALTER TABLE users ADD COLUMN private_chat_available INTEGER NOT NULL DEFAULT 0 CHECK(private_chat_available IN (0,1));
CREATE INDEX users_group ON users(group_id);
CREATE TABLE subjects (
 id INTEGER PRIMARY KEY, group_id INTEGER NOT NULL REFERENCES groups(id),
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200), teacher TEXT,
 archived_at TEXT, version INTEGER NOT NULL DEFAULT 1 CHECK(version>0),
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(id,group_id)
);
CREATE UNIQUE INDEX subjects_active_name ON subjects(group_id,name) WHERE archived_at IS NULL;
CREATE INDEX subjects_group ON subjects(group_id);
CREATE TABLE assignments (
 id INTEGER PRIMARY KEY, group_id INTEGER NOT NULL REFERENCES groups(id), subject_id INTEGER NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('homework','test','exam','practice','other')),
 title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200), description TEXT NOT NULL DEFAULT '' CHECK(length(description)<=3000),
 deadline_at TEXT NOT NULL, importance TEXT NOT NULL DEFAULT 'normal' CHECK(importance IN ('low','normal','high')),
 url TEXT, created_by INTEGER NOT NULL REFERENCES users(id),
 status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','cancelled')), version INTEGER NOT NULL DEFAULT 1 CHECK(version>0),
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 FOREIGN KEY(subject_id,group_id) REFERENCES subjects(id,group_id)
);
CREATE INDEX assignments_group_deadline ON assignments(group_id,deadline_at,id);
CREATE INDEX assignments_subject ON assignments(subject_id);
CREATE TABLE attachments (
 id INTEGER PRIMARY KEY, assignment_id INTEGER NOT NULL REFERENCES assignments(id),
 kind TEXT NOT NULL CHECK(kind IN ('document','photo')), file_id TEXT NOT NULL, file_name TEXT NOT NULL DEFAULT '', mime TEXT NOT NULL DEFAULT ''
);
CREATE INDEX attachments_assignment ON attachments(assignment_id);
CREATE TABLE completions (
 user_id INTEGER NOT NULL REFERENCES users(id), assignment_id INTEGER NOT NULL REFERENCES assignments(id), done_at TEXT NOT NULL,
 PRIMARY KEY(user_id,assignment_id)
);
CREATE TABLE settings (
 user_id INTEGER NOT NULL REFERENCES users(id), key TEXT NOT NULL CHECK(key IN ('reminders_enabled','quiet_start','quiet_end')),
 value TEXT NOT NULL, PRIMARY KEY(user_id,key)
);
CREATE TABLE dialog_states (
 id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id), kind TEXT NOT NULL CHECK(kind IN ('join','leave','add','edit','cancel_task','admin','settings','subjects')),
 step TEXT NOT NULL, payload_json TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision>0),
 status TEXT NOT NULL CHECK(status IN ('active','completed','cancelled','expired')), expires_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX dialogs_active_user ON dialog_states(user_id) WHERE status='active';
CREATE INDEX dialogs_expiry ON dialog_states(expires_at) WHERE status='active';
CREATE TABLE audit_log (
 id INTEGER PRIMARY KEY, group_id INTEGER REFERENCES groups(id), actor_id INTEGER REFERENCES users(id),
 action TEXT NOT NULL, entity TEXT NOT NULL, entity_id INTEGER NOT NULL, payload_json TEXT NOT NULL, at TEXT NOT NULL
);
CREATE INDEX audit_group_at ON audit_log(group_id,at,id);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE dialog_states;
DROP TABLE settings;
DROP TABLE completions;
DROP TABLE attachments;
DROP TABLE assignments;
DROP TABLE subjects;
DROP INDEX users_group;
ALTER TABLE users DROP COLUMN private_chat_available;
DROP INDEX groups_chat;
ALTER TABLE groups DROP COLUMN chat_binding_version;
ALTER TABLE groups DROP COLUMN version;
ALTER TABLE groups DROP COLUMN digest_enabled;
ALTER TABLE groups DROP COLUMN telegram_chat_id;
