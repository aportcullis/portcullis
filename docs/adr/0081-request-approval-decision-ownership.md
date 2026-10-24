# ADR-0081: Request approval decision ownership

- **Status:** Accepted
- **Date:** 2026-10-24
- **Refines:** ADR-0018 for domain behavior ownership; its transaction, authorization, time and audit contracts remain in force.

## Context

Request already owns submission, approval-window and state-machine behavior. The PostgreSQL decision transaction still branches directly on rejection and the current eligible approval count. This puts the pure quorum/state decision in the persistence adapter and prevents its invariants from being exercised independently of database setup. Moving transaction coordination indiscriminately would risk current permission checks, lock order, authoritative time and the intentional commit of lazy expiry before returning refusal.

## Options

Keep the pure state/quorum branch in the adapter; move that branch to a Request receiver while retaining the existing consumer-owned repository port; or introduce transaction callbacks and relocate the entire workflow in one change. Choose the receiver as a small cohesive extraction. Further transaction-port design requires its own evidence and does not justify importing SQL or driver types into application code.

## Decision

Request.ApplyApprovalDecision consumes a validated Approval, the current eligible approval count and approval validity. It returns the next request value without persistence, permission resolution, clock reads or audit writes. Approval.DecidedAt is its single explicit authoritative instant. Revalidate public approval fields and request/organization association, reject missing decision time, negative counts and an impossible pinned quorum through existing domain errors, and preserve the existing self-approval and reason rules.

Below quorum, return the complete request unchanged, including timestamp, version, expiry, reason and submission snapshot. At or above quorum, reuse Request.Approved to open the window exactly at the supplied decision instant with positive validity and clear the system state reason. One rejection changes state and updated time, clears the system reason and preserves any existing expiry without creating or extending one. Unused validity does not alter a pending or rejected outcome. Do not increment request versions or rewrite immutable submission data in this receiver.

Retain the consumer-owned Repository Approve/Reject interface. The PostgreSQL adapter owns the organization coordinator, locked row and membership, lazy expiry, current resource authorization and eligible-count reads, distinct decision insertion, guarded state persistence and same-transaction audit/read view. Stamp the decision from the timestamp returned by insertion before invoking the receiver; use that same database instant for permission, count, validity and audit. Pre-validation does not supply authoritative time. Persist a transition only when the returned state changes and retain the database's guarded update.

Keep lazy-expiry refusal outside the transaction callback so the observed expiry and its audit commit rather than roll back. Preserve post-lock database time: transaction-start time can predate lock waits. A domain decision is not an authorization checkpoint or workflow lease. The application fake may delegate its pure state decision to the same receiver but must not claim to reproduce database locking or authorization.

## Consequences

Quorum and rejection rules become independently testable domain behavior and can be reused through the consumer's fake port. Transaction and permission facts still come from the adapter and require real PostgreSQL regressions. The extraction changes implementation ownership without changing product requirements, APIs, schema, current permissions or review/execution policy.

Observe compiling-stub behavioral red before implementation, cover at least four successful and four refused decision cases, and retain real-database timestamp, under-quorum, rejection, duplicate, concurrent eligibility and lazy-expiry regressions. Require the full functional/security gates, code/architecture/design and staged review, and actual running-application QA before the focused commit. Record implementation evidence in milestone qualification rather than amending this decision body.

## Sources (verified 2026-10-24)

- [DDD domain entities and business invariants](https://learn.microsoft.com/en-us/dotnet/architecture/microservices/microservice-ddd-cqrs-patterns/microservice-domain-model).
- [Application coordination and infrastructure boundaries](https://learn.microsoft.com/en-us/dotnet/architecture/microservices/microservice-ddd-cqrs-patterns/microservice-application-layer-implementation-web-api).
- [PostgreSQL 18 transaction and clock timestamps](https://www.postgresql.org/docs/18/functions-datetime.html#FUNCTIONS-DATETIME-CURRENT).
