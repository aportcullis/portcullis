# ADR-0015: Connection policies — per-class approvals, execution limits, immutable versions

- **Status:** Accepted
- **Date:** 2026-07-18

## Context
Access requests (next slice) must pin, at submit time, the connection policy that governed them: which statement classes are allowed, how many approvals each class needs, and the execution limits (PRD §4.3 "정책 snapshot", §12.1).
That pin (`policy_version` inside the approval payload, §4.3) is only meaningful if the pinned snapshot can never change — so the policy store must be versioned and immutable.
ADR-0014 deferred exactly this: `connection_policy_versions` + `connections.current_policy_version` + a default-policy backfill, in "the next free migration" — which is **0011**.

The PRD fixes: per-class (`read`/`write`/`ddl`) allow flag and `required_approvals` (0–N, default 1; 0 = system auto-approval, audited the same), new connections default read-only (`read=true, write=false, ddl=false`), and "enabling write/DDL leaves an admin audit event" (§4.3).
It leaves open: the exact schema, the granularity of the execution limits (§6's `read/write/ddl별 required_approvals·limit` is ambiguous), limit defaults and bounds, concurrency control for policy updates, and what happens to in-flight requests when none exist yet.

Permission keys `policies.get` / `policies.update` were seeded in migration 0002 with no enforcement site; this slice adds the enforcement.

## Decision

### Immutable versions, explicit columns
`connection_policy_versions` is **append-only**: every change inserts version N+1; rows are never updated or deleted.
Columns are explicit (no jsonb) so bounds live in CHECK constraints and future slices can query/filter without JSON path expressions (data.md).

```
connection_id              uuid    not null
organization_id            uuid    not null
version                    bigint  not null  check (version > 0)
read_allowed               boolean not null
write_allowed              boolean not null
ddl_allowed                boolean not null
read_required_approvals    int     not null  check (0..100)
write_required_approvals   int     not null  check (0..100)
ddl_required_approvals     int     not null  check (0..100)
query_timeout_seconds      int     not null  check (1..300)
max_rows                   int     not null  check (1..10000)
max_result_bytes           bigint  not null  check (4096..67108864)
created_by                 uuid    not null  references users (id)  -- + index (data.md)
created_at                 timestamptz not null default now()
primary key (connection_id, version)
foreign key (connection_id, organization_id)
    references connections (id, organization_id) on delete restrict
```

- The composite FK targets `connections (id, organization_id)` — the `unique (id, organization_id)` key that 0007 created precisely so org-pinned references cannot cross organizations (ADR-0004 posture).
- `*_required_approvals` is stored even while `*_allowed=false`, so disabling a class and re-enabling it later does not silently reset its quorum.
  No cross-column CHECK couples them.
- **Sensitive table (data.md gate):** rows are evidence that future approval payloads pin.
  The migration `REVOKE UPDATE` from the runtime role in the same file (DELETE is already revoked cluster-wide by 0010), and `privcheck.go` gets an explicit `{required: SELECT, INSERT; forbidden: UPDATE, DELETE, TRUNCATE, TRIGGER, REFERENCES, MAINTAIN}` entry — the same append-only shape as `audit_events` (ADR-0009).

### Limits are per-policy, not per-class
PRD §6's one-line data model (`read/write/ddl별 required_approvals·limit`) reads either as "per-class approvals *and* per-class limits" or "per-class approvals, plus limits".
We fix the second reading: **one `{query_timeout_seconds, max_rows, max_result_bytes}` set per policy version.** Rationale:

- §8.2 states the execution limits globally ("query timeout 30s 기본/최대 5m", row cap 10k, byte cap) with no per-class distinction anywhere in the PRD.
- The model the PRD cites for approvals (kviklet 0.9.2's `ReviewConfig.numTotalRequired`, §4.3, re-verified 2026-09-30) configures approvals **per connection**.
  Its connection DTO does not define read/write/DDL-specific limits — the per-class split is Portcullis's extension for *approvals*, where §4.3 explicitly demands it; nothing demands it for limits.
  Enterprise role review requirements are a separate extension (ADR-0019).
- Nine limit columns triple the form/payload/proto surface with no PRD-driven use case.
- Forward-compatible: versions are append-only, so a later per-class need adds columns in a new migration backfilled from the policy-level values; old snapshots stay valid.

The PRD §6 line is amended alongside this ADR to say the limits are per policy version.

**Defaults (v1 / new connections) and bounds:**

| field | default | bounds | source |
|---|---|---|---|
| read | allowed, approvals=1 | approvals 0–100 | §4.3 read-only default, default 1 |
| write / ddl | **not allowed**, approvals=1 | approvals 0–100 | §4.3 |
| query_timeout_seconds | 30 | 1–300 | §8.2 (default 30s, max 5m) |
| max_rows | 10 000 | 1–10 000 | §8.2 (row cap 10k; policy may only lower) |
| max_result_bytes | 16 MiB | 4 KiB–64 MiB | ADR-0011 (per-user quota 64 MiB; one result must fit) |

The approvals upper bound (100) is a sanity cap, far above any real quorum; it exists so the column is never unbounded.
Timeout enforcement lands with the PG dialect adapter: the executor applies the policy value per statement (PostgreSQL `statement_timeout` — verified: measured from command arrival to completion, per-statement in simple protocol; the global `postgresql.conf` setting is explicitly not recommended, so a per-execution setting is the correct mechanism).
This ADR only fixes the stored contract.

### `connections.current_policy_version` — deferrable FK, no default
`connections` gains `current_policy_version bigint not null check (> 0)` plus a composite FK `(id, current_policy_version) → connection_policy_versions (connection_id, version)` declared **DEFERRABLE INITIALLY DEFERRED**.
The connection↔policy reference is circular inside the create transaction; PostgreSQL checks deferred FK constraints **at commit** (verified: `REFERENCES` constraints are deferrable; `DEFERRED` constraints are not checked until transaction commit), so insert order inside the transaction is irrelevant and a connection row can never commit without its policy row.
The column deliberately has **no default**: a code path that forgets the policy insert fails at commit instead of silently pointing at nothing.

**Migration 0011 order:** create table (+ grants/revokes) → add nullable column → backfill a version-1 default policy row for **every** existing connection, archived included (`created_by` = `connections.created_by`; the backfilled row is the PRD-mandated default state, not an admin action — no audit event) → set `current_policy_version = 1` → add NOT NULL + CHECK + the deferred FK.
Archived connections are included so the NOT NULL invariant has no carve-out, the future restore flow (ADR-0014 deferral) finds a policy in place, and history joins never hit NULL.

**Create atomicity:** `app/connection.Service` and the `Repository.Create` port are unchanged.
The postgres `ConnectionStore.Create` transaction inserts the `connection.DefaultPolicy()` v1 row alongside the connection (like the credential envelope, it is a storage-level companion of the insert); the integration test proves it.
The v1 defaults ride `CONNECTION_CREATED` implicitly — §4.3 only requires auditing write/DDL *enabling*, and the default enables nothing.

### Concurrency: the policy version is the optimistic token
`UpdatePolicy` is a **full replacement** (snapshot semantics, like `ReplaceConfig`) carrying `expected_version` — the version the client read.
One transaction:

1. `UPDATE connections SET current_policy_version = expected + 1 WHERE id = $1 AND organization_id = $2 AND archived_at IS NULL AND current_policy_version = expected RETURNING …` — zero rows disambiguates via a follow-up read into `ErrNotFound` / `ErrArchived` / `ErrPolicyConflict` (the `missingArchivedOrConflict` pattern).
2. `INSERT` the version `expected + 1` row — the `(connection_id, version)` PK is a second structural guard, and the deferred FK validates the pointer at commit.
   Amended 2026-10-04: the inserted and returned version is the `current_policy_version` the bump returned; the caller-supplied `Policy.Version` is ignored, so the pointer and the snapshot have one source.
3. Audit events in the same transaction (ADR-0009).

`connections.version` (the descriptor's optimistic token) and `updated_at` are **not** touched: descriptor and policy concurrency are orthogonal, and bumping the descriptor token would spuriously invalidate in-flight `ReplaceConfig` calls and reorder frontend summary application.
`ErrPolicyConflict` is distinct from `ErrConflict` so the transport can say "policy changed — refresh and retry" (mapped to `Aborted`).

### Audit vocabulary
- `CONNECTION_POLICY_UPDATED` — every update, same-tx.
  Metadata: new `policy_version`, the full new snapshot (`read/write/ddl: {allowed, required_approvals}`, the three limits), and diffs computed against the previous version: `enabled_classes`, `disabled_classes`, `changed_fields`, `auto_approve_classes` (classes at `required_approvals=0` — keeps the §4.3 small-team-deadlock setting audit-queryable).
- `CONNECTION_POLICY_CLASS_ENABLED` — one companion event per **newly enabled** `write` or `ddl` class (metadata: `class`, `required_approvals`, `policy_version`).
  This satisfies §4.3's "write/DDL 활성화는 admin audit event" as a first-class queryable action — the `CONNECTION_TLS_RELAXED` companion-event pattern (ADR-0014).

### Access rules
- `Get` is allowed on **archived** connections: the policy is part of the historical snapshot, like the descriptor (§4.3 내역 보존).
- `Update` on an archived connection fails with `ErrArchived` (`FailedPrecondition`) — enforced structurally by the `archived_at IS NULL` predicate in the pointer-bump UPDATE.
  No execution can occur on an archived connection, so new versions would be noise.
- RPC surface: a separate `ConnectionPolicies` Connect service (`Get`, `Update`), 1:1 with the `policies.*` permission resource seeded in 0002.
  `Connection`/`ConnectionSummary` messages are unchanged.
- *Amended 2026-07-18 (self-review):* `Update` REJECTS a request omitting any of the three class fields (`InvalidArgument`) — nil-coalescing an omitted class to `{false, 0}` would silently reset its kept quorum, making a later re-enable auto-approve.

### Deferred: expiring in-flight requests on policy change
§4.3 requires a policy change to expire the connection's un-executed `pending`/`approved` requests (`expired(reason=policy_changed)`).
`access_requests` does not exist yet. **The access_requests slice must add that expiry into the same transaction as step 1's pointer-bump UPDATE** — that UPDATE is the designated hook point; this ADR records the contract so the next slice cannot miss it.
Until then a policy update only bumps the version and audits. *Discharged 2026-07-22 by ADR-0018: `UpdatePolicy` now expires the connection's in-flight requests (with derived audit events) inside that same transaction.* *Amended 2026-07-27 (external review round 13): because an update is a **full replacement**, recovering from a conflict is a **three-way merge**, not a token swap.
Keeping the admin's draft and refreshing only `expectedVersion` — the round-7 recovery — re-sent every field they had not touched, so the other admin's change was reverted by the retry: the lost update the pointer bump exists to prevent, arrived at through the recovery path.
`rebasePolicy(base, mine, fresh)` now decides each field against the version the form was opened on — mine if only I moved it, theirs if only they did, **theirs plus a named conflict** if both — and the message tells the admin exactly which fields to re-decide.
Preferring theirs on a real conflict is the safe direction: silently re-opening a class the other admin just closed is the worst possible revert.* *Amended 2026-07-26 (external review round 12): the **policy version's `created_at` is that same observed instant**, not the application clock the caller built the policy with.
It arrived from `s.now()` before the transaction existed and was inserted before the instant was read, so an immutable version could be filed earlier than the very event that created it and earlier than the requests it expired — and the response handed that stale moment back.
The caller's timestamp is now an input to domain validation only; the database's observation is what is stored and returned.* *Amended 2026-07-26 (external review round 11): the pointer bump IS this transaction's wait — it UPDATEs the connection row, so it parks behind any request write holding it `FOR SHARE`.
The cascade and every event here are therefore dated from an instant observed **after** that bump (and after locking the rows to be swept), never from `now()`, which would place them before the writes they waited for.
The rule, its counter-examples, and its enforcing test live in ADR-0009.*

## Consequences
- Every future execution-path slice (dialect adapter, access_requests, executor) reads its class gate, quorum, and limits from the **pinned** policy version, never the current one.
- The append-only shape means policy history is complete by construction — a governance answer to "who allowed DDL here, and when" is one indexed query.
- Per-class limits, if ever needed, are an additive migration (columns backfilled from the policy-level values), not a redesign.
- The backfill makes `current_policy_version` NOT NULL from day one; no code path handles a policy-less connection.

## Sources (checked 2026-07-18)
- PostgreSQL `statement_timeout` semantics (per-statement, measured at server, global setting not recommended): https://www.postgresql.org/docs/current/runtime-config-client.html
- PostgreSQL deferrable constraints — `REFERENCES` is deferrable, `DEFERRED` checked at commit: https://www.postgresql.org/docs/current/sql-set-constraints.html
- kviklet 0.9.2 review configuration (re-checked 2026-09-30): [connection DTO](https://github.com/kviklet/kviklet/blob/0.9.2/backend/src/main/kotlin/dev/kviklet/kviklet/service/dto/Connection.kt), [review configuration](https://github.com/kviklet/kviklet/blob/0.9.2/backend/src/main/kotlin/dev/kviklet/kviklet/service/dto/ExecutionRequest.kt), ADR-0019.
- ADR-0011 — 64 MiB per-user result quota (byte-cap ceiling)
- ADR-0014 — deferral table (this slice), canonical-id and same-tx audit patterns
