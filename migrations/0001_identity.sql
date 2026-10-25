-- +goose Up
CREATE TABLE groups (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    invite_code TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);

CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    telegram_id INTEGER NOT NULL UNIQUE CHECK (telegram_id > 0),
    display_name TEXT NOT NULL,
    group_id INTEGER REFERENCES groups(id) ON DELETE RESTRICT,
    role TEXT NOT NULL DEFAULT 'student' CHECK (role IN ('student', 'editor', 'admin')),
    created_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE users;
DROP TABLE groups;
