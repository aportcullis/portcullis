# ADR-0021: Governed PostgreSQL execution

- **Status:** Accepted
- **Date:** 2026-10-03

## Context

The request and approval workflow exists, but its target execution adapter is not exposed to users.
PRD §4.4 requires single-use execution, durable start evidence, lease recovery, and late-completion fencing.

## Decision

Acquire the connection lock before the request lock, matching configuration and policy cascades.
Under these locks, recheck requester eligibility, approval expiry, current configuration and policy versions, valid approval quorum, and the authenticated payload digest.
Insert the unique request execution, transition to executing, and insert EXECUTION_STARTED in one metadata transaction.
No target connection is opened before this transaction commits.

Lease heartbeat runs every 15 seconds and extends the deadline by 60 seconds.
An expired lease cannot be revived by its former owner.
Startup and 30-second background reconciliation mark expired attempts outcome_unknown without retrying target SQL.
Reconciliation and completion both lock request then execution, so concurrent transitions serialize.
Completion is fenced by executing state, owner, attempt ID, and live deadline.
A late completion appends LATE_COMPLETION_OBSERVED without changing the original outcome.

Expiry and invalid approval decisions commit their system audit event before returning a refusal.
Owner-verified preflight refusals, including saturated workers and shutdown admission, record EXECUTION_REJECTED without acquiring a lease.
STARTED identifies admission to target execution; treating a refused preflight as STARTED would falsely imply ownership and target admission. PRD §6.1 distinguishes these events in both translations.
All execution events carry the immutable request's digest, class, connection, and redacted SQL.
They never carry original SQL, parameters, database errors, or result values.

### PostgreSQL catalog gate and bounded processing

The PostgreSQL wire protocol does not expose the analyzed expression tree's selected callable OIDs.
M1 uses a stricter candidate proof: under pinned `search_path=public`, every visible function/operator candidate for each explicitly referenced name must have a bootstrap built-in OID below 16384 in pg_catalog, including the operator's implementation function.
PostgreSQL searches pg_catalog implicitly before every listed schema, so built-in names still resolve first while unqualified objects created by approved DDL land in `public` (revised 2026-10-04: listing `pg_catalog` first made it the creation schema, so every unqualified governed DDL failed with 42501 after approval).
Explicit types must also resolve exclusively to built-in catalog types.
Reject the whole statement when any candidate is untrusted, even if PostgreSQL would select a safe overload for these arguments.
This intentionally reduces compatibility; it does not claim to reproduce PostgreSQL overload selection.
ADR-0002 and both PRD translations adopt this conservative alternative to exact selected-OID resolution.
Catalog ownership, concurrent target catalog changes, implicit casts from user-defined relation columns, views, triggers, row security, and function bodies remain under the least-privilege target account and trusted database administrator boundary.

### Lock waits (2026-10-04)

