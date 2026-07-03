# ADR-0008: RBAC — permissions in code, roles in the database

- **Status:** Accepted
- **Date:** 2026-06-28

## Context
The PRD originally fixed three roles (admin/approver/requester) as an enum. We need **custom
roles**: an admin must be able to define new roles without a code change. This ADR makes roles
data and keeps permissions as code, and the PRD §4.3/§6 are amended to match.

Standards verified 2026-06-28 (OWASP/oso/WorkOS/Kubernetes RBAC): permissions are **atomic units
defined by the application**; roles are **bundles of permissions stored in the database**; users
get roles via membership; and **authorization is checked against permissions, not role names**.

## Decision

### Permissions — fine-grained catalog in SQL, loaded at startup
- Permissions follow **Google-IAM-style `resource.verb` keys** (dotted, fine-grained). Verbs:
  **`list`** (see the collection), **`get`** (see one record's detail/inner values), `create`,
  `update`, `delete`, plus resource-specific actions. Examples:
  `connections.{list,get,create,update,delete,test}`,
  `requests.{list,get,create,execute,approve,reject}`,
  `users.{list,get,create,update,disable}`, `roles.{list,get,create,update,delete}`,
  `savedqueries.{list,get,create,update,delete,share}`, `policies.{get,update}`,
  `audit.{list,get}`.
- The **catalog lives in SQL**: a seeded `permissions(key, description)` table is the source of truth
  (FK integrity for `role_permissions`; the role UI lists available permissions from it). The app
  **loads the catalog from the database at startup** rather than hardcoding it.
- Go keeps only the `Permission` type. We do **not** maintain a code-side catalog/enum; a specific
  key is referenced only at the exact site that enforces it, when that feature ships.

### Roles = database rows (custom roles allowed)
- `roles(id, organization_id, name, is_system, is_bootstrap_default, created_at)`, unique
  `(organization_id, name)`; at most one `is_bootstrap_default` per org.
- `role_permissions(role_id, permission)` — `permission` is a code-defined catalog key; the app
  validates it against the catalog on write.
- **Custom roles**: admins (with `roles.create`/`roles.update`/`roles.delete` from the catalog)
  create roles and assign any catalog permissions.
- **System roles** (`is_system = true`) are seeded **defaults, not a closed set**: `admin` (all
  permissions), `approver` (create/execute/review requests, saved queries, audit.view),
  `requester` (create/execute requests, saved queries). They cannot be deleted/renamed, but the set
  of roles is open — admins add custom roles, and **the code never enumerates role names** (e.g.
  `role == "admin"`) for authorization; only permissions are checked.

### Membership references a role
- `organization_memberships.role_id → roles(id)` (replacing the text enum).
- Role names live only in SQL (seed data). **Go never hardcodes a role name** — bootstrap assigns
  the role resolved by the `is_bootstrap_default` flag (seeded on `admin`), so even the
  bootstrap-role choice is data, not code.

### Authorization checks permissions, not roles
- The runtime resolves a user's effective permissions (membership → role → role_permissions) and
  checks **`has(permission)`**, never `role == "admin"`. The last active admin protection is
  expressed as "at least one active member with `users.update` **and** `users.disable`" (the
  catalog keys — there is no aggregate `users.manage` permission).

## Consequences
- New domain types: `Permission` (+catalog), `Role{ID, Name, IsSystem, Permissions}`; ports
  `RoleRepository` and permission resolution on the user side. New migration introduces `roles`,
  `role_permissions`, seeds the three system roles, and switches memberships to `role_id`.
- PRD §4.3 amended: roles are DB-stored with a permission set; three system roles are seeded;
  custom roles are supported; authorization is permission-based. §6 gains `roles`/`role_permissions`
  and `organization_memberships.role_id`.
- More moving parts than a fixed enum, but required for custom roles and aligned with standard RBAC.

## Sources (checked 2026-06-28)
- oso RBAC best practices: https://www.osohq.com/learn/rbac-best-practices
- WorkOS RBAC best practices: https://workos.com/blog/rbac-best-practices
- Kubernetes RBAC: https://kubernetes.io/docs/reference/access-authn-authz/rbac/
