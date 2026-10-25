# ADR-0008: Telegram MVP, persistent dialogs and durable delivery

Status: Accepted by the user on 2026-10-25 as part of the MVP plan.

## Context

The identity bootstrap needs learning workflows and scheduled notifications.
The user selected Telegram commands/buttons, personal reminders and group-only
digests. SQLite stays inside the bot container with a persistent volume.

## Decision

Keep service interfaces in `internal/service`; place all compound writes in a
single UnitOfWork transaction. Recheck actor role, group and entity version
for each operation. Subjects and assignments share a composite foreign key.

Persist typed dialog drafts in SQLite. Each new dialog gets a fresh ID;
updates require its owner and revision. Terminal states clear the payload.
The TTL is 30 minutes, extended by each successful step. Publication and
dialog completion share one transaction.

Use an in-process scheduler and a SQLite outbox. Planning is idempotent;
one delivery is claimed with a two-minute token lease. Telegram HTTP runs
outside transactions. Success is recorded together with reminder/digest
progress; stale leases cannot acknowledge a later claim.

Revalidate notification data before sending. Personal delivery respects
membership, completion, settings, quiet hours, versions and deadline. Group
digests contain today's deadlines without personal marks, and have separate
receipts per part. Retry up to five attempts and honor Telegram retry_after.
Share a cancellable limiter between background and interactive messages.

## Consequences

No external cron, broker, HTTP API or additional container is needed.
Pending work and dialogs survive restart. Confirmed events are deduplicated;
a crash after Telegram accepts a message and before SQLite commits its receipt
can duplicate a message. A concurrently accepted HTTP request cannot be revoked.

Existing migration 0001 remains unchanged. New migrations 0002/0003 implement
the approved model. No production rollout or data rollback is performed by agents.

Plan: [2026-10-25-mvp.md](../plans/2026-10-25-mvp.md).
Contracts: [mvp.md](../contracts/mvp.md).
