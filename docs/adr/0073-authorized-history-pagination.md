# ADR-0073: Authorized history pagination beyond candidate limits

- **Status:** Accepted
- **Date:** 2026-10-18
- **Partly supersedes:** [ADR-0071](0071-binding-authorization-activation.md)'s total-candidate ceiling for audit and request lists; its other authorization boundaries remain binding.

## Context

Audit events and requests accumulate during ordinary operation. Refusing every page after 1,024 candidates makes the default installation unusable without any change in policy. Raising the ceiling merely postpones that failure. PRD §4.3 and §7.1 require resource authorization, requester/reviewer privacy, exact authorized totals and consistent page clamping.

## Options

Filtering an SQL page cannot establish authorized totals or page boundaries. Translating the restricted CEL evaluator into SQL would create a second implementation of its language and budgets. Unbounded materialization of all event metadata and sealed request bodies wastes memory. A batched complete scan preserves the existing evaluator and exact pagination while separating resident display data from total history size.

## Decision

For audit and request lists only, interpret `PORTCULLIS_AUTHORIZATION_READ_MAX_CANDIDATES` as the database batch and authorization-cache size, retaining its default 1,024 and ceiling 65,536. It no longer caps accumulated history. Other collections and session capability discovery retain ADR-0071's total-candidate ceiling; this decision does not qualify or change them.

Acquire the existing shared organization policy coordinator in a Repeatable Read transaction. Scan every candidate batch in the requested timestamp/UUID order. Evaluate each trusted row's permission and CEL condition, plus request ownership or current resource-specific approve/reject authority. Use the explicit post-lock database instant for effective-state filtering, displayed state and approval validity. Count only visible rows, retain the requested page and one last-page fallback, and clamp after the complete scan. Load current approval counts only for the returned request page; they do not determine lifecycle filtering or visibility.

Reuse membership/revision and current grants within the scan, caching by permission and verified connection ancestry with a bounded cache. Cached grants never cache a condition's outcome: evaluate every resource ID and every requested permission separately using the existing pure evaluator and unchanged per-decision Binding/CEL limits. Source ancestry comes exclusively from organization-scoped database rows.

Before unary output, reacquire the shared policy coordinator and read current membership, revision and database time. Resolve the existence and ancestry of every resource that contributed to the authorized count or page in bounded SQL batches, reevaluate its required permissions and conditions, and reject changed revision, ancestry, revocation or expiry. Counts require witnesses for off-page resources too. Check membership/revision even for an empty result. The original Repeatable Read snapshot can predate a policy-lock wait; its result is never output authority until this fresh fence succeeds. Retain the existing final session authentication fence. New audit appends need not invalidate a still-authorized prior snapshot.

The existing read deadline (default five seconds, ceiling thirty seconds) covers the complete scan, including lock waits, SQL and condition work. Output has its own equally bounded fresh check. Errors, cancellation, timeouts and per-resource Binding overflow return no partial items or totals. Do not increase defaults, bypass conditions or treat a transient authorization failure as a successful prefix.

## Consequences

Display-row memory is proportional to batch size plus page size and grant-cache size is bounded. Authorization witnesses remain proportional to the visible resource/permission count so off-page totals can be fenced. Exact counting, OFFSET traversal and pure evaluation still cost work proportional to history, with repeated SQL count/offset work at deeper batches; the deadline remains an operational limit, not a capacity certification. Future indexed policy-aware reads may reduce this cost without changing the observable contract.

Real PostgreSQL scenarios require more than 1,024 audit events and requests under organization, connection, conditional and own-request grants; both sort directions, deep-page clamping, false conditions and list-without-reviewer privacy; small batches, concurrent appends/state changes, cancellation/deadline and Binding overflow; and revocation, revision changes, ancestry changes and actual time expiry before output. Required repository and supply-chain gates precede commit. M1's other outstanding acceptance obligations remain separate.

## Sources

- [PostgreSQL 18 transaction isolation](https://www.postgresql.org/docs/18/transaction-iso.html), verified 2026-10-18: successive Repeatable Read queries share a snapshot; Read Committed statements can observe different committed data.
- [OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html), verified 2026-10-18: deny by default, validate permissions on every request, and enforce authorization on the server.
