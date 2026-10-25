# Architecture

> Authoritative design doc for humans and agents.
> Agent rules: AGENTS.md. Decisions: docs/adr/.

## 1. Context

Medical bot (@NSMU_medicial_bot) replaces ad-hoc "homework in the group chat"
with a structured Telegram flow: assignments, tests, deadlines, reminders,
digests, completion marks.

- Primary users: students of one medical cohort.
- Secondary: староста (editor).
- Admin: single operator.
- Storage: учебные данные only. No PHI, no clinical data.

MVP scale: one cohort, ~100–500 users, low write rate, reads dominated by
deadline lookups and digests.

## 2. Component diagram

        +--------------+   Telegram Bot API   +---------------+
        |  Telegram    |<-------------------->|  internal/bot |
        |   clients    |                      |  (handlers)   |
        +--------------+                      +-------+-------+
                                                      | calls
                                              +-------v-------+
                                              | internal/     |
                                              |  service      |<---- domain
                                              +---+-------+---+
                                                  |       |
                                   +--------------v-+ +---v--------------+
                                   | internal/      | | internal/        |
                                   | repository     | | scheduler        |
                                   | (SQLite)       | | (in-process)     |
                                   +-------+--------+ +-------+----------+
                                           |                  | uses service
                                    +------v------+           |
                                    |  SQLite     |<----------+
                                    |  file volume|
                                    +-------------+

Dependency direction (inward only):

- internal/bot — Telegram transport. Parses updates, calls service, renders replies.
- internal/service — use cases + interfaces it consumes (AssignmentRepo, UserRepo, ...).
- internal/repository — SQLite implementation of service interfaces.
- internal/domain — entities, sentinel errors, value objects. No deps.
- internal/scheduler — time-driven jobs (reminders, digests). Uses service.
- internal/config — env parsing.
- cmd/bot — wiring, lifecycle, graceful shutdown.

Interfaces live in the consumer (service), not in repository.

## 3. Target directory layout

    cmd/bot/main.go
    internal/
      bot/{router.go,handlers_*.go,keyboards.go,callback.go}
      service/{assignment.go,user.go,reminder.go,digest.go,interfaces.go,errors.go}
      repository/{sqlite/*.go, migrations.go}
      domain/{assignment.go,user.go,group.go,errors.go}
      scheduler/{scheduler.go,jobs.go}
      config/config.go
    migrations/0001_init.sql, ...
    docs/adr/

## 4. Data model (MVP)

All timestamps UTC (TEXT, RFC3339). FKs enforced via PRAGMA foreign_keys = ON.

- groups(id PK, title, invite_code UNIQUE, created_at)
- users(id PK, telegram_id UNIQUE, display_name, group_id FK, role, created_at)
    role in {student, editor, admin}
- subjects(id PK, group_id FK, name, teacher NULL)
- assignments(id PK, group_id FK, subject_id FK, kind, title, description,
    deadline_at, importance, url NULL, created_by FK users.id, status,
    created_at, updated_at)
    kind in {homework, test, exam, practice, other}
    status in {active, cancelled}
- attachments(id PK, assignment_id FK, file_id, file_name, mime)
- completions(user_id FK, assignment_id FK, done_at) — PK (user_id, assignment_id)
- reminders(id PK, assignment_id FK, send_at, sent_at NULL, target)
    target in {user:<id>, group:<id>}
- audit_log(id PK, actor_id FK, action, entity, entity_id, payload_json, at)
- settings(user_id FK, key, value) — per-user reminder prefs

Indexes: assignments(group_id, deadline_at), assignments(subject_id),
reminders(send_at) WHERE sent_at IS NULL.

## 5. Key flows

5.1. Create assignment (editor)

/add -> wizard (subject -> kind -> title -> desc -> deadline -> url/file -> publish)
-> service.CreateAssignment(ctx, input) -> insert + schedule default reminders
(deadline-24h, deadline-3h) -> post card to group chat + persist message id for
later edit/pin.

5.2. Daily digest

scheduler tick (from DIGEST_TIME in TZ) -> service.BuildDigest(groupID, "day")
-> format -> bot.SendMessage(groupChatID) -> optional pin.

5.3. Reminder

scheduler polls reminders due in the next minute -> service.DispatchReminder
-> bot.SendMessage(user|group) -> mark sent_at -> respect quiet hours.

5.4. Mark complete

Inline done:<assignmentID> -> service.MarkComplete(userID, assignmentID)
-> upsert completions -> update card in-place (personal view only).

## 6. Contracts

Any change below requires Lead approval + human confirmation before implementation.

- Service interfaces (Go, internal/service/interfaces.go):
  UserRepo, GroupRepo, SubjectRepo, AssignmentRepo, CompletionRepo,
  ReminderRepo, AuditRepo. Method signatures are the contract; no silent additions.
- DB schema: defined in migrations/. Schema change = new goose migration
  (.up.sql + .down.sql) + ADR if non-trivial.
- callback_data format: "<action>:<id>" (e.g. done:42, edit:42, snooze:42:3h).
  Actions are a closed set; new actions get reviewed.
- Env keys (in .env.example, single source of truth): BOT_TOKEN, DB_PATH, TZ,
  DIGEST_TIME, REMINDER_OFFSETS, LOG_LEVEL, ENV.
  Adding a key requires updating .env.example, README, and this doc.
- Log fields: event, group_id, assignment_id, user_id, err.

## 7. Design decisions (see ADR)

- ADR-0001: SQLite via modernc.org/sqlite (pure-Go) behind a repository
  interface; swap to Postgres later if load demands it. Rationale: single
  binary, CGO_ENABLED=0, trivial Docker build, one moving part on a single VPS.
- ADR-0002: No web admin in MVP; admin via bot commands. Mini App later.
- ADR-0003: Attachments stored as Telegram file_id, not re-uploaded.
- ADR-0004: Scheduler is in-process (no external queue) for MVP.
- ADR-0005: Migrations embedded into the binary (embed.FS + goose.Up at startup).
  No separate migration container. Rationale: simplest deploy story on VPS.
- ADR-0006: No in-repo CI. Image is built and pushed to the registry outside
  this repository; VPS deploy is `docker compose pull && docker compose up -d`.

## 8. Deployment

- Runtime: docker compose on a VPS.
- Image: multi-stage build (CGO_ENABLED=0), distroless/alpine final stage,
  single binary.
- Config: .env next to docker-compose.yml.
- Volume: ./data/bot.db mounted to DB_PATH.
- Upgrades: docker compose pull && docker compose up -d. Migrations run at startup.
- Rollback: pin previous image tag in compose.
- Logs: docker compose logs -f; slog JSON in prod.
- Backups: periodic `sqlite3 .backup` of bot.db (cron on host).

## 9. Extensibility points

- Mini App / web admin (over the same service layer).
- Multiple groups/cohorts — group_id is already threaded through.
- Google Calendar / ICS export (service.ExportICS).
- Swap SQLite -> Postgres via the repository interface (ADR-0001).

## 10. Non-goals

- Grades, transcripts, LMS features.
- Storing patient or clinical data.
- Multi-tenant SaaS.
- Real-time chat.
- In-repo CI pipelines and deploy automation (out of scope for this repo).

## 11. Observability

- Structured logs via slog (JSON in prod).
- Startup config dump (redacted).
- Scheduler tick log with counts.
- No metrics backend in MVP; leave interface for future.

## 12. How to update this doc

- Small edits: PR with `docs:` prefix, reviewed by Lead.
- Architectural shifts: add a new ADR under docs/adr/NNNN-title.md, then update
  this file. ADRs are append-only.