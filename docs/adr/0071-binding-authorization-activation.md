# ADR-0071: Atomic activation of resource Binding authorization

- **Status:** Partly superseded by [ADR-0073](0073-authorized-history-pagination.md) for audit/request lists and [ADR-0074](0074-session-capability-discovery-and-recovery.md) for session capability discovery and metadata failures
- **Date:** 2026-10-18
- **Implements:** [ADR-0062](0062-role-bindings-and-conditions.md) and [ADR-0064](0064-binding-schema-preparation-and-activation.md)'s coordinated activation.
- **Refines:** [ADR-0066](0066-resource-binding-authorization-snapshots.md)'s runtime snapshots and result identity.
- **Supersedes:** ADR-0067–0070's inactive production boundary; their management, delegation and source-preservation rules remain binding.

## Context

Prepared Binding persistence, restricted CEL evaluation, administration services and UI cannot safely become a second grant source alongside membership roles. Bootstrap, authentication metadata, workflow admission and protected output must switch with the migration. Existing grants, execution history and an unconditional recovery administrator must survive the transition.

## Decision

### Migration and readiness

Migration 0025 locks organizations and the organization policy coordinators before policy tables. Refuse nonempty owner-seeded preparation policy, copy each current membership role into exactly one unconditional organization Binding and validate both assignment mapping and effective-grant equivalence. Add the five Binding management keys explicitly to the system bootstrap-default role. Refuse populated organizations without an active recovery administrator holding the complete current management catalog; do not expand an arbitrary custom role.

Membership becomes independent of role assignment. Retain the old nullable `role_id` column as an always-NULL CHECK-constrained tombstone rather than destructively dropping it in this change. It cannot grant authority or accept an old assignment writer. No runtime path unions old and new grants or writes both models. Older binaries remain unsupported against the migrated schema; rollback requires the matching binary and database backup. Runtime readiness requires the validated retirement constraint, NULL legacy slots and positive organization revisions.

Bootstrap creates account, password, independent membership, selected Role Binding, revision and audit evidence atomically. Runtime privileges permit Binding head SELECT/INSERT/UPDATE, immutable version SELECT/INSERT and revision SELECT/UPDATE; DELETE and version UPDATE remain forbidden. Revision creation remains owner-controlled.

Consume interactive bootstrap tokens only after acquiring the organization coordinator; a token that expires during that wait cannot create an account. Bootstrap and login creation/audit timestamps use the database instant after their required locks.

### Trusted purpose and current decisions

The composition root injects the restricted CEL service into one current-policy evaluator. Protected adapters and the active authorizer have no legacy permission resolver. A typed, server-owned RPC registry selects the permission, resource kind and protobuf identity field. Context carries that authenticated purpose, never browser-supplied ancestry, actor identity or an authority claim. PostgreSQL resolves ancestry before any decryption. Malformed identities retain permission-denied responses for callers without potential authority; admitted callers receive a generic invalid-argument response without resource lookup.

An execution's identity remains its request ID. Result identity and connection ancestry come from durable execution/request metadata, so cache eviction cannot erase durable execution history. This resource reference does not establish result availability: the result reader separately checks current ownership and cache TTL before loading an envelope or chunk and immediately before output. Missing or ambiguous durable ancestry refuses authorization.

### Policy and workflow ordering

Use the existing class-5 organization coordinator before connection, request, execution or class-2 session locks. Policy changes and workflow admissions acquire it exclusively; coherent protected reads acquire it shared in READ COMMITTED transactions before reading resources and authoritative database time. This avoids an older repeatable-read snapshot being established before a policy-lock wait. Never hold metadata locks across target SQL, credential validation, browser output or other external I/O.

Approval and rejection check the action's current resource grant and CEL under the coordinator and request lock. Count distinct recorded non-self approvals by evaluating each approver's current `requests.approve` grant against that request at the decision instant. Execution acquisition repeats requester execution permission, current quorum, expiry, policy/configuration and digest checks. Heartbeats stop continuing execution when requester permission or current quorum disappears. Terminal completion and reconciliation remain possible so revocation cannot suppress outcome evidence or authorize replay.

