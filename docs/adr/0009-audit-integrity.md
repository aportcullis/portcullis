# ADR-0009: Audit integrity — transactional delivery and the runtime permission boundary

- **Status:** Accepted (amended 2026-07-04: normative audit schema/index and the detached-write
  timeout transcribed)
- **Date:** 2026-07-03 (amended 2026-07-04)

## Context
The PRD requires structured, **append-only** audit events for authentication (and later, execution),
and a runtime DB role that can only `INSERT/SELECT` on `audit_events`, separate from the migration
owner. The first audit implementation had two integrity gaps:

1. Events were written best-effort *after* the state change committed, so a transient DB failure or
   a cancelled request context could leave a successful bootstrap/login/logout with **no trail**.
2. Append-only was enforced only by triggers, while the server connected as the schema **owner** —
   and PostgreSQL lets the owner `ALTER TABLE ... DISABLE TRIGGER`, so the "immutability" did not
   bind the application at all (verified against the PostgreSQL ALTER TABLE docs).

Standards verified 2026-07-03: OWASP Logging Cheat Sheet (log all authentication successes and
failures); single-database transactional-outbox guidance — when the audit sink is the **same**
database, writing the audit row **in the same ACID transaction** is the standard; an outbox/queue
is only needed for external sinks.

## Decision

### Delivery guarantees
- **State-changing auth operations commit their audit event in the same transaction.** The
  consumer-defined port carries the event into the atomic op:
  `BootstrapAdmin(..., evt)`, `RotateSession(..., evt)`, `RevokeSession(..., evt)` — the store
  inserts it via the transaction-bound queries. A created admin, an issued session, or a revoked
  session can therefore never exist without its trail (and vice versa: if the event can't be
  written, the state change rolls back).
- A first OIDC login extends that invariant: inserting `oidc_identities`, rotating the session, and its
  `AUTH_LOGIN` event (metadata `identity_linked=true`) are one transaction. A failed session/audit commit
  therefore cannot leave a newly usable external authenticator behind without evidence.
- The store completes `OrganizationID` (single-org MVP) and, for bootstrap, the actor — the created
  user's id exists only inside the transaction.
- **No-state-change events (failed logins) stay best-effort**: a login must not fail because the
  audit store hiccuped. A failed event is recorded on **every failure exit** of Login (a deferred
  catch-all) — wrong credentials, infra errors, and requests aborted mid-flight by a client
  disconnect (ctx cancellation during the lookup or the hash) all leave a trail. The write detaches
  from the request context (`context.WithoutCancel` + a **5s** timeout — long enough for a DB
  hiccup, short enough that a stuck store can't accumulate goroutines), and a drop is logged
  (action + error *type* only, never values).
- No outbox: the audit sink is the metadata database itself; same-transaction insert gives strictly
  stronger guarantees with none of the outbox's relay machinery.

### Runtime permission boundary
- Migrations run on `PORTCULLIS_MIGRATE_DATABASE_URL` (a short-lived pool at startup) as the schema
  owner **with `CREATEROLE`** — plain table ownership is not enough to `CREATE ROLE`. Alternatively,
  provision the runtime role beforehand (the migration skips creation when it exists), in which
  case the migrate user needs no `CREATEROLE`. The server then runs on the **runtime DSN**
  (`PORTCULLIS_DATABASE_URL`).
- **The migration principal must be able to act as the owner of the database AND schema `public`**
  (amended 2026-07-05). The boundary migrations `REVOKE ... FROM PUBLIC` *as the owner*; a non-owner
  makes those revokes silent no-ops (PostgreSQL warns but commits), the migration records as
  applied, and the boot-time privilege check then fails **every** boot blaming the runtime role,
  with no migration left to self-repair. A migration **preflight** checks this and fails fast with
  an accurate message (a superuser satisfies it implicitly).
- Migration `0003` creates a cluster-wide **NOLOGIN group role** (no secret in migrations): schema
  usage + table DML, minus `UPDATE/DELETE` on `audit_events`. Deployments create a login user and
  `GRANT <runtime role> TO <user>` (compose does this via an initdb script creating
  `portcullis_app`). Migration **`0005` additionally grants `USAGE` on sequences** (existing and, via
  `ALTER DEFAULT PRIVILEGES`, future) — a table with a serial/identity column needs it for the
  runtime `INSERT`, and without it the server boots fine but fails at first insert (amended
  2026-07-05). A sensitive future table must still `REVOKE` in its own migration (data.md).
- *Amended 2026-07-18 (external review):* **the runtime role holds hard `DELETE` on NO table.**
  Every entity is soft-delete-only (data.md), not a single runtime query issues `DELETE`, and
  history removal is a separate retention concern (PRD §4.3) — so 0003's blanket `DELETE` grant
  violated least privilege: an application SQL defect or a compromised process could permanently
  destroy `users`/`roles`/`sessions`. Migration **`0010` revokes `DELETE`** on all current tables
  and from the owner's per-schema default privileges (a per-schema ADP `REVOKE` exactly reverses
  the previous per-schema ADP `GRANT` for the same defining role — web-verified), and the boot
  matrix's **default table policy now forbids `DELETE`** alongside the schema-shaping verbs. A
  table that genuinely needs runtime `DELETE` (e.g. a result-cache TTL eviction, ADR-0011) grants
  it in its own migration and registers an explicit per-table policy exception — the same gate
  sensitive tables already pass through.
