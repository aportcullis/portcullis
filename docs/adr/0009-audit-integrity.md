# ADR-0009: Audit integrity — transactional delivery and the runtime permission boundary

- **Status:** Accepted (amended 2026-07-04: normative audit schema/index and the detached-write timeout transcribed)
- **Date:** 2026-07-03 (amended 2026-07-04)

## Context
The PRD requires structured, **append-only** audit events for authentication (and later, execution), and a runtime DB role that can only `INSERT/SELECT` on `audit_events`, separate from the migration owner.
The first audit implementation had two integrity gaps:

1. Events were written best-effort *after* the state change committed, so a transient DB failure or a cancelled request context could leave a successful bootstrap/login/logout with **no trail**.
2. Append-only was enforced only by triggers, while the server connected as the schema **owner** — and PostgreSQL lets the owner `ALTER TABLE ... DISABLE TRIGGER`, so the "immutability" did not bind the application at all (verified against the PostgreSQL ALTER TABLE docs).

Standards verified 2026-07-03: OWASP Logging Cheat Sheet (log all authentication successes and failures); single-database transactional-outbox guidance — when the audit sink is the **same** database, writing the audit row **in the same ACID transaction** is the standard; an outbox/queue is only needed for external sinks.

## Decision

### Delivery guarantees
- **State-changing auth operations commit their audit event in the same transaction.** The consumer-defined port carries the event into the atomic op: `BootstrapAdmin(..., evt)`, `RotateSession(..., evt)`, `RevokeSession(..., evt)` — the store inserts it via the transaction-bound queries.
  A created admin, an issued session, or a revoked session can therefore never exist without its trail (and vice versa: if the event can't be written, the state change rolls back).
- A first OIDC login extends that invariant: inserting `oidc_identities`, rotating the session, and its `AUTH_LOGIN` event (metadata `identity_linked=true`) are one transaction.
  A failed session/audit commit therefore cannot leave a newly usable external authenticator behind without evidence.
- The identity store completes `OrganizationID` for authentication events (single-org MVP) and, for bootstrap, the actor — the created user's id exists only inside the transaction.
- **Every other audit write names its organization explicitly** (amended 2026-10-04): the shared insert refuses an empty organization (`audit.ErrOrganizationRequired`) instead of attributing the event to the default one, a mutation's events take the mutation's organization and an event naming another organization is refused (`audit.ErrOrganizationMismatch`), and `AuditStore.List`/`Get` take the caller's organization so a foreign event id is not found (ADR-0004).
- **No-state-change events (failed logins) stay best-effort**: a login must not fail because the audit store hiccuped.
  A failed event is recorded on **every failure exit** of Login (a deferred catch-all) — wrong credentials, infra errors, and requests aborted mid-flight by a client disconnect (ctx cancellation during the lookup or the hash) all leave a trail.
  The write detaches from the request context (`context.WithoutCancel` + a **5s** timeout — long enough for a DB hiccup, short enough that a stuck store can't accumulate goroutines), and a drop is logged (action + error *type* only, never values).
- No outbox: the audit sink is the metadata database itself; same-transaction insert gives strictly stronger guarantees with none of the outbox's relay machinery.

### Runtime permission boundary
- Migrations run on `PORTCULLIS_MIGRATE_DATABASE_URL` as the schema owner **with `CREATEROLE`** — plain table ownership is not enough to `CREATE ROLE`.
  Alternatively, provision the runtime role beforehand (the migration skips creation when it exists), in which case the migrate user needs no `CREATEROLE`.
  The server runs on the **runtime DSN** (`PORTCULLIS_DATABASE_URL`).
- *Amended 2026-07-18 (external review):* the owner DSN must not live in the serving process.
  A server that carries `PORTCULLIS_MIGRATE_DATABASE_URL` in its environment defeats this ADR's compromised-process threat model — the attacker reads the env and escalates to the owner, who can disable the audit triggers.
  The **one-shot `portcullis migrate` command** is the recommended deployment shape: a separate short-lived process/container (compose: the `migrate` service) is the only holder of the owner DSN, and `serve` gets the runtime DSN only, skipping startup migration (ADR-0010 startup step 6 has the exact gating).
  Startup migration remains supported when the operator explicitly keeps the owner DSN on the server, or opts in with `PORTCULLIS_STARTUP_MIGRATE=true` (single-role dev/e2e — deliberately a dedicated flag, never a side effect of the privileged-runtime debug flag).
  Note the boundary this buys: it removes the standing owner credential from the server's env/memory; it does not defend the migrate container itself — that is a deliberately smaller, shorter-lived surface.
- **The migration principal must be able to act as the owner of the database AND schema `public`** (amended 2026-07-05).
  The boundary migrations `REVOKE ... FROM PUBLIC` *as the owner*; a non-owner makes those revokes silent no-ops (PostgreSQL warns but commits), the migration records as applied, and the boot-time privilege check then fails **every** boot blaming the runtime role, with no migration left to self-repair.
  A migration **preflight** checks this and fails fast with an accurate message (a superuser satisfies it implicitly).
- Migration `0003` creates a cluster-wide **NOLOGIN group role** (no secret in migrations): schema usage + table DML, minus `UPDATE/DELETE` on `audit_events`.
  Deployments create a login user and `GRANT <runtime role> TO <user>` (compose does this via an initdb script creating `portcullis_app`).
  Migration **`0005` additionally grants `USAGE` on sequences** (existing and, via `ALTER DEFAULT PRIVILEGES`, future) — a table with a serial/identity column needs it for the runtime `INSERT`, and without it the server boots fine but fails at first insert (amended 2026-07-05).
  A sensitive future table must still `REVOKE` in its own migration (data.md).
- *Amended 2026-07-18 (external review):* **the runtime role holds hard `DELETE` on NO table.** Every entity is soft-delete-only (data.md), not a single runtime query issues `DELETE`, and history removal is a separate retention concern (PRD §4.3) — so 0003's blanket `DELETE` grant violated least privilege: an application SQL defect or a compromised process could permanently destroy `users`/`roles`/`sessions`.
  Migration **`0010` revokes `DELETE`** on all current tables and from the owner's per-schema default privileges (a per-schema ADP `REVOKE` exactly reverses the previous per-schema ADP `GRANT` for the same defining role — web-verified), and the boot matrix's **default table policy now forbids `DELETE`** alongside the schema-shaping verbs.
  A table that genuinely needs runtime `DELETE` (e.g. a result-cache TTL eviction, ADR-0011) grants it in its own migration and registers an explicit per-table policy exception — the same gate sensitive tables already pass through.
- **Roles are cluster-wide**, so the role name is configurable (`PORTCULLIS_RUNTIME_ROLE`, default `portcullis_runtime`; validated as a plain identifier).
  Migrate publishes it as the `portcullis.runtime_role` session GUC on the migration connection; `0003` reads it via `current_setting` and splices it with `format(%I)` — no blind text substitution (which could rewrite an unrelated substring in a future migration, or leak the dev-membership grant).
  Installs sharing one PostgreSQL cluster MUST use distinct names — a shared name would merge their privileges.
  The compose dev-membership grant (`GRANT <role> TO portcullis_app`) fires **only for the default role name**, so a custom-name install never grants its role to a foreign install's login user.
  Migration `0003` also **revokes `CONNECT` from `PUBLIC`** on the database and grants it explicitly to the runtime role, so roles from other installs cannot even connect (the owner keeps `CONNECT` implicitly).
  It also revokes database `TEMPORARY` from `PUBLIC`: PostgreSQL searches a session's temporary schema before permanent relations, so a temporary `audit_events` could otherwise divert an unqualified audit insert.
  Application and verification SQL schema-qualifies metadata relations (`public.audit_events`, etc.) as the second half of this defense.
  Because `0003` is version-recorded on databases migrated before this rework (and applied migrations are immutable — data.md), migration **`0004` re-establishes the `PUBLIC` revocations** (database `TEMPORARY`/`CREATE`, schema `USAGE`/`CREATE`) idempotently, so pre-rework databases pass the boot-time privilege verification instead of failing it forever.
- Append-only is now enforced twice: privileges bind the runtime (no `UPDATE/DELETE`, and — not being the owner — no `ALTER TABLE`/trigger changes), and the triggers still guard the owner path during migrations/operations.
  `schema_migrations` is **owner-only** (all runtime access revoked): with any access the runtime could delete applied records (forcing re-runs) or pre-insert future versions (skipping security migrations).
- **The boundary is verified on BOTH sides at every boot.** (1) The configured group role, via the owner connection after migrations (below).
  (2) The **actual runtime connection's `session_user`**, right after the runtime pool connects, which must: be a member of the runtime role; hold no dangerous attribute directly; **not own — nor be able to `SET ROLE` into the owner of — the database, the `public` schema, or `audit_events`/ `schema_migrations`** (any such owner can `DROP`/`ALTER` the audit table or disable its triggers, e.g. `DROP SCHEMA public CASCADE`); **be able to `SET ROLE` into no role other than itself and the runtime role** — checked as `pg_has_role(..., 'SET')` **or** `pg_has_role(..., 'MEMBER WITH ADMIN OPTION')`, separately from mere `MEMBER`, so inert `SET FALSE, INHERIT FALSE` memberships remain valid but a `WITH ADMIN OPTION` membership is rejected even when granted `SET FALSE` (the member can grant itself `SET` and then escalate) (amended 2026-07-05); hold no `TEMPORARY` privilege; satisfy the same effective privilege matrix; and be connected to the migrated database.
  `session_user` is the floor identity because a connection whose `current_user` was masked by a login-time role setting can always execute `SET ROLE NONE` to recover the authenticated principal's powers.
  Checking only the role would miss an owner/superuser DSN, direct grants on the login user, SET-ROLE-reachable privileged roles, and wrong-database DSNs.
  Only **over-privilege** violations (the above) are downgraded to a warning by the explicit `PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME=true` (dev only; classified via `ErrRuntimeInsecure`).
  A wrong or unmigrated database, a missing required privilege, **non-membership (always — even alongside an over-privilege violation)**, or a verification-query failure is **always fatal** — the flag can't mask a misconfigured DSN into a falsely-healthy boot.
  Membership is checked **before** the remaining over-privilege classes, so a wrong principal that also happens to hold excess privilege is reported as the non-member it is, not downgraded (amended 2026-07-05).
  The only shapes the flag permits are the intentional single-role dev setups — owner or superuser — which are caught by the owner-reach / dangerous-attribute checks that precede the membership check.
- **The runtime role is verified on every boot**, not only at creation.
  Before migrations run, a pre-existing role is rejected if it holds `SUPERUSER`/`CREATEROLE`/`CREATEDB`/`BYPASSRLS`/ `REPLICATION` (members can `SET ROLE` into those powers; `LOGIN` is allowed — the role may be the login user itself).
  After migrations, the effective state must hold: CONNECT; **USAGE on schema `public`** (table privileges evaluate independently of schema USAGE, so without this check a role missing only USAGE would verify "healthy" and then fail every runtime query); on `audit_events` SELECT+INSERT and none of UPDATE/DELETE/TRUNCATE/TRIGGER/REFERENCES/MAINTAIN (TRIGGER would let a non-owner attach e.g. a BEFORE INSERT trigger that silently blocks the trail); on `schema_migrations` no privilege at all; and no database `TEMPORARY`.
  This catches a renamed `PORTCULLIS_RUNTIME_ROLE` (migration 0003 is version-recorded, so a new name never receives grants) and later drift, failing fast with a pointer here.
  Existing **memberships** of a pre-provisioned role are the operator's responsibility — the server does not audit who may `SET ROLE` into it.
- **Role rotation procedure** (changing `PORTCULLIS_RUNTIME_ROLE` after the first boot): as the owner, run 0003's statements for the new name, then strip the old role —
  ```sql
  create role <new> nologin;                    -- if not already provisioned
  revoke temporary on database <db> from public;
  grant connect on database <db> to <new>;
  grant usage on schema public to <new>;
  grant usage on schema result_cache to <new>;
  grant select, insert, update, delete on all tables in schema result_cache to <new>;
  -- No DELETE anywhere except settings: hard delete is revoked for the runtime
  -- (0010, amendment below) — a rotation must not resurrect it, or boot
  -- verification fails on every start with no migration left to self-repair.
  grant select, insert, update on all tables in schema public to <new>;
  grant usage on all sequences in schema public to <new>;
  -- settings is the one DELETE exception: reset-to-default removes the override
  -- row (0012, ADR-0017); boot verification REQUIRES this grant.
  grant delete on public.settings to <new>;
  revoke update on public.audit_events from <new>;
  -- append-only policy snapshots (0011, ADR-0015) — same boundary as audit_events
  revoke update on public.connection_policy_versions from <new>;
  -- append-only approval evidence (0013, ADR-0018) — same boundary
  revoke update on public.approvals from <new>;
  revoke all on public.schema_migrations from <new>;
  alter default privileges in schema public grant select, insert, update on tables to <new>;
  alter default privileges in schema public grant usage on sequences to <new>;
  grant <new> to <login user>;
  -- decommission the old role COMPLETELY — default privileges would otherwise
  -- keep granting it DML on every FUTURE table and USAGE on every future
  -- sequence (and any leftover default-privilege entry makes DROP ROLE fail
  -- with a dependency error), and the login user would keep inheriting
  -- whatever the old role still holds:
  alter default privileges in schema public
      revoke select, insert, update, delete on tables from <old>;
  alter default privileges in schema public
      revoke usage on sequences from <old>;
  revoke all on all tables in schema public from <old>;
  revoke usage on all sequences in schema public from <old>;
  revoke all on all tables in schema result_cache from <old>;
  revoke usage on schema result_cache from <old>;
  revoke usage on schema public from <old>;
  revoke connect on database <db> from <old>;
  revoke <old> from <login user>;
  drop role <old>;  -- preferred once nothing else depends on it
  ```
  then update the env var and restart.
  `TestRuntimeRoleRotationRunbook` executes this runbook verbatim against a database with a real sequence — keep the two statement lists in sync by hand.
- Future tables get DML via default privileges; a migration adding a **sensitive** table must `REVOKE` in that same migration (rule recorded in `docs/conventions/data.md`).
- With `PORTCULLIS_MIGRATE_DATABASE_URL` unset, `portcullis migrate` (and single-role dev's startup migration) falls back to `PORTCULLIS_DATABASE_URL` — see the one-shot amendment above.

### Migrator identity (amended 2026-07-13 — external review)
- `ALTER DEFAULT PRIVILEGES` binds to the **creating role** and is never inherited through membership (PostgreSQL docs).
  A member-of-owner migrating without `SET ROLE` therefore creates tables the runtime role has **no grants on** — boot used to pass (the old postflight checked only audit/history) and the first RPC failed.
  Fixed on three fronts:
  1. **`Migrate` runs AS the schema owner**: it resolves the owner (PG15+: schema `public` belongs to the `pg_database_owner` pseudo-role, so the real target is `datdba`; a reassigned schema resolves to its owner and must also cover the database-level revokes) and `SET ROLE`s into it (`set_config('role', …)`, `RESET ROLE` on exit; a failed reset destroys the pooled connection).
     The migrator must **be** the owner or hold **SET-capable membership** (`GRANT <owner> TO <migrator> WITH SET TRUE`) — inherit-only membership is refused, since it can run REVOKEs but can never fix the default-privilege binding.
     A superuser that is not `datdba` also SET ROLEs; its objects now belong to `datdba` (behavior change).
     A legacy `schema_migrations` owned by a previous member migrator is re-owned before the switch.
  2. **Migration `0008_owner_grant_repair`** re-grants runtime DML on everything that exists, re-binds the default privileges to the owner, and re-applies the sensitive revokes — so databases whose grants were bound to a previous migrator self-repair.
     Residual limit: ownership of tables created by an old member migrator is not normalized (manual `REASSIGN OWNED` if a future migration must ALTER them); grants, the failure class, are.
  3. **Boot verification covers every table and sequence**: `verifyTablePrivileges` checks a per-table policy (default: SELECT/INSERT/UPDATE required, DELETE/TRUNCATE/TRIGGER/ REFERENCES/MAINTAIN forbidden — no-hard-delete is the baseline since 0010; exceptions: `audit_events` S+I only, `schema_migrations` nothing — the code form of data.md's sensitive-table rule) plus sequence USAGE (UPDATE forbidden).
     Unknown new tables get the default policy, so a migration that forgets grants fails its very first boot in CI; a new sensitive table fails until its policy entry lands — the review gate, enforced.
     In the runtime-connection check a missing required verb is always fatal; a forbidden one is the dev-downgradable over-privilege class.

### Migration history and bounds (amended 2026-10-04)
- **Checksummed history**: migration 0019 adds `schema_migrations.checksum` (sha256 of the exact embedded file); the runner records it for each new file and backfills rows applied before the column existed.
  Before applying anything it refuses a recorded checksum that differs from the embedded file (a released migration was edited) and any recorded version the binary does not ship (a newer binary migrated this database), so an older binary cannot run against a schema it does not know.
- **Separate lock wait**: the session migration lock is polled with `pg_try_advisory_lock` for a bounded wait (default 2 minutes, `WithLockWaitTimeout`), and the server and `portcullis migrate` no longer run migration inside the 30-second startup budget.
- **Bounded transactions**: each file runs with `SET LOCAL lock_timeout` (5s) and `statement_timeout` (15m); a `55P03` lock timeout rolls back and retries the whole file up to five times with a backoff, while a statement timeout fails without retry.
- **Constraint additions**: migration 0017 added its CHECK while validating existing rows under the table lock; it is released and stays as is, and future CHECKs on populated tables use `NOT VALID` followed by `VALIDATE CONSTRAINT` (data.md).

### Audit table shape (normative — mirrors migration 0001)
- Columns: `id uuid pk`, `organization_id` (FK, restrict), `occurred_at timestamptz default now()`, `actor_type` (`check in ('user','system','service')`), `actor_user_id` (nullable FK), `actor_service`, `action`, `target_type`, `target_id`, `outcome`, `previous_state`, `next_state`, `payload_digest bytea`, `payload_digest_key_version int`, `request_id`, `connection_id uuid`, `query_type`, `rows_affected bigint`, `duration_ms bigint`, `risk_score double precision`, `metadata jsonb not null default '{}'`.
- **Digest pairing is CHECK-enforced**, not convention: `(payload_digest is null) = (payload_digest_key_version is null)` (a digest without its key version is unverifiable; a version without a digest is meaningless), and `payload_digest_key_version > 0` when present.
- **Timeline index:** `(organization_id, occurred_at desc, id desc)` — the org-scoped reverse-chronological list is the only hot read path, and the `id desc` tail is the pagination tie-breaker (PRD §7.1) so page boundaries are stable when timestamps collide.
- **Append-only needs two triggers** (plus the privilege boundary below): a row-level `before update or delete` trigger and a **separate statement-level `before truncate`** trigger — row-level triggers do not fire on TRUNCATE.
  Both call one `raise exception` function.

### Scope notes
- **`occurred_at` is DB-assigned by default, and MAY be passed for a DERIVED event** (amended 2026-07-26, external review round 5): the column keeps its `default now()`, so an ordinary event's timestamp never comes from the caller.
  A derived event — one the server records *because it observed something* — instead carries the DB instant of that observation.
  The case that forced this: a lazily observed TTL expiry runs behind a `FOR UPDATE` read that may have waited on a row lock, and `now()` is the *transaction's start* time, so the event (and the row's `updated_at`) would claim the request expired **before** its own `expires_at`.
  `ExpireOverdueAccessRequest` therefore takes one `clock_timestamp()` in a CTE and uses it for the predicate, `updated_at`, and the event, making `expires_at ≤ updated_at = occurred_at` hold by construction (ADR-0018).
  This does not weaken the integrity story: `occurred_at` is not covered by any digest, and row-sealing is an explicit non-goal below.
- **A mutation dates itself from ONE instant observed AFTER its locks are held — never `now()`** (amended 2026-07-26, external review round 11; this generalizes and supersedes the narrower rule above).
  `now()` is the transaction's *start* time, so it orders contending transactions by **when they began**, and under a row-lock wait that is the opposite of the order they actually ran in.
  Two verified consequences, both reproduced by forcing the wait in integration tests: an archive that began before a submit and ran after it expired the fresh request with a timestamp *older than the submission*, and the audit list — sorted `occurred_at desc` — showed the cascade **above its own cause**.
  The rule is therefore **lock → observe → write**: take the row locks as their own statements, read `clock_timestamp()` as its own statement (`ObserveWallClock`), and pass that value to every row and every audit event the transaction writes (`stampEvents`, `internal/infra/postgres/instant.go`).
  Three details are load-bearing. **(a)** Inlining `clock_timestamp()` into the waiting statement is *not* equivalent: PostgreSQL promises only that "the search condition of the command (the `WHERE` clause) is re-evaluated" after a lock wait and says nothing about the SET list, so a stamp written that way may still predate the wait. **(b)** A cascade must also lock the **request rows** it will sweep (`LockSweptRequestsForConnection`): only `CreateDraft` and `Submit` lock the connection, so an instant taken right after the connection lock can still precede a concurrent *decision*, which locks the request row alone — removing that lock makes the test fail again. **(c)** An event that already carries a more precise moment of its own (a decision's `decided_at`, an auto-approval derived from `expires_at`) keeps it; the transaction's instant only fills the gaps.
  The rule is enforced, not remembered: `TestWriteQueriesDoNotStampWithNow` fails on any `now()` inside a write query unless the query is named in an allowlist **with its reason** (currently the login-backoff arithmetic, session revocation, and the `InsertAuditEvent` fallback). **Banning `now()` was not enough** (amended 2026-07-27, external review round 13): `submitted_at` kept arriving as a *parameter* the application computed before the transaction existed — the same defect with a different source.
  A second gate, `TestWriteQueriesTakeEventTimesFromTheObservedInstant`, therefore fails any write that fills an **event-time column** (`created_at`, `updated_at`, `submitted_at`, `decided_at`, `occurred_at`, `archived_at`) from the caller, and the observed instant travels under one name — `@at` — so the contract is visible in the SQL rather than only in the store. **Deadlines are a different question and deliberately out of scope**: `expires_at`, `idle_expires_at`, `absolute_expires_at` and `locked_until` answer "until when", are policy the application computes, and are correct as caller-supplied values.
  Adding the gate immediately found two more instances (`InsertConnection`, `InsertConnectionPolicyVersion`), which is the point of writing rules as tests.
  `ExtendSessionIdle` was allowlisted here on the grounds that its `now()`s sit in a `WHERE` re-check rather than in a stamp. **That exception is withdrawn** (2026-07-26, round 12): a re-check evaluated against a pre-wait clock does not re-check anything, and the concrete consequence was a slide resurrecting an expired session (ADR-0006).
  The rule therefore reads as written — a write may not consult `now()` either — and the remaining exceptions are all writes with no lock-wait path at all.
- **`payload_digest` is a keyed MAC over payloads/values** (ADR-0003) — e.g. the encrypted request payload of an execution event, stored with `payload_digest_key_version` so rotation keeps it verifiable.
  Auth events carry no payload, so they store neither. **Row-sealing** every audit row (tamper-evidence against the owner/backup path) is an explicit non-goal here; if wanted later it is its own ADR (hash chain / external anchor).
- **The login rate limiter is process-local by design** for the single-instance MVP (bucket parameters in ADR-0010).
  Multiple replicas would multiply the burst budget; shared, per-account failure counters arrive with the progressive-backoff work the PRD requires (PRD §8.3; parameters pinned in ADR-0006, Parameters).

## Consequences
- `auth.Repository`'s transactional methods take an `audit.Event`; the identity domain port no longer declares single-session revocation (it is always audited, and `audit` already imports `identity`, so declaring it there would cycle).
- Tests pin the guarantees: an unmappable event rolls back the whole rotation; `SET ROLE portcullis_runtime` can `INSERT/SELECT` but not `UPDATE/DELETE/ALTER` on `audit_events`.
- Operators get one new production step: create the runtime login user and grant it the role.

## Sources (checked 2026-07-03)
- OWASP Logging Cheat Sheet: https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html
- PostgreSQL ALTER TABLE (owner can disable triggers; `NOT VALID` CHECK then `VALIDATE CONSTRAINT` takes a weaker lock): https://www.postgresql.org/docs/current/sql-altertable.html
- PostgreSQL client settings — `lock_timeout` applies per lock acquisition and `statement_timeout` per statement (checked 2026-10-04): https://www.postgresql.org/docs/current/runtime-config-client.html
- Transactional outbox (single-DB: same-tx write suffices): https://pradeepl.com/blog/transactional-outbox-pattern/