Session rotation acquires the organization coordinator before the current membership and user-session coordinator. Require an active independent member before inserting a password or OIDC session. Existing account and policy services retain complete delegation, self-change refusal, recovery protection, atomic audit and affected-session revocation.

### Coherent collections and bounded output

Connection lists, requestable targets, request lists and audit pages evaluate the complete bounded candidate set before producing items, totals or page clamps. Request ownership or a current resource-specific reviewer grant remains an additional privacy rule. Never filter an already paginated SQL result or return a successful prefix after candidate overflow.

Request lifecycle filters, effective-state badges and approval validity use the same post-lock database instant. Do not filter on transaction-start `now()` after a policy-lock wait; a request may have expired while the reader was waiting.

Use `PORTCULLIS_AUTHORIZATION_READ_MAX_CANDIDATES` (default 1024, ceiling 65536) and `PORTCULLIS_AUTHORIZATION_READ_TIMEOUT` (default 5 seconds, ceiling 30 seconds) for coherent scans, separate from per-permission binding and CEL budgets. The read deadline covers lock waits, queries and evaluation; cleanup is also bounded. Capacity beyond the qualified scan budget refuses the whole read and requires an explicit operator budget or a later indexed policy-aware read design. These defaults are operational bounds, not a production capacity claim.

Preserve cancellation and deadline identity through active read and admission adapters. Snapshot unavailability and candidate overflow remain operational refusals; do not replace them with a successful partial response or a generic internal-error contract. Unknown permission keys remain server defects.

Management details and collections use the same shared coordinator, bounded context and database instant as their authorization snapshot. Their output checkpoints retain the actor, resource, permission and revision, so neither a revision change nor condition expiry can release stale protected metadata.

Before releasing a protected unary response, re-evaluate the disclosed resource checkpoints, reject a changed policy revision and authenticate the session again, including database-clock session expiry validation. Result lifetime has an additional fresh ownership/TTL fence. CSV checks the current result policy, owner and TTL for each at-most-16-KiB send; streaming session authentication and database-clock validity checks repeat on each send. Revocation stops subsequent output without promising to undo SQL or bytes already delivered.

### Session and SPA contracts

Login and Me expose separate evaluated navigation capabilities, caller-owned Binding summaries with scope/condition presence and an authorization revision. Compatibility `permissions` contains only unconditional organization grants; the obsolete single-role label is empty. Capability discovery scans only potentially held permission kinds, so unrelated audit history cannot invalidate a scoped requester's login metadata. These remain advisory values; every protected operation uses a fresh server decision.

The SPA uses capabilities for navigation, displays multiple Binding scopes and conditions, and fences protected caches and page/form lifetimes by identity, authorization revision and capabilities. Management routes use the active services; remove the mandatory pending interceptor rather than adding an activation feature flag. UI contract fixtures remain separate from real-server authorization evidence.

## Qualification and limits

Migration scenarios cover preserved current/custom/disabled/changed assignments, empty bootstrap and atomic refusal of mixed or unrecoverable policy. Real PostgreSQL/CEL scenarios cover organization and connection grants, additive bindings, current conditions, denied decisions without side effects, fresh requester/quorum checks at acquisition and heartbeat, capability compatibility, output revocation, revision changes, bounded scans and lock deadlines. Existing distinct-reviewer, ownership, single-execution, immutable audit and last-administrator race scenarios remain required.

The complete repository gates, staged review and observed red/mutation failures must be recorded before commit. Activation does not by itself complete M1: one-time password setup consumption, remaining result-navigation/export/account UX acceptance and release admission retain their own PRD acceptance requirements and evidence boundaries.

## Sources

- [Google IAM policy model](https://cloud.google.com/iam/docs/policies) and [IAM Conditions overview](https://cloud.google.com/iam/docs/conditions-overview) separate role bindings from conditions and evaluate applicable grants additively.
- [OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html) requires server-side, per-request authorization and deny-by-default behavior.
- [PostgreSQL explicit locking](https://www.postgresql.org/docs/current/explicit-locking.html) defines transaction advisory locks, shared/exclusive modes and lock ordering considerations.