- **Roles are cluster-wide**, so the role name is configurable (`PORTCULLIS_RUNTIME_ROLE`, default
  `portcullis_runtime`; validated as a plain identifier). Migrate publishes it as the
  `portcullis.runtime_role` session GUC on the migration connection; `0003` reads it via
  `current_setting` and splices it with `format(%I)` — no blind text substitution (which could
  rewrite an unrelated substring in a future migration, or leak the dev-membership grant). Installs
  sharing one PostgreSQL cluster MUST use distinct names — a shared name would merge their
  privileges. The compose dev-membership grant (`GRANT <role> TO portcullis_app`) fires **only for
  the default role name**, so a custom-name install never grants its role to a foreign install's
  login user. Migration `0003` also **revokes `CONNECT` from `PUBLIC`** on the database and grants it
  explicitly to the runtime role, so roles from other installs cannot even connect (the owner keeps
  `CONNECT` implicitly). It also revokes database `TEMPORARY` from `PUBLIC`: PostgreSQL searches a
  session's temporary schema before permanent relations, so a temporary `audit_events` could
  otherwise divert an unqualified audit insert. Application and verification SQL schema-qualifies
  metadata relations (`public.audit_events`, etc.) as the second half of this defense.
  Because `0003` is version-recorded on databases migrated before this rework (and applied
  migrations are immutable — data.md), migration **`0004` re-establishes the `PUBLIC` revocations**
  (database `TEMPORARY`/`CREATE`, schema `USAGE`/`CREATE`) idempotently, so pre-rework databases
  pass the boot-time privilege verification instead of failing it forever.
- Append-only is now enforced twice: privileges bind the runtime (no `UPDATE/DELETE`, and — not
  being the owner — no `ALTER TABLE`/trigger changes), and the triggers still guard the owner path
  during migrations/operations. `schema_migrations` is **owner-only** (all runtime access revoked):
  with any access the runtime could delete applied records (forcing re-runs) or pre-insert future
  versions (skipping security migrations).
- **The boundary is verified on BOTH sides at every boot.** (1) The configured group role, via the
  owner connection after migrations (below). (2) The **actual runtime connection's
  `session_user`**, right after the runtime pool connects, which must:
  be a member of the runtime role; hold no dangerous attribute directly; **not own — nor be able to
  `SET ROLE` into the owner of — the database, the `public` schema, or `audit_events`/
  `schema_migrations`** (any such owner can `DROP`/`ALTER` the audit table or disable its triggers,
  e.g. `DROP SCHEMA public CASCADE`); **be able to `SET ROLE` into no role other than itself and the
  runtime role** — checked as `pg_has_role(..., 'SET')` **or** `pg_has_role(..., 'MEMBER WITH ADMIN
  OPTION')`, separately from mere `MEMBER`, so inert `SET FALSE, INHERIT FALSE` memberships remain
  valid but a `WITH ADMIN OPTION` membership is rejected even when granted `SET FALSE` (the member
  can grant itself `SET` and then escalate) (amended 2026-07-05); hold no `TEMPORARY` privilege; satisfy the
  same effective privilege matrix; and be connected to the migrated database. `session_user` is the
  floor identity because a connection whose `current_user` was masked by a login-time role setting
  can always execute `SET ROLE NONE` to recover the authenticated principal's powers.
  Checking only the role would miss an owner/superuser DSN, direct grants on the login user,
  SET-ROLE-reachable privileged roles, and wrong-database DSNs.
  Only **over-privilege** violations (the above) are downgraded to a warning by the explicit
  `PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME=true` (dev only; classified via `ErrRuntimeInsecure`). A wrong
  or unmigrated database, a missing required privilege, **non-membership (always — even alongside an
  over-privilege violation)**, or a verification-query failure is **always fatal** — the flag can't
  mask a misconfigured DSN into a falsely-healthy boot. Membership is checked **before** the
  remaining over-privilege classes, so a wrong principal that also happens to hold excess privilege
  is reported as the non-member it is, not downgraded (amended 2026-07-05). The only shapes the flag
  permits are the intentional single-role dev setups — owner or superuser — which are caught by the
  owner-reach / dangerous-attribute checks that precede the membership check.
