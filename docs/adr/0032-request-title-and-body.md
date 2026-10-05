# ADR-0032: Access request titles and explanatory bodies

- **Status:** Accepted
- **Date:** 2026-10-03

## Context

Reviewers need a short request title and a separate explanation of purpose, impact and review context before reading SQL. SQL alone does not describe the requested work. The request workflow remains inside reloadable pages (ADR-0022).

## Decision

Add a single-line title of at most 200 Unicode code points and an optional plain-text body of at most 4,000 code points. New requests in the UI require a title; legacy requests and API clients that omit narrative fields remain valid and display an untitled fallback. Reject nonempty whitespace-only titles, invalid UTF-8 and NUL; titles reject line breaks.
Include narrative bytes in the existing 56 KiB payload budget.

Keep the title in the scoped list projection and also in the sealed payload. Titles are visible request metadata; the form explains that secrets belong in neither titles nor descriptions. The body is encrypted together with SQL and parameters and returned only by the existing authorized Get operation. Never copy narrative text into audit metadata or logs.
Render both as framework-escaped text, preserving body line breaks without executing HTML or adding Markdown dependencies.

Creation and optimistic draft replacement save title, body, SQL and parameters together. Submission freezes all of them; changes require a new approval unit. Authenticate the narrative in the approval digest when present, using optional canonical JSON fields so existing empty-narrative v2 approvals remain verifiable. Execution recomputes the digest using the persisted title and decrypted body.
Missing narrative fields in old sealed documents decode to empty strings. No new visibility permission or lifecycle state is introduced.

## Consequences

A title column migration uses an empty default for historical rows and a database length/line-break constraint. Plain titles can contain sensitive data if users disregard the guidance, so preserve organization/requester/reviewer scopes and never add them to audit exports.
Test narrative persistence, limits, stale edits, post-submission immutability, encrypted round trips, digest changes and literal HTML display through existing scenario boundaries.

## Primary sources (verified 2026-10-03)

- [OWASP Transaction Authorization](https://cheatsheetseries.owasp.org/cheatsheets/Transaction_Authorization_Cheat_Sheet.html): authorization binds the reviewed data and protects it against later modification.
- [OWASP Cross Site Scripting Prevention](https://cheatsheetseries.owasp.org/cheatsheets/Cross_Site_Scripting_Prevention_Cheat_Sheet.html): use framework escaping for untrusted text rather than HTML sinks.
