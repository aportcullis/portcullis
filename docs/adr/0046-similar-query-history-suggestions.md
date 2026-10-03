# ADR-0046: Similar-query history suggestions

- **Status:** Accepted (implementation pending)
- **Date:** 2026-10-04

## Context

The owner identifies discovery of similar past queries as a core reuse feature: show the request title, author and time while composing SQL, with separate actions to visit its history or use its query. Saved-query browsing alone does not address discovering earlier team work during composition. This belongs to Core 2 / Library (M3); it is planned and not implemented by this decision.

## Decision

Show an inline Similar queries section beside or below the SQL editor, without a modal or automatic replacement. Start with five ranked suggestions and bounded, paginated expansion. Each suggestion shows title, author, connection, explicit request/execution status, submitted time and execution time when recorded. Display an explanation such as same query structure or same referenced tables rather than an invented confidence percentage. Missing facts remain explicit; a matching request is not evidence of successful execution or retained results.

Provide two independent actions: View history opens the authorized request detail page; Use this query fills the current draft editor with authorized SQL and parameter definitions. Do not silently copy parameter values, change connection, overwrite title/body, submit, execute or inherit approval. Applying a suggestion preserves an undoable previous editor state; navigating to history preserves the current composition for return. A source request's state, payload and audit evidence are never mutated. Show when a historical request used a different connection config version; query reuse must be validated against the current target and policy through a fresh submission. Revalidate source permissions and selected connection/config before delivering reusable SQL; stale or revoked sources fail inline without erasing the draft.

Limit initial candidates to submitted history in the current organization and selected connection/dialect, authorized through the existing payload-read/ownership rules. Do not expose other users' private drafts, candidate titles, authors, counts or matching explanations without source permission. Organization-shared saved versions may participate when the Library visibility contract is implemented. Recheck authorization on each detail and reuse action; do not infer access from a previous suggestion response. Session, connection and editor revision changes fence responses and clear stale suggestions.

Use dialect-aware structural matching in Portcullis metadata: ignore formatting/comments and normalize literal differences for comparison, then rank matching structure before looser same-object/class candidates and use recency as a deterministic tie-breaker. Similarity is not semantic equivalence, a safety verdict or authorization. A parser failure while typing yields no structural suggestion and never blocks editing. Do not query target databases, require pg_stat_statements, execute EXPLAIN or use an external AI service for matching. Bound request size, candidate work, response size and latency; debounce input and cancel superseded searches. Carry SQL in a protected request body, not URLs or logs. Retain encrypted originals and use versioned keyed structural signatures within organization scope for exact-shape indexing; never persist plaintext SQL, literal values or public low-entropy hashes in a search index. Validate index/ranking design and resource budgets with actual scoped workloads before release.

Use ordinary labeled links/buttons and a bounded list for the two-action history cards. Preserve editor focus while suggestions refresh; keyboard and screen-reader users can reach each action. Do not give rich cards with multiple buttons a listbox-option role. If editor-native completion is added later, apply the corresponding accessible completion pattern independently.

## Acceptance

Scenario TDD must reproduce discovery of differently formatted/literal-valued queries, correct title/author/timestamp/status, deterministic ranking and bounded expansion. Cover inaccessible or cross-organization sources, private drafts, connection/config changes, malformed partial SQL, revocation between suggestion and reuse, session change and late search responses. Browser scenarios must show that history navigation preserves composition, reuse affects only the intended editor fields, undo restores prior input, parameters need fresh values and submitting creates a new governed request. Search/index tests must verify no literal/comment secrets in index/log output and no target SQL execution. These scenarios have not been run yet; implementation and release gates remain pending.

## Sources

- [PostgreSQL pg_stat_statements](https://www.postgresql.org/docs/18/pgstatstatements.html): structural query grouping is useful precedent, but representative text can retain literals and query IDs are not stable across major versions; its target-side statistics are not a Portcullis history or authorization source.
- [OWASP authorization guidance](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html): validate permission on each request and scope access to the resource.
- [W3C combobox pattern](https://www.w3.org/WAI/ARIA/apg/patterns/combobox/): distinguish suggestions from automatic completion and preserve the keyboard interaction contract; rich history cards here use a normal action list.
