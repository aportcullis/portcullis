# M3 · Library

**Not started — completes the MVP after Bridge.**

Turn useful SQL into reusable assets: saved queries, versions, favorites, organization sharing, typed parameters, and execution history. Reusing a query creates a new governed request rather than inheriting an earlier approval.

**Discover while composing:** Inline similar-query history suggestions show authorized titles, authors, timestamps and status, with separate View history and Use this query actions. Preserve composition during history navigation and provide undo for filling SQL; parameter values and previous approval are not inherited. Match within the selected connection/dialect and current permissions, and disclose historical target-config changes. Search requires neither target execution nor external AI. This core reuse flow remains planned; see [ADR-0046](../../adr/0046-similar-query-history-suggestions.md).

**Also deliver:** Immutable Git migration artifact → status → SQL dry-run preview → deterministic impact facts, with source/observation time, estimates and unknowns. This preview slice exposes no migration apply endpoint; apply follows Forge.

**To complete:** Similar-history discovery must pass visibility/revocation, stale-search, bounded-ranking, keyboard, draft-preservation and fresh-approval scenarios. Saved-query workflows and the result grid must work across both committed databases under the PRD's ownership, sharing, versioning, and approval rules. Schema preview must pass artifact/target pinning, permission, audit, bounded-output and per-DB object acceptance. Core 2 scope remains subject to the product-validation work in [PRD §1.4](../../product/prd.en.md#14-problem-validation).

**Release boundary:** Gate is the first PostgreSQL alpha. Library, including Bridge, is the MVP.
