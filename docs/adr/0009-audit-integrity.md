# ADR-0009: Audit integrity — transactional delivery and the runtime permission boundary

- **Status:** Accepted
- **Date:** 2026-07-03

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
- The store completes `OrganizationID` (single-org MVP) and, for bootstrap, the actor — the created
  user's id exists only inside the transaction.
- **No-state-change events (failed logins) stay best-effort**: a login must not fail because the
  audit store hiccuped. A failed event is recorded on **every failure exit** of Login (a deferred
  catch-all) — wrong credentials, infra errors, and requests aborted mid-flight by a client
  disconnect (ctx cancellation during the lookup or the hash) all leave a trail. The write detaches
  from the request context (`context.WithoutCancel` + a bounded timeout), and a drop is logged
  (types only, never values).
- No outbox: the audit sink is the metadata database itself; same-transaction insert gives strictly
  stronger guarantees with none of the outbox's relay machinery.

### Runtime permission boundary
- Migrations run on `PORTCULLIS_MIGRATE_DATABASE_URL` (a short-lived pool at startup) as the schema
  owner **with `CREATEROLE`** — plain table ownership is not enough to `CREATE ROLE`. Alternatively,
  provision the runtime role beforehand (the migration skips creation when it exists), in which
  case the migrate user needs no `CREATEROLE`. The server then runs on the **runtime DSN**
  (`PORTCULLIS_DATABASE_URL`).
- Migration `0003` creates a cluster-wide **NOLOGIN group role** (no secret in migrations): schema
  usage + table DML, minus `UPDATE/DELETE` on `audit_events`. Deployments create a login user and
  `GRANT <runtime role> TO <user>` (compose does this via an initdb script creating
  `portcullis_app`).
- **Roles are cluster-wide**, so the role name is configurable (`PORTCULLIS_RUNTIME_ROLE`, default
  `portcullis_runtime`; validated as a plain identifier and substituted into the migration SQL).
  Installs sharing one PostgreSQL cluster MUST use distinct names — a shared name would merge
  their privileges. Migration `0003` also **revokes `CONNECT` from `PUBLIC`** on the database and
  grants it explicitly to the runtime role, so roles from other installs cannot even connect
  (the owner keeps `CONNECT` implicitly).
- Append-only is now enforced twice: privileges bind the runtime (no `UPDATE/DELETE`, and — not
  being the owner — no `ALTER TABLE`/trigger changes), and the triggers still guard the owner path
  during migrations/operations. `schema_migrations` is **owner-only** (all runtime access revoked):
  with any access the runtime could delete applied records (forcing re-runs) or pre-insert future
  versions (skipping security migrations).
- **The boundary is verified on BOTH sides at every boot.** (1) The configured group role, via the
  owner connection after migrations (below). (2) The **actual runtime connection's
  `current_user`**, right after the runtime pool connects: it must be a member of the runtime
  role, attribute-safe, unable to `SET ROLE` into the audit-table owner (the owner can disable the
  append-only triggers — owner/superuser DSNs are refused), hold the same effective privilege
  matrix, and be connected to the migrated database. Checking only the role would miss an
  owner/superuser DSN, direct grants on the login user, inherited privileged roles, and
  wrong-database DSNs. Single-role dev (DSN = owner) boots only with the explicit
  `PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME=true` (logged as a warning; never production).
- **The runtime role is verified on every boot**, not only at creation. Before migrations run, a
  pre-existing role is rejected if it holds `SUPERUSER`/`CREATEROLE`/`CREATEDB`/`BYPASSRLS`/
  `REPLICATION` (members can `SET ROLE` into those powers; `LOGIN` is allowed — the role may be
  the login user itself). After migrations, the effective state must hold: CONNECT; on
  `audit_events` SELECT+INSERT and none of UPDATE/DELETE/TRUNCATE/TRIGGER/REFERENCES/MAINTAIN
  (TRIGGER would let a non-owner attach e.g. a BEFORE INSERT trigger that silently blocks the
  trail); on `schema_migrations` no privilege at all. This catches a renamed
  `PORTCULLIS_RUNTIME_ROLE` (migration 0003 is version-recorded, so a new name never receives
  grants) and later drift, failing fast with a pointer here. Existing **memberships** of a
  pre-provisioned role are the operator's responsibility — the server does not audit who may
  `SET ROLE` into it.
- **Role rotation procedure** (changing `PORTCULLIS_RUNTIME_ROLE` after the first boot): as the
  owner, run 0003's statements for the new name, then strip the old role —
  ```sql
  create role <new> nologin;                    -- if not already provisioned
  grant connect on database <db> to <new>;
  grant usage on schema public to <new>;
  grant select, insert, update, delete on all tables in schema public to <new>;
  revoke update, delete on audit_events from <new>;
  revoke all on schema_migrations from <new>;
  alter default privileges in schema public grant select, insert, update, delete on tables to <new>;
  grant <new> to <login user>;
  -- decommission the old role COMPLETELY — default privileges would otherwise
  -- keep granting it DML on every FUTURE table, and the login user would keep
  -- inheriting whatever the old role still holds:
  alter default privileges in schema public
      revoke select, insert, update, delete on tables from <old>;
  revoke all on all tables in schema public from <old>;
  revoke usage on schema public from <old>;
  revoke connect on database <db> from <old>;
  revoke <old> from <login user>;
  drop role <old>;  -- preferred once nothing else depends on it
  ```
  then update the env var and restart.
- Future tables get DML via default privileges; a migration adding a **sensitive** table must
  `REVOKE` in that same migration (rule recorded in `docs/conventions/data.md`).
- Empty `PORTCULLIS_MIGRATE_DATABASE_URL` falls back to the runtime DSN (single-role dev).

### Scope notes
- **`payload_digest` is a keyed MAC over payloads/values** (ADR-0003) — e.g. the encrypted request
  payload of an execution event, stored with `payload_digest_key_version` so rotation keeps it
  verifiable. Auth events carry no payload, so they store neither. **Row-sealing** every audit row
  (tamper-evidence against the owner/backup path) is an explicit non-goal here; if wanted later it
  is its own ADR (hash chain / external anchor).
- **The login rate limiter is process-local by design** for the single-instance MVP. Multiple
  replicas would multiply the burst budget; shared, per-account failure counters arrive with the
  progressive-backoff work the PRD requires (PRD §8.3).

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
