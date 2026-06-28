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

## RBAC permissions (ADR-0008)
Permissions are **Google-IAM-style `resource.verb`** (verbs `list`/`get`/`create`/`update`/`delete`
plus resource-specific actions), seeded in SQL and loaded at startup. Roles live in the DB; admins
create custom roles. Authorization checks **permissions, never role names**.
