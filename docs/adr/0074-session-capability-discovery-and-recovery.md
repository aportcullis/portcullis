# ADR-0074: Session capability discovery and metadata recovery

- **Status:** Accepted
- **Date:** 2026-10-18
- **Partly supersedes:** [ADR-0071](0071-binding-authorization-activation.md)'s total resource-candidate ceiling and silent session-metadata fallback; [ADR-0073](0073-authorized-history-pagination.md)'s reservation of that ceiling for capability targets.

## Context

A connection-scoped member can lose every menu when accumulated connections, requests, executions and live results exceed 1,024 candidates. The previous reader checked the combined size before looking for an allowed resource. Login and Me then hid resolution errors behind a successful empty permission view, making ordinary growth or an operational outage indistinguishable from an account with no grants.

## Decision

Discover navigation capabilities as existential resource decisions within the existing shared organization policy coordinator and post-lock database instant. Query only potentially held permission keys. For each supported resource kind, select organization-pinned identity and verified connection ancestry using current Role/Binding scope and permission predicates, ordered by UUID with keyset batches. Apply the same resource evaluator and restricted CEL to every selected row. Stop a permission's search only on a real grant or after exhausting its candidates; a full batch with no match is not proof of absence. SQL scope predicates only narrow candidates and never evaluate CEL or grant authority.

Reuse the transaction-local bounded grant cache from history reads, keyed by permission and trusted connection ancestry. Keep row-specific condition outcomes out of the cache. Preserve unconditional organization compatibility permissions, additive grants, current membership/revision checks and per-resource Binding/CEL budgets. Include execution ancestry from durable request metadata and include result candidates only while their cache entries remain live, as before. Check result ancestry uniqueness for every selected result, including cache hits, using the existing result/organization lookup index; corruption must not select a convenient parent. Reserved permission keys without supported resource kinds yield no new scoped hint.

For capability resource discovery, `PORTCULLIS_AUTHORIZATION_READ_MAX_CANDIDATES` becomes batch/cache size, retaining its default 1,024 and ceiling 65,536. It still caps the complete caller-owned Binding-summary response and other unchanged collections. Retain the complete-read deadline, default five seconds and ceiling thirty seconds, including lock waits, SQL and evaluation. Deadline, cancellation, invalid snapshots and grant/summary overflow return no partial capability snapshot. This removes a fixed resource-population ceiling, not the time or memory budgets.

Add `authorization_unavailable` to LoginResponse and MeResponse. Successful authentication keeps its committed session/CSRF cookies and authenticated identity even if advisory metadata resolution fails; return the marker with no permissions, capabilities, Binding summaries, compatibility role label or revision. A resolved grantless account returns a positive revision with the marker false. Active snapshots with nonpositive revision are invalid. Preserve no-store responses and existing session/CSRF refusal and final session-output checks. Legacy metadata-provider failures use the same explicit marker. Log a bounded reason category without raw driver errors, condition source, credentials or partial metadata.

The SPA treats the marker as a recoverable access-information outage, clears advisory authority and protected caches through the existing authorization identity fence, and renders an explicit retry view. Do not retain stale menus, infer account disablement, show a wrong-password message for a committed login, issue an automatic Login/Logout, or replay credentials. Retry resolves Me using the established cookie; normal focus/online refreshes may also resolve it. Preserve latest-operation fencing so delayed success or failure cannot overwrite newer login/logout or metadata state. Genuine session refusals remain anonymous and transport outages remain distinguishable.

## Consequences

Capability search retains only a candidate batch and bounded current grants, and can stop without counting or reading every resource. A long run of nonmatching conditions still costs work and can hit the unchanged deadline. Binding-summary response bounds remain explicit operational limits. Navigation capabilities remain advisory: every protected RPC separately checks current permission, scope, condition, ownership, reviewer privacy and workflow invariants.

Qualify more than 1,024 resources, scope exclusion, a last-batch condition match, negative conditions, disabled/nonmember callers, live/expired/evicted results and preserved error identities. Raw Connect scenarios distinguish healthy organization/scoped/conditional/grantless metadata from resolver failures and invalid revision, retain cookies/no-store, omit partial data and recover through Me. UI scenarios cover safe state disposal, overlapping responses, real sign-in cookies, explicit retry and no credential replay; injected browser metadata failures establish UI recovery rather than production failure frequency. Repository gates and staged review precede commit. Management-form lifetime and other M1 acceptance remain separate.

## Sources

- [OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html), verified 2026-10-18: deny by default, validate server-side permissions on every request, handle authorization failures safely and log appropriately.
- [PostgreSQL 18 LIMIT and OFFSET](https://www.postgresql.org/docs/18/queries-limit.html), verified 2026-10-18: bounded subsets require deterministic ordering; skipped OFFSET rows still require database work.
- [Connect protocol](https://connectrpc.com/docs/protocol/), verified 2026-10-18: unary responses carry serialized messages and response headers; the explicit metadata marker preserves the successful authentication outcome.
