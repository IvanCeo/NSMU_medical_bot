# AGENTS.md

Instructions for AI agents working in this repository.
Humans: see README.md and ARCHITECTURE.md.

> Companion docs
> - ARCHITECTURE.md — system design, layers, data model, contracts.
> - docs/adr/ — architecture decision records (immutable, append-only).
> - Nested AGENTS.md in subdirectories may add stricter rules; the closest file wins.

---

## 1. Project overview

Medical bot (@NSMU_medicial_bot) — Telegram-бот для студентов-медиков: домашние
задания, контрольные, тесты, дедлайны, напоминания и дайджесты в группу.
Заменяет «пишем ДЗ в чат группы».

- Language: Go 1.27.1 (pinned in go.mod)
- Telegram: github.com/go-telegram-bot-api/telegram-bot-api/v5
- DB: SQLite via modernc.org/sqlite (pure-Go, CGO_ENABLED=0)
- Migrations: goose, embedded into the binary via embed.FS
- Config: .env (single source of truth for env keys; .env.example committed)
- Deploy: docker compose on a VPS, image pulled from a container registry
- Code/tests/commits: English. UI strings and user docs: Russian.

Non-goals (MVP): web admin, LMS grades, storing any patient / clinical (PHI)
data, multi-tenant SaaS, in-repo CI (build+push are done outside this repo).

---

## 2. Quick start

    make setup     # install tools, copy .env.example -> .env
    make run       # run bot locally
    make test      # unit tests
    make test-race # race detector
    make lint      # gofmt + go vet + golangci-lint
    make migrate   # apply migrations locally
    make build     # build binary

If a Makefile target is missing — do NOT invent hidden commands. Add the target
and document it in Makefile + README.md.

---

## 3. Repository layout

    cmd/bot/              entrypoint (thin: config, wiring, graceful shutdown)
    internal/
      bot/                Telegram transport: handlers, router, keyboards, callbacks
      service/            use cases (business logic) + interfaces it consumes
      repository/         SQLite implementations of service interfaces
      domain/             entities, value objects, sentinel errors
      scheduler/          in-process tickers (reminders, digests)
      config/             env parsing
    migrations/           goose SQL migrations (source of truth for schema)
    docs/adr/             architecture decision records
    test/                 integration helpers

Rules:
- main is thin: no business logic.
- Dependencies point inward: bot -> service -> repository/domain. domain has no deps.
- Interfaces are defined in the consumer package (service), not in repository.
- No direct Telegram API calls outside internal/bot.
- No SQL outside internal/repository and migrations/.
- Migrations are embedded via embed.FS and applied with goose.Up at startup.

---

## 4. Roles & delegation (Codex in two hats)

Codex acts in two role-scoped modes over the course of a task. Phase gates are
mandatory — a phase cannot be skipped just because the same agent can do both.

Lead (architect / planner / integrator):
- Owns architecture, planning, contracts, integration, final review.
- Produces Lead artifacts before implementation starts:
  - the contract(s) being touched (service interfaces, DB schema, callback_data,
    env keys, message shapes);
  - a short plan / task breakdown;
  - acceptance criteria.
- Approves contracts and cross-cutting decisions.
- Reviews Executor output against the plan.

Executor (backend implementer):
- Implements strictly per the approved contract and plan.
- Works only within its competence (Go / SQLite / migrations / Dockerfile);
  escalates ambiguity instead of guessing.
- Returns: summary, diff, test results, blockers, skipped tests.

Phase gates (hard rules):
1. Lead -> human approval -> Executor. No implementation before the contract
   and plan are approved by a human.
2. Executor -> Lead review -> human merge. No merge to main without a human.
3. Contracts must not be edited by the Executor unilaterally.
4. Destructive actions require explicit human confirmation: drop table,
   force push, delete/rename a migration, rotate secrets, touch prod data,
   edit docker-compose.prod.yml.

---

## 5. Coding conventions

- gofmt -s, go vet, golangci-lint run must pass.
- Errors: wrap with %w; sentinel errors live in internal/domain.
- context.Context is the first parameter of anything doing I/O.
- No global mutable state; wire via constructors in main/cmd.
- Logging: log/slog (JSON in prod, text in dev). No fmt.Println in services.
- Time: store UTC; render in user TZ from TZ.
- Naming: code in English, user-facing strings in Russian.
- Tests: table-driven; TestFuncName_Scenario.
- Comments: only when "why" is non-obvious.
- Keep functions <= ~50 lines, packages small. Prefer many small packages over
  one big.

---

## 6. Database

- Schema is defined only by migrations/ (goose). No CREATE TABLE in code.
- Every migration has .up.sql and .down.sql.
- Naming: snake_case, plural table names (assignments, users).
- Foreign keys enabled on every connection (PRAGMA foreign_keys = ON).
- Timestamps: TEXT in RFC3339 UTC. Pick once, stay consistent.
- Indexes for hot queries: assignments(group_id, deadline_at),
  assignments(subject_id), reminders(send_at) WHERE sent_at IS NULL.
- Test data only. Never put real personal or medical data in repo or tests.

---

## 7. Testing

- Unit tests: internal/service (fake repo, fake TG client).
- Integration tests: internal/repository against a temp SQLite file.
- go test ./..., go test -race ./...
- Coverage target: >= 70% in internal/service.
- Report which tests were not run and why.

---

## 8. Security & secrets

- Never commit secrets (.env, tokens, keys).
- Never deploy and never modify production data from an agent session.
- Never auto-merge to main.
- Confirm with a human before: destructive SQL, force push, deleting data,
  changing IAM / API keys, editing docker-compose.prod.yml.
- Store only Telegram ID, display name, group. No PHI, no real patient data.

---

## 9. Commits, branches, PRs

- Conventional Commits: feat:, fix:, chore:, docs:, test:, refactor:.
- Branches: feat/<short>, fix/<short>, chore/<short>.
- PR must contain: what, why, how tested, plus the checklist below.
- Keep PRs focused (one logical change).

---

## 10. Definition of Done (agent checklist)

[ ] gofmt -s -l . clean, go vet ./... clean, golangci-lint run clean
[ ] Tests added/updated; all pass; any skipped test is reported
[ ] Migration added for any schema change (up + down)
[ ] Contracts touched were approved by Lead (human gate)
[ ] Docs updated: README.md, ARCHITECTURE.md, docs/adr/ if a decision changed
[ ] No secrets in diff
[ ] Human reviewer assigned; no auto-merge

---

## 11. Anti-patterns (do not do)

- Calling Telegram API from service or repository.
- Raw SQL in handlers.
- Business logic in main.go.
- panic in library code (except truly unrecoverable startup).
- Silent _ = err.
- God-packages (utils, common, helpers).
- Stateful globals for the bot client.
- Adding CI workflows, deploy scripts, or new services without an explicit task.

---

## 12. Nested AGENTS.md

Subpackages may include their own AGENTS.md with stricter rules
(e.g. internal/repository/AGENTS.md for SQL conventions).
The closest file to the edited code wins, combined with this root file.