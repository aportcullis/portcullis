# Data conventions (PostgreSQL)

## Soft delete, no cascade
- **All deletes are soft** (`deleted_at`; accounts use `status = disabled`, connections
  `archived_at`). Never hard-`DELETE` entity rows.
- **No `ON DELETE CASCADE`** — foreign keys use `RESTRICT`, so deleting/soft-deleting one entity
  never wipes related rows or history. Queries filter `deleted_at is null`. (Join/assignment tables
  that hold current state may be replaced in place.)

## Indexes
Design an index for every lookup and foreign key (e.g. `organization_memberships(user_id)`,
`role_permissions(permission_key)`). Use **partial unique indexes** for one-per-scope invariants
(e.g. one `is_bootstrap_default` role per org, ignoring soft-deleted rows).

## Migrations
Ordered SQL in `migrations/` (embedded, applied at startup, tracked in `schema_migrations`). A
committed migration is **immutable once released** — add a new file, never edit an applied one. Use
`if not exists` / `or replace` so migrations are re-runnable.
The migration runner drops any session-local temporary objects and pins its dedicated session to
`search_path = public` (`pg_catalog` remains implicitly first); never rely on a role/DSN-provided
search path without schema-qualifying every migration.

Migrations run as the **schema owner**; the server runs as the configured runtime role
(`PORTCULLIS_RUNTIME_ROLE`, default `portcullis_runtime`; ADR-0009), which gets DML on new tables
automatically via default privileges. A table is **sensitive** when the
application must not be able to rewrite its rows — concretely: append-only history
(`audit_events`), migration/version bookkeeping (`schema_migrations`), and any future table
whose rows are evidence (immutable artifacts, approval records once terminal). A migration
adding a sensitive table must `REVOKE` the forbidden verbs from the role named by the
`portcullis.runtime_role` migration GUC **in that same migration** and say so in a SQL comment; a
new table without either the revoke or an explicit "not sensitive" judgment in review is a defect.
`schema_migrations` stays **owner-only** — never grant the runtime any access to it (0003 revokes
all; Migrate verifies this on every boot).
Application SQL schema-qualifies metadata relations with `public.`; the runtime has no database
`TEMPORARY`, so a temporary relation cannot shadow a protected table.

## RBAC permissions (ADR-0008)
Permissions are **Google-IAM-style `resource.verb`** (verbs `list`/`get`/`create`/`update`/`delete`
plus resource-specific actions), seeded in SQL and loaded at startup. Roles live in the DB; admins
create custom roles. Authorization checks **permissions, never role names**.

## Org scope (ADR-0004)
Every query over an org-scoped table (anything carrying `organization_id`) **must** filter by the
caller's organization — pass the org explicitly and add `where organization_id = @organization_id`;
never resolve a user's permissions/rows across all orgs. Cross-org references are pinned in the schema
too: a membership's role is constrained by a composite FK `(role_id, organization_id)` so a role can't
be assigned across orgs. ADR-0004 mandates a **single repository choke point that injects the org
predicate** so it can't be forgotten per-call; until that builder exists (lands with the multi-table
Core 1 work), this rule is the review gate — an org-scoped query without the predicate is a defect.
Cross-org isolation is covered by integration tests.