- **The runtime role is verified on every boot**, not only at creation. Before migrations run, a
  pre-existing role is rejected if it holds `SUPERUSER`/`CREATEROLE`/`CREATEDB`/`BYPASSRLS`/
  `REPLICATION` (members can `SET ROLE` into those powers; `LOGIN` is allowed — the role may be
  the login user itself). After migrations, the effective state must hold: CONNECT; **USAGE on
  schema `public`** (table privileges evaluate independently of schema USAGE, so without this
  check a role missing only USAGE would verify "healthy" and then fail every runtime query); on
  `audit_events` SELECT+INSERT and none of UPDATE/DELETE/TRUNCATE/TRIGGER/REFERENCES/MAINTAIN
  (TRIGGER would let a non-owner attach e.g. a BEFORE INSERT trigger that silently blocks the
  trail); on `schema_migrations` no privilege at all; and no database `TEMPORARY`. This catches a renamed
  `PORTCULLIS_RUNTIME_ROLE` (migration 0003 is version-recorded, so a new name never receives
  grants) and later drift, failing fast with a pointer here. Existing **memberships** of a
  pre-provisioned role are the operator's responsibility — the server does not audit who may
  `SET ROLE` into it.
- **Role rotation procedure** (changing `PORTCULLIS_RUNTIME_ROLE` after the first boot): as the
  owner, run 0003's statements for the new name, then strip the old role —
  ```sql
  create role <new> nologin;                    -- if not already provisioned
  revoke temporary on database <db> from public;
  grant connect on database <db> to <new>;
  grant usage on schema public to <new>;
  -- No DELETE anywhere: hard delete is revoked for the runtime (0010, amendment
  -- below) — a rotation must not resurrect it, or boot verification fails on
  -- every start with no migration left to self-repair.
  grant select, insert, update on all tables in schema public to <new>;
  grant usage on all sequences in schema public to <new>;
  revoke update on public.audit_events from <new>;
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
  revoke usage on schema public from <old>;
  revoke connect on database <db> from <old>;
  revoke <old> from <login user>;
  drop role <old>;  -- preferred once nothing else depends on it
  ```
  then update the env var and restart. `TestRuntimeRoleRotationRunbook` executes this
  runbook verbatim against a database with a real sequence — keep the two statement
  lists in sync by hand.
- Future tables get DML via default privileges; a migration adding a **sensitive** table must
  `REVOKE` in that same migration (rule recorded in `docs/conventions/data.md`).
- Empty `PORTCULLIS_MIGRATE_DATABASE_URL` falls back to the runtime DSN (single-role dev).

