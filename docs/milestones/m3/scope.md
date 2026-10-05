# M3 · Library

**Not started — completes the MVP after Bridge.**

Turn useful SQL into reusable assets: saved queries, versions, favorites, organization sharing, typed parameters, and execution history. Reusing a query creates a new governed request rather than inheriting an earlier approval.

**Discover while composing:** Inline similar-query history suggestions show authorized titles, authors, timestamps and status, with separate View history and Use this query actions. Preserve composition during history navigation and provide undo for filling SQL; parameter values and previous approval are not inherited. Match within the selected connection/dialect and current permissions, and disclose historical target-config changes. Search requires neither target execution nor external AI. This core reuse flow remains planned; see [ADR-0046](../../adr/0046-similar-query-history-suggestions.md).

## Review inbox and profile personalization

Show the current user's actionable pending reviews as a set of request cards with manual previous/next navigation and a link to the complete review list. Each card presents the request context and approval state and opens the governed detail page for a decision.
Display a pending-review badge and requester status notifications so users can see work waiting for them. Counts and cards follow current organization, visibility and reviewer eligibility; revoked access removes both content and counts.
Users can configure their own profile image. Google sign-in uses the verified account's available profile photo; local accounts and unavailable photos fall back to a stable generated avatar. A chosen image remains consistent across reloads and sign-in sessions and can be reset to the generated default.

**Acceptance:** Cards are keyboard-accessible and never auto-advance while being reviewed. Empty, loading and unavailable states are explicit; badge counts agree with the authorized pending-review list, and opening a card does not approve a request or mark unrelated notifications read.
Profile changes cannot alter another user's account, expose credentials or allow unsafe images; unavailable Google photos retain the local fallback without blocking sign-in.
These supporting MVP features follow the reusable-query foundation and are separate from M1's account menu and CSV dialog.

**Also deliver:** Immutable Git migration artifact → status → SQL dry-run preview → deterministic impact facts, with source/observation time, estimates and unknowns. This preview slice exposes no migration apply endpoint; apply follows Forge.

**To complete:** Similar-history discovery must pass visibility/revocation, stale-search, bounded-ranking, keyboard, draft-preservation and fresh-approval scenarios. Saved-query workflows and the result grid must work across both committed databases under the PRD's ownership, sharing, versioning, and approval rules. Schema preview must pass artifact/target pinning, permission, audit, bounded-output and per-DB object acceptance. Core 2 scope remains subject to the product-validation work in [PRD §1.4](../../product/prd.en.md#14-problem-validation).

**Release boundary:** Gate is the first PostgreSQL alpha. Library, including Bridge, is the MVP.
