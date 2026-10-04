# ADR-0008: RBAC — permissions in code, roles in the database

- **Status:** Accepted (amended 2026-07-04: the seeded catalog and system-role grants are transcribed as the normative appendix; column/name fixes to match the shipped schema; amended 2026-10-04: enforced-key constants and startup catalog coverage check)
- **Date:** 2026-06-28 (amended 2026-07-04)

## Context
The PRD originally fixed three roles (admin/approver/requester) as an enum.
We need **custom roles**: an admin must be able to define new roles without a code change.
This ADR makes roles data and keeps permissions as code, and the PRD §4.3/§6 are amended to match.

Standards verified 2026-06-28 (OWASP/oso/WorkOS/Kubernetes RBAC): permissions are **atomic units defined by the application**; roles are **bundles of permissions stored in the database**; users get roles via membership; and **authorization is checked against permissions, not role names**.

## Decision

### Permissions — fine-grained catalog in SQL, loaded at startup
- Permissions follow **Google-IAM-style `resource.verb` keys** (dotted, fine-grained).
  Verbs: **`list`** (see the collection), **`get`** (see one record's detail/inner values), `create`, `update`, `delete`, plus resource-specific actions.
  Examples: `connections.{list,get,create,update,delete,test}`, `requests.{list,get,create,execute,approve,reject}`, `users.{list,get,create,update,disable}`, `roles.{list,get,create,update,delete}`, `savedqueries.{list,get,create,update,delete,share}`, `policies.{get,update}`, `audit.{list,get}`.
- The transport reflects that boundary: a `list` response carries only a collection-safe summary, while target coordinates, TLS settings, audit correlation/network fields, and metadata appear only in a `get` response.
  Mutation responses use the same summary shape, so `create`/`update`/`delete` never accidentally become a detail-read grant.
- The **catalog lives in SQL**: a seeded `permissions(key, description)` table is the source of truth (FK integrity for `role_permissions`; the role UI lists available permissions from it).
  The app **loads the catalog from the database at startup** rather than hardcoding it.
- Go keeps the `Permission` type and, since 2026-10-04, one set of **enforced-key constants** in `internal/domain/identity` (`EnforcedPermissions`).
  This is not a code-side catalog: the catalog, its descriptions and role grants still come only from SQL, and a key enters the constant set only when an enforcement site that checks it ships.
  Keys previously spelled as string literals or per-handler constants could drift from the catalog, and a typo surfaced only as `Internal` on the first request that reached it (`ErrUnknownPermission`).
  Startup now runs `ValidatePermissionCatalog` after loading the catalog and **refuses to serve** when any enforced key is missing, the same fail-fast stance as the keyring and runtime-connection checks.
  A source-level test requires every transport enforcement site to pass one of these constants, and storage adapters that check a key in SQL should adopt the same constants.

### Roles = database rows (custom roles allowed)
- `roles(id, organization_id, name, is_system, is_bootstrap_default, created_at, deleted_at)` — soft-deleted, never hard-deleted (`docs/conventions/data.md`), plus `unique (id, organization_id)` as the composite-FK target so a membership's role must belong to the membership's own org (ADR-0004).
- Partial unique indexes (both ignore soft-deleted rows, so a deleted role frees its name): `roles_org_name` on `(organization_id, name)`, and `roles_one_bootstrap_default` on `(organization_id) where is_bootstrap_default` — at most one bootstrap default per org.
- `role_permissions(role_id, permission_key)` — `permission_key` FKs `permissions(key)` (the catalog is the referential source of truth), primary key `(role_id, permission_key)`, plus a reverse-lookup index on `permission_key`.
- **Custom roles**: admins (with `roles.create`/`roles.update`/`roles.delete` from the catalog) create roles and assign any catalog permissions they themselves hold; ADR-0053 defines the administration services, escalation guard and last-administrator check.
- **System roles** (`is_system = true`) are seeded **defaults, not a closed set** — their exact permission sets are in the appendix below.
  They cannot be deleted/renamed, but the set of roles is open — admins add custom roles, and **the code never enumerates role names** (e.g. `role == "admin"`) for authorization; only permissions are checked.

### Membership references a role
- `organization_memberships.role_id → roles(id)` (replacing the text enum).
- Role names live only in SQL (seed data). **Go never hardcodes a role name** — bootstrap assigns the role resolved by the `is_bootstrap_default` flag (seeded on `admin`), so even the bootstrap-role choice is data, not code.

### Authorization checks permissions, not roles
- The runtime resolves a user's effective permissions (membership → role → role_permissions) and checks **`has(permission)`**, never `role == "admin"`.
  The last active admin protection is expressed as "at least one active member with `users.update` **and** `users.disable`" (the catalog keys — there is no aggregate `users.manage` permission).

## Appendix — seeded catalog & system-role grants (normative, mirrors migration 0002)
The SQL seed remains the runtime source of truth (loaded at startup); this appendix is its documentation-side mirror — a divergence between the two is a defect.
Adding a permission = a new migration inserting the key **and** an amendment here.

**Catalog (35 keys; `settings.*` added by migration 0012, ADR-0017):**
- `users.{list,get,create,update,disable}`
- `roles.{list,get,create,update,delete}`
- `connections.{list,get,create,update,delete,test}`
- `policies.{get,update}`
- `requests.{list,get,create,execute,approve,reject}`
- `savedqueries.{list,get,create,update,delete,share}`
- `audit.{list,get}`
- `settings.{list,get,update}`

**System-role grants (seeded for the default org; `admin` carries `is_bootstrap_default`):**
| Role | Permissions |
|---|---|
| `admin` | every catalog key (seeded as a cross join — a key added in a later migration must extend the admin grant in that same migration) |
| `approver` | `requests.{list,get,create,execute,approve,reject}`, `savedqueries.{list,get,create,update,delete,share}`, `audit.{list,get}` |
| `requester` | `requests.{list,get,create,execute}`, `savedqueries.{list,get,create,update,delete}` |

(Prose shorthands like "review requests" or "audit view" in earlier drafts meant `requests.approve`/`requests.reject` and `audit.list`/`audit.get` — only the keys above exist.)

**A feature must not borrow another resource's key** (amended 2026-07-24).
`requester`/`approver` deliberately hold no `connections.*`: connection administration is the admin's.
But making a request needs to *name* a connection, and the first implementation reused `connections.list` for the request form's picker — which silently made the request flow unusable for exactly the roles it exists for.
The rule this fixes: when a feature needs a sliver of another resource, it gets **its own narrow surface under its own permission** (here `AccessRequests.ListRequestableConnections`, gated by `requests.create`, returning active connections with only the fields the form needs), never a widened grant of the other resource's key.
Grants stay least-privilege and the read surface stays as narrow as the use case — the same reasoning that keeps authorization permission-based rather than role-name-based.
SPA navigation and landing routes are permission-aware for the same reason: a default-role user must never be redirected into a page their role cannot open.

## Consequences
- New domain types: `Permission` (+catalog), `Role{ID, Name, IsSystem, Permissions}`; ports `RoleRepository` and permission resolution on the user side.
  Migration 0002 introduces `permissions`/`roles`/`role_permissions`, seeds the catalog and the three system roles, and switches memberships to `role_id` (composite FK `(role_id, organization_id)`).
- PRD §4.3 amended: roles are DB-stored with a permission set; three system roles are seeded; custom roles are supported; authorization is permission-based. §6 gains `roles`/`role_permissions` and `organization_memberships.role_id`.
- More moving parts than a fixed enum, but required for custom roles and aligned with standard RBAC.

## Sources (checked 2026-06-28)
- oso RBAC best practices: https://www.osohq.com/learn/rbac-best-practices
- WorkOS RBAC best practices: https://workos.com/blog/rbac-best-practices
- Kubernetes RBAC: https://kubernetes.io/docs/reference/access-authn-authz/rbac/
