# M3 · Library

**Not started — completes the MVP after Bridge.**

Turn useful SQL into reusable assets: saved queries, versions, favorites, organization sharing, typed parameters, and execution history. Reusing a query creates a new governed request rather than inheriting an earlier approval.

**Discover while composing:** Inline similar-query history suggestions show authorized titles, authors, timestamps and status, with separate View history and Use this query actions. Preserve composition during history navigation and provide undo for filling SQL; parameter values and previous approval are not inherited.
Match within the selected connection/dialect and current permissions, and disclose historical target-config changes. Search requires neither target execution nor external AI. This core reuse flow remains planned; see [ADR-0046](../../adr/0046-similar-query-history-suggestions.md).

## Keycloak sign-in

Add optional company SSO through an operator-configured Keycloak realm after the reusable-query foundation. Authenticate through standard OIDC server callbacks and issue the existing HttpOnly session; Portcullis continues to own users, roles and request authorization.
Administrators explicitly bind the configured issuer and subject to an existing account. Do not auto-create users, link by email alone or import Keycloak roles/groups as permissions. Local administrative recovery remains available.

**Acceptance:** A pinned real Keycloak fixture verifies successful login, wrong issuer/audience, invalid or expired tokens, state/nonce/PKCE and replay rejection, denied account linking, disabled users, local logout/session revocation and provider failure. Existing Google/local login, CSRF and cross-organization checks still pass.
Provider tokens never enter browser storage. Keycloak logout or disabling a Keycloak account does not automatically revoke a Portcullis session; upstream revocation synchronization requires a separate decision. LDAP, SAML, SCIM, additional providers and group-role synchronization remain outside this slice.
Per [ADR-0057](../../adr/0057-keycloak-oidc-in-mvp.md), this is M3 scope; M2 retains its database, deployment and SQL-review sequence.

## Request discussion

Provide request comments and replies with author, timestamp and append-only history. Discussion stays separate from approval decisions and cannot change submitted SQL or execute a query.

**Acceptance:** Recheck request visibility and commenting permission; reject forged authorship, cross-request replies and revoked access. Render text safely and bound comment input and retrieval. Structured SQL review suggestions remain post-MVP.
Per [ADR-0060](../../adr/0060-request-comments-and-replies.md), basic discussion belongs to M3.

## Review inbox and profile personalization

Show the current user's actionable pending reviews as a set of request cards with manual previous/next navigation and a link to the complete review list. Each card presents the request context and approval state and opens the governed detail page for a decision.
Keep actionable review counts distinct from unread discussion counts; a generic pending-list count is not a personal notification count.
Display a pending-review badge and requester status notifications so users can see work waiting for them. Counts and cards follow current organization, visibility and reviewer eligibility; revoked access removes both content and counts.
Users can configure their own profile image. Google sign-in uses the verified account's available profile photo; local accounts and unavailable photos fall back to a stable generated avatar. A chosen image remains consistent across reloads and sign-in sessions and can be reset to the generated default.

**Acceptance:** Cards are keyboard-accessible and never auto-advance while being reviewed. Empty, loading and unavailable states are explicit; badge counts agree with the authorized pending-review list, and opening a card does not approve a request or mark unrelated notifications read.
Profile changes cannot alter another user's account, expose credentials or allow unsafe images; unavailable Google photos retain the local fallback without blocking sign-in.
These supporting MVP features follow the reusable-query foundation and are separate from M1's account menu and CSV dialog.

**Also deliver:** Immutable Git migration artifact → status → SQL dry-run preview → deterministic impact facts, with source/observation time, estimates and unknowns. This preview slice exposes no migration apply endpoint; apply follows Forge.

**To complete:** Similar-history discovery must pass visibility/revocation, stale-search, bounded-ranking, keyboard, draft-preservation and fresh-approval scenarios. Saved-query workflows and the result grid must work across both committed databases under the PRD's ownership, sharing, versioning, and approval rules.
Schema preview must pass artifact/target pinning, permission, audit, bounded-output and per-DB object acceptance. Core 2 scope remains subject to the product-validation work in [PRD §1.4](../../product/prd.en.md#14-problem-validation).

**Release boundary:** Gate is the first PostgreSQL alpha. Library, including Bridge, is the MVP.
