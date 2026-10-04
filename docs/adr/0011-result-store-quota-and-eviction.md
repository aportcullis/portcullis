# ADR-0011: Result store quota & eviction

- **Status:** Accepted — model fixed; numeric values are **provisional until the Core 2 load test** (they gate Core 2 per the PRD §12.2 and are re-confirmed, not re-designed, by it).
- **Date:** 2026-07-04 (amended 2026-10-03: preserve live snapshots on admission refusal; amended 2026-10-04: authenticated snapshot metadata)

## Context
The PRD fixes the result-store shape (§6, §7.1, §12.1): PostgreSQL `result_cache` schema, UNLOGGED `result_sets`/`result_chunks`, per-result DEK (AES-256-GCM, ADR-0003 envelope), 15-minute TTL, per-result caps of **10,000 rows / 25 MiB** (`truncated=true` beyond), and a **512 MiB** global logical cap with LRU eviction.
It deliberately left three things open: the per-user quota, the evict-vs-reject priority at the global cap, and UNLOGGED autovacuum tuning.
This ADR closes them so the admission path is deterministic.

## Decision

### Quotas (logical ciphertext bytes, tracked on `result_sets`)
| Limit | Value |
|---|---|
| per result | 10,000 rows / **25 MiB** (PRD, restated) |
| per user | **64 MiB** *(provisional)* — ⌈2.5⌉ max-size results; single-org MVP |
| global | **512 MiB** (PRD) |

### Admission algorithm (on snapshot write; single source of truth for "full")
Reject invalid or individually oversized incoming snapshots before accounting. For a valid bounded snapshot:
1. Delete **expired** snapshots (TTL 15 min) touched by the accounting query — expiry always wins over any live data.
2. If the writer's **per-user** usage + incoming size exceeds the user quota → plan eviction of that user's own snapshots, **least-recently-accessed first**, until it fits.
3. If **global** usage + incoming size exceeds 512 MiB → plan eviction of global LRU (by `last_accessed_at`, oldest first) — but never below a **per-user floor of one snapshot** for other users (a single heavy user must not flush everyone).
4. If it still doesn't fit because other users' one-snapshot floors prevent enough reclamation → **reject** the snapshot write with `result_store_full`; the execution itself still completes and `query_executions` records the outcome with no result handle; the UI shows the existing `result_unavailable` state with a "store full, retry later" reason.
   Expiry is unconditional, but live evictions are only a plan until admission is proven possible. On refusal, discard all planned live evictions: do not delete live ciphertext or emit live-eviction events. If admission is possible, apply the planned live evictions and insert the new snapshot atomically under the existing transaction-scoped advisory lock. This replaces the original eviction-then-reject policy: a failed incoming write must not destroy useful live results without gaining a replacement.
5. Every eviction of a non-expired snapshot leaves an audit event (`RESULT_EVICTED`, actor `system:result-store`, metadata: cause `user_quota|global_cap`).

**Concurrency (amended 2026-10-04):** admission is serialized **per organization** by a two-key transaction advisory lock (class 4 in the ADR-0010 keyspace, object = hash of the organization id) instead of one install-wide key, so organizations admit concurrently; the "global" cap is accounted over the organization's rows, which in the single-org MVP is the whole install.
The accounting read no longer takes `FOR UPDATE` on every result row, so readers and last-access touches are not blocked by an admission; a planned delete of a row a purge already removed is a no-op.
Chunks are written with one `COPY` instead of one round trip each while the lock is held.
TTL purge takes no admission lock: it selects expired rows through `result_sets_expiry_idx` with `FOR UPDATE SKIP LOCKED` in bounded batches and leaves rows another transaction holds to a later pass.
Admission semantics are unchanged: planned live evictions are applied, with their audit events, only when admission succeeds.

`last_accessed_at` updates are throttled to once per minute per snapshot (same rationale as the session idle-slide throttle, ADR-0006 Parameters).

### Metadata integrity (amended 2026-10-04)
`row_count` and `truncated` are final before a snapshot is sealed and are authenticated, with the derived chunk count, inside every chunk's AAD (ADR-0003). A reader that finds them altered gets `result unavailable` instead of a silently shortened page or CSV, or a false truncation notice. `byte_size` stays unauthenticated accounting input for quota decisions.

### UNLOGGED autovacuum & bloat *(provisional — confirm via the Core 2 load test)*
- `result_chunks` / `result_sets` get per-table storage parameters: `autovacuum_vacuum_scale_factor = 0.02`, `autovacuum_analyze_scale_factor = 0.05`, `autovacuum_vacuum_cost_delay = 0` — churn is the norm here (15-min TTL), so vacuum must keep up with delete volume; UNLOGGED tables skip WAL, making aggressive vacuum cheap.
- The load test measures physical bloat at steady state (write/evict churn at the 512 MiB cap); acceptance: physical size stays under **2×** the logical cap.
  If it doesn't, tune the scale factors here before Core 2 ships.

## Consequences
- The admission path is fully deterministic and testable: fixtures pin steps 1–4 (expired first, own-LRU, global-LRU with per-user floor, reject-without-live-loss).
- Per-user quota, floors, and autovacuum parameters carry a *provisional* marker: the Core 2 load test re-confirms the numbers and this ADR is amended with the measured results — changing a number is an amendment here, not code drift.
- `result_sets` needs `owner_user_id`, `byte_size`, `last_accessed_at`, `expires_at` columns indexed for the accounting queries (index design per `docs/conventions/data.md`).

## Sources (checked 2026-07-04)
- PostgreSQL UNLOGGED tables (no WAL, crash-truncated, not replicated): https://www.postgresql.org/docs/current/sql-createtable.html#SQL-CREATETABLE-UNLOGGED
- Per-table autovacuum storage parameters: https://www.postgresql.org/docs/current/sql-createtable.html#SQL-CREATETABLE-STORAGE-PARAMETERS

- PostgreSQL advisory locks (checked 2026-10-03): https://www.postgresql.org/docs/current/explicit-locking.html#ADVISORY-LOCKS
- PostgreSQL `SELECT ... FOR UPDATE SKIP LOCKED` (checked 2026-10-04): https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE
- pgx `CopyFrom` for bulk inserts (checked 2026-10-04): https://pkg.go.dev/github.com/jackc/pgx/v5#Conn.CopyFrom
