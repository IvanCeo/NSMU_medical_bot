# ADR-0009: Telegram navigation and callback error handling

Status: Accepted by the user on 2026-10-25 as part of the snooze/navigation plan.

## Context

Snooze persisted a reminder and then edited an unchanged task card. Telegram's
400 message-is-not-modified response was rendered as a temporary failure.
The callback defer overwrote that failure with the successful answer result,
preventing the polling loop from logging it. Button navigation also sent a
new message for each screen, leaving previous dialog keyboards visible.

## Decision

Confirm snooze through answerCallbackQuery without editing the unchanged card.
Treat only the recognized edit-no-op as success. Keep other Telegram errors,
and log callback handling and callback-answer failures independently with safe
categories, operation names and numeric codes. Never log raw error descriptions,
callback payloads, message text or token-bearing URLs.

Reuse the callback source message for the first rendered screen through a
request-scoped target in context. Further parts and files use new messages.
Explicitly send an empty keyboard when a step has no buttons. Fall back to a
new message only when the source is missing or cannot be edited.

Use the existing subject callback to return from a task card. Other screens
offer /start; dialogs also offer /cancel. A plain /start cancels the active
dialog before rendering the main menu. Reuse the current dialog revision checks.
No navigation stack, table, service method, env key or callback format is added.

## Consequences

Most inline navigation stays in one message, and obsolete dialog keyboards are
replaced. Commands, typed replies, files and long-text continuations still create
messages. Existing history is retained. Navigation has no state across updates.
The current business authorization and dialog concurrency checks remain effective.

Plan: [2026-10-25-snooze-navigation.md](../plans/2026-10-25-snooze-navigation.md).
