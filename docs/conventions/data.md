# Data conventions (PostgreSQL)

## Soft delete, no cascade
- **Soft-delete entities**
  - Use `deleted_at`, account `status = disabled`, or connection `archived_at`; never hard-delete entity rows.
  - Filter active entity queries by their deletion marker.
- **Preserve relationships and history**
  - Use FK `RESTRICT`; never `ON DELETE CASCADE`.
  - Current-state join and assignment tables may be replaced in place.

## Indexes
Design an index for every lookup and foreign key (e.g. `organization_memberships(user_id)`, `role_permissions(permission_key)`).
A non-partial index must lead with each foreign key's first column; `TestForeignKeysHaveCoveringIndexes` enforces this over `public` and `result_cache`.
Use **partial unique indexes** for one-per-scope invariants (e.g. one `is_bootstrap_default` role per org, ignoring soft-deleted rows).

## Migrations
Ordered SQL in `migrations/` (embedded, applied at startup, tracked in `schema_migrations`).
A committed migration is **immutable once released** — add a new file, never edit an applied one.
The runner records each file's sha256 in `schema_migrations.checksum` and refuses to start when an applied file's checksum changed or the database records a version the binary does not ship (ADR-0009).
Use `if not exists` / `or replace` so migrations are re-runnable.

- **Keep migration locks short**
  - Each file runs with `lock_timeout` 5s (retried as a whole) and `statement_timeout` 15m; design files to fit those bounds.
  - Add a CHECK or foreign key to a populated table as `NOT VALID`, then `VALIDATE CONSTRAINT` in a later statement, so validation takes `SHARE UPDATE EXCLUSIVE` instead of blocking writes; released 0017 predates this rule and is not edited.
The migration runner drops any session-local temporary objects and pins its dedicated session to `search_path = public` (`pg_catalog` remains implicitly first); never rely on a role/DSN-provided search path without schema-qualifying every migration.

- **Separate migration and runtime privileges**
  - Migrate as the schema owner; serve with `PORTCULLIS_RUNTIME_ROLE`, defaulting to `portcullis_runtime` (ADR-0009).
  - Default privileges grant runtime DML on new tables.
  - Keep `schema_migrations` owner-only; boot verification enforces this boundary.
- **Protect evidence tables**
  - Treat append-only history, version bookkeeping, immutable artifacts, and terminal approval evidence as sensitive.
  - Revoke forbidden verbs from the GUC-named runtime role in the same migration and explain the boundary in a SQL comment.
  - Every new table needs either the revokes or an explicit non-sensitive classification.
- **Prevent relation shadowing**
  - Qualify application metadata relations with `public.`.
  - Runtime roles hold no database `TEMPORARY` privilege.

## Authorization permissions (ADR-0008/0062)
Permissions are **Google-IAM-style `resource.verb`** (verbs `list`/`get`/`create`/`update`/`delete` plus resource-specific actions), seeded in SQL and loaded at startup.
Roles live in the DB; admins create custom roles.
Authorization checks **permissions, never role names**.

[ADR-0062](../adr/0062-role-bindings-and-conditions.md) and [ADR-0071](../adr/0071-binding-authorization-activation.md) use independent membership and scoped role bindings with optional conditions. Evaluate permission, resource scope and condition together; a flat union of role keys cannot authorize an operation. Migration 0025 preserves current grants and retires membership `role_id` behind an always-NULL constraint. Keep binding/condition versions organization-scoped with composite FKs, explicit indexes, soft revocation and protected history; do not rewrite existing migrations. Runtime can insert/update Binding heads, append immutable versions and advance existing organization revisions; only the owner creates revisions. Acquire the organization policy coordinator before connection, request, execution and session locks, and never hold it across target SQL or network output.

## Org scope (ADR-0004)
Every query over an org-scoped table (anything carrying `organization_id`) **must** filter by the caller's organization — pass the org explicitly and add `where organization_id = @organization_id`; never resolve a user's permissions/rows across all orgs.
Cross-org references are pinned in the schema too: a membership's role is constrained by a composite FK `(role_id, organization_id)` so a role can't be assigned across orgs.
ADR-0004 mandates a **single repository choke point that injects the org predicate** so it can't be forgotten per-call; until that builder exists (lands with the multi-table Core 1 work), this rule is the review gate — an org-scoped query without the predicate is a defect.
Cross-org isolation is covered by integration tests.