Governed sessions pin `lock_timeout` from the Tier-C setting `execution_lock_timeout` (default 5s, range [1s, 60s]; ADR-0017), alongside `statement_timeout`.
PostgreSQL queues later lock requests behind a waiting one, so an approved `ALTER TABLE` or `TRUNCATE` waiting for ACCESS EXCLUSIVE otherwise stalls every reader and writer of that table for up to the five-minute statement timeout.
The timeout applies separately to each lock acquisition and only while waiting; exceeding it raises SQLSTATE 55P03 before COMMIT, which is a confirmed `failed` outcome and leaves the target healthy.
When a policy's statement timeout is at or below the lock timeout, PostgreSQL's statement timeout fires first, which is equally bounded.
Source: [PostgreSQL client connection defaults](https://www.postgresql.org/docs/current/runtime-config-client.html#GUC-LOCK-TIMEOUT).

### Interruption outcomes (2026-10-04)

An interruption is classified by whether COMMIT had been sent, because a transaction opened with BEGIN commits only on COMMIT.
The dialect marks every context end observed during dial, BEGIN, the catalog gate, the statement or a truncated read with `query.ErrInterruptedBeforeCommit`; owner, shutdown or lease-loss cancellation before COMMIT records `cancelled`, and a local deadline before COMMIT records `failed`.
A context end while COMMIT is in flight carries no marker and stays `outcome_unknown`, as do connection loss and SQLSTATE 08xxx/40003 in any phase.
The target's statement timeout (SQLSTATE 57014) is a server-confirmed rollback recorded as `failed`.
PostgreSQL measures `statement_timeout` from when each command arrives and disables it before COMMIT, so deferred COMMIT work is bounded only by the local deadline.
The local deadline therefore exceeds the policy timeout by `query.ExecutionDeadlineGrace` (10 seconds): previously both used the same duration while the local clock started before dial, BEGIN and the catalog gate, so it always fired first and every timeout and confirmed cancellation was recorded `outcome_unknown`.
Sources: [PostgreSQL statement_timeout](https://www.postgresql.org/docs/current/runtime-config-client.html#GUC-STATEMENT-TIMEOUT), [pgconn CancelRequestContextWatcherHandler](https://pkg.go.dev/github.com/jackc/pgx/v5/pgconn#CancelRequestContextWatcherHandler).

The server admits two active target executions and two result-processing workers.
Saturation refuses work before acquiring a lease; result processing responds with Retry-After.
Governed target streams check raw row bytes before decoding and limit protocol message allocation to the result byte budget plus 64 KiB framing allowance.
They separately reserve cell structs and row headers, including the executor's row copy, within the same policy byte budget before decoding. NULL or narrow wide-column rows cannot bypass admission by contributing no payload bytes. These are admission bounds, not a measured bound on total Go heap, allocator overhead, or encryption scratch space; ADR-0020 capacity qualification remains pending.
Once a returning write exceeds the snapshot budget, drain remaining rows and confirm COMMIT; truncation never commits only part of a statement.
A read stops at the snapshot ceiling instead (revised 2026-10-04): it sends a cancel request, closes the read-only session without COMMIT, and reports the delivered rows as rows affected, because the total count only arrives with the final command tag.
Draining a read held the worker, target I/O and network until the statement finished, the statement timeout fired or a later row raised an error that the delivered snapshot never contained.
A protocol/connection interruption with unconfirmed completion is outcome_unknown.
Confirmed SQL refusal is failed; a missing or saturated result cache does not change confirmed target success.
Snapshot persistence runs on `context.WithoutCancel` with its own 10-second bound (revised 2026-10-04): it previously shared the execution deadline, so a statement finishing near its timeout or a cancellation arriving after COMMIT silently dropped a committed result.
A rejected or failed snapshot records `result_store_full` or `result_persistence_failed` as `result_unavailable_reason` in EXECUTION_FINISHED metadata; the reason is a fixed vocabulary validated by the domain, never store error text. Source: [Go context.WithoutCancel](https://pkg.go.dev/context#WithoutCancel).

### Result and notification contracts

Result snapshots use one DEK per result, independently authenticated schema/100-row chunks, owner-and-organization-scoped reads, 15-minute TTL, and ADR-0011 admission/eviction.
The 25MiB snapshot ceiling cannot enlarge a pinned connection policy's lower limit; new policies retain ADR-0015's 16MiB default.
An oversized snapshot keeps the longest row prefix that fits the budget: the codec measures each row's encoded size once and encrypts once (revised 2026-10-04), instead of re-sealing the whole snapshot for every step of a binary search.
Server sorting retains exact integer/decimal precision, NULL-last ordering, and original row ordinal ties.
CSV processes chunks through the bounded worker pool and escapes spreadsheet formulas in headers and cells.
The M1 single-process server records and forwards cancellation only for its locally active owner; only a cancellation observed before COMMIT is reported as a rollback.
CSV streaming authenticates session and CSRF before serving, rechecks permissions while sending, and periodically revalidates session liveness.
Each CSV HTTP write has a refreshed 30-second deadline so a stalled consumer releases the worker.
Unknown JSON and binary Execute fields are rejected before target admission; decode errors never echo payload values.
Approval notifications use PRD §7.4's 30-second fallback polling, including foreground/reconnection refresh and a pending badge.
Live server push is additive; no delivery guarantee beyond re-reading durable request state is implied.

## Consequences

Request history and execution records survive connection archive and result-cache loss.
A metadata failure after target commit leaves an unknown outcome; the system does not retry SQL to recover a result.
Integration scenarios verify concurrent acquisition, replay refusal, requester and organization boundaries, expired-owner recovery, and late completion.

## Sources

Verified 2026-10-03:

- [OWASP transaction authorization](https://cheatsheetseries.owasp.org/cheatsheets/Transaction_Authorization_Cheat_Sheet.html): final authorization gate before execution.
- [PostgreSQL explicit locking](https://www.postgresql.org/docs/current/explicit-locking.html): row locks and consistent lock order.
- [PostgreSQL function resolution](https://www.postgresql.org/docs/current/typeconv-func.html): names do not establish callable identity.
- [Go Sizeof](https://pkg.go.dev/unsafe#Sizeof): size includes struct padding but excludes referenced payloads, which the separate raw-byte counter covers.