### Migrator identity (amended 2026-07-13 — external review)
- `ALTER DEFAULT PRIVILEGES` binds to the **creating role** and is never inherited through
  membership (PostgreSQL docs). A member-of-owner migrating without `SET ROLE` therefore
  creates tables the runtime role has **no grants on** — boot used to pass (the old postflight
  checked only audit/history) and the first RPC failed. Fixed on three fronts:
  1. **`Migrate` runs AS the schema owner**: it resolves the owner (PG15+: schema `public`
     belongs to the `pg_database_owner` pseudo-role, so the real target is `datdba`; a
     reassigned schema resolves to its owner and must also cover the database-level revokes)
     and `SET ROLE`s into it (`set_config('role', …)`, `RESET ROLE` on exit; a failed reset
     destroys the pooled connection). The migrator must **be** the owner or hold **SET-capable
     membership** (`GRANT <owner> TO <migrator> WITH SET TRUE`) — inherit-only membership is
     refused, since it can run REVOKEs but can never fix the default-privilege binding. A
     superuser that is not `datdba` also SET ROLEs; its objects now belong to `datdba`
     (behavior change). A legacy `schema_migrations` owned by a previous member migrator is
     re-owned before the switch.
  2. **Migration `0008_owner_grant_repair`** re-grants runtime DML on everything that exists,
     re-binds the default privileges to the owner, and re-applies the sensitive revokes — so
     databases whose grants were bound to a previous migrator self-repair. Residual limit:
     ownership of tables created by an old member migrator is not normalized (manual
     `REASSIGN OWNED` if a future migration must ALTER them); grants, the failure class, are.
  3. **Boot verification covers every table and sequence**: `verifyTablePrivileges` checks a
     per-table policy (default: SELECT/INSERT/UPDATE required, DELETE/TRUNCATE/TRIGGER/
     REFERENCES/MAINTAIN forbidden — no-hard-delete is the baseline since 0010; exceptions:
     `audit_events` S+I only, `schema_migrations` nothing — the code form of data.md's
     sensitive-table rule) plus sequence USAGE (UPDATE forbidden). Unknown new tables get the default policy, so a
     migration that forgets grants fails its very first boot in CI; a new sensitive table
     fails until its policy entry lands — the review gate, enforced. In the runtime-connection
     check a missing required verb is always fatal; a forbidden one is the dev-downgradable
     over-privilege class.

### Audit table shape (normative — mirrors migration 0001)
- Columns: `id uuid pk`, `organization_id` (FK, restrict), `occurred_at timestamptz default
  now()`, `actor_type` (`check in ('user','system','service')`), `actor_user_id` (nullable FK),
  `actor_service`, `action`, `target_type`, `target_id`, `outcome`, `previous_state`,
  `next_state`, `payload_digest bytea`, `payload_digest_key_version int`, `request_id`,
  `connection_id uuid`, `query_type`, `rows_affected bigint`, `duration_ms bigint`,
  `risk_score double precision`, `metadata jsonb not null default '{}'`.
- **Digest pairing is CHECK-enforced**, not convention:
  `(payload_digest is null) = (payload_digest_key_version is null)` (a digest without its key
  version is unverifiable; a version without a digest is meaningless), and
  `payload_digest_key_version > 0` when present.
- **Timeline index:** `(organization_id, occurred_at desc, id desc)` — the org-scoped
  reverse-chronological list is the only hot read path, and the `id desc` tail is the
  pagination tie-breaker (PRD §7.1) so page boundaries are stable when timestamps collide.
- **Append-only needs two triggers** (plus the privilege boundary below): a row-level
  `before update or delete` trigger and a **separate statement-level `before truncate`**
  trigger — row-level triggers do not fire on TRUNCATE. Both call one `raise exception`
  function.

### Scope notes
- **`payload_digest` is a keyed MAC over payloads/values** (ADR-0003) — e.g. the encrypted request
  payload of an execution event, stored with `payload_digest_key_version` so rotation keeps it
  verifiable. Auth events carry no payload, so they store neither. **Row-sealing** every audit row
  (tamper-evidence against the owner/backup path) is an explicit non-goal here; if wanted later it
  is its own ADR (hash chain / external anchor).
- **The login rate limiter is process-local by design** for the single-instance MVP (bucket
  parameters in ADR-0010). Multiple replicas would multiply the burst budget; shared,
  per-account failure counters arrive with the progressive-backoff work the PRD requires
  (PRD §8.3; parameters pinned in ADR-0006, Parameters).

## Consequences
- `auth.Repository`'s transactional methods take an `audit.Event`; the identity domain port no
  longer declares single-session revocation (it is always audited, and `audit` already imports
  `identity`, so declaring it there would cycle).
- Tests pin the guarantees: an unmappable event rolls back the whole rotation; `SET ROLE
  portcullis_runtime` can `INSERT/SELECT` but not `UPDATE/DELETE/ALTER` on `audit_events`.
- Operators get one new production step: create the runtime login user and grant it the role.

## Sources (checked 2026-07-03)
- OWASP Logging Cheat Sheet: https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html
- PostgreSQL ALTER TABLE (owner can disable triggers): https://www.postgresql.org/docs/current/sql-altertable.html
- Transactional outbox (single-DB: same-tx write suffices): https://pradeepl.com/blog/transactional-outbox-pattern/
