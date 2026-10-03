# ADR-0023: Local SQL formatting for editable requests
- **Status:** Accepted
- **Date:** 2026-10-03

## Context

The product owner wants automatic SQL indentation for readable composition. Requests bind exact submitted SQL and parameter values into immutable approval evidence, so formatting must not rewrite submitted payloads or execution input. Formatting is presentation assistance, not SQL validation.

The [SQL Formatter documentation](https://github.com/sql-formatter-org/sql-formatter) documents browser usage and PostgreSQL support. Its [parameter documentation](https://github.com/sql-formatter-org/sql-formatter/blob/master/docs/paramTypes.md) requires explicit `named: [':']` support for Portcullis placeholders and warns that configured parameter types override defaults. Its [dialect API](https://github.com/sql-formatter-org/sql-formatter/blob/master/docs/dialect.md) supports importing only the chosen PostgreSQL dialect.

## Options

- Server formatting: unnecessary transmission and potential approval-unit mutation.
- Formatting on every keystroke: disrupts partially written SQL and caret position.
- Local formatting when leaving an editable SQL field: readable drafts without interrupting typing, with explicit controls and undo.

## Decision

Use pinned `sql-formatter` locally through the PostgreSQL dialect API. Preserve keyword, identifier, function and data-type case; use two-space indentation. Enable both native `$1` and application `:name` placeholders without substituting parameter values. No SQL is sent to a third-party service.

New-request and draft-edit SQL fields default to format-on-blur, with an opt-out checkbox, a manual Format SQL action and a one-step Undo formatting action. Undo disables automatic formatting for that editor session so leaving the field does not immediately reapply it. Subsequent typing invalidates the previous undo snapshot. Format runs synchronously before the following explicit save/submit click; no hidden formatting occurs inside an RPC or after submission. Failed/unsupported formatting leaves the input intact and reports a short inline message. Bound formatting input to 64 KiB; server validation remains authoritative. Conservatively retain SQL containing adjacent string literals (including separators with comments): their newline can carry [PostgreSQL concatenation semantics](https://www.postgresql.org/docs/18/sql-syntax-lexical.html#SQL-SYNTAX-CONSTANTS), so formatting must neither remove an existing newline nor introduce one between invalid adjacent literals.

Submitted/review SQL continues displaying the stored original. It is never silently formatted, persisted again, hashed differently or replaced at execution. Parameters remain in their separate typed draft rows. Store temporary undo content in component memory only and clear it on editor unmount.

## Consequences

Cover PostgreSQL casts, named/native parameters, exact literals and comments, incomplete SQL, formatting failure and undo. Browser scenarios verify automatic formatting, opt-out, undo and saving the displayed draft. Refresh README captures and GIFs with the new controls. Formatting adds a client dependency but does not change backend approval or execution semantics.
