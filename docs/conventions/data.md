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

Migrations run as the **schema owner**; the server runs as `portcullis_runtime` (ADR-0009), which
gets DML on new tables automatically via default privileges. A migration adding a **sensitive**
table (append-only or restricted, like `audit_events`) must `REVOKE` the forbidden verbs from
`portcullis_runtime` **in that same migration**. `schema_migrations` stays **owner-only** — never
grant the runtime any access to it (0003 revokes all; Migrate verifies this on every boot).

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
