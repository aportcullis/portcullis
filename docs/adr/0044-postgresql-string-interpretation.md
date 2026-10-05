# ADR-0044: Pin PostgreSQL string interpretation

- **Status:** Accepted
- **Date:** 2026-10-03

## Context

The PostgreSQL parser, binder and redactor assume standard-conforming ordinary string literals. A target database or role can override PostgreSQL's default and treat backslashes as escapes, so the server can execute a different expression tree from the approved and classified SQL. Read-only transactions do not prevent all function side effects. The owner-supplied review identifies this mismatch.

## Decision

Set `standard_conforming_strings=on` as a startup parameter on every PostgreSQL execution connection, including governed execution. Check the server's reported ParameterStatus immediately after connecting; close and reject the connection before BEGIN, catalog validation or user SQL unless the reported value is exactly `on`. Never fall back to a different interpretation or retry user SQL.
Existing prohibitions on session mutation remain in force.

Verify the contract on PostgreSQL 16–18 with an owned database whose default is explicitly `off`. PostgreSQL 19 no longer permits `off`; assert the expected feature-not-supported SQLSTATE and a fresh connection reporting `on` instead. Determine the branch from the actual server version, not the requested test family, and fail on unexpected errors or settings.
In both cases, an ordinary backslash-containing string followed by a comment must retain the classifier's one-column meaning rather than expose a hidden second expression. Use a harmless constant expression for regression coverage, not a process-control function. Other engine string modes require independent adapter decisions and qualification before product registration.

## Consequences

Approved SQL interpretation is independent of inherited target role/database defaults. Unsupported or contradictory server reports fail closed, while legacy target defaults remain usable through the explicit startup setting. This does not broaden allowed syntax, function privileges or database-version qualification.

## Source

- [PostgreSQL 19 development compatibility settings](https://www.postgresql.org/docs/devel/runtime-config-compatible.html): `standard_conforming_strings` remains inspectable but is always `on` and cannot be set to `off`.

- [PostgreSQL string compatibility settings](https://www.postgresql.org/docs/current/runtime-config-compatible.html): ordinary string handling depends on `standard_conforming_strings`, and clients can inspect its reported setting.
