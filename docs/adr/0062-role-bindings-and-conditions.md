# ADR-0062: Separate roles, resource bindings and conditions

- **Status:** Accepted
- **Date:** 2026-10-16
- **Implementation refinement:** [ADR-0065](0065-restricted-cel-condition-evaluation.md) pins the upstream canonical module and concrete versioned evaluator limits; the original adapter naming below records the decision at its date.
- **Supersedes:** [ADR-0008](0008-rbac-roles-and-permissions.md) membership-based role assignment and flat effective-permission resolution; its permission catalog, startup validation and role-definition rules remain binding.
- **Supersedes:** [ADR-0053](0053-user-and-role-administration.md) single-role assignment, delegation comparison, administrator definition and administration API shape; its password setup, account protection, atomic audit and session safeguards remain binding as refined below.

## Context

The owner selected a Google-IAM-style separation of Role, Binding and Condition for Portcullis. A role defines operations, a binding grants that role to a principal within a resource scope, and an optional condition determines when that grant applies. This is the M1 authorization boundary, not a claim that the current code implements it or that Portcullis implements the full Google IAM product.

The current implementation attaches one role to `organization_memberships.role_id`. The authorization service resolves an organization-wide permission list, transport calls `Authorize(user, permission)` without a resource, and session responses expose one role label. Membership therefore combines organization participation with an authorization grant; a user cannot hold different roles on different connections. Flat permission-set comparison in administration also loses scope and conditions.

Google's allow policies bind principals to roles on resources; optional CEL conditions constrain individual grants. Grants are additive: a conditional binding cannot restrict another unconditional grant. Portcullis adopts that model while retaining its own organization isolation, approval workflow and immediate authorization checks. Google IAM's eventual-consistency model is not adopted.

## Options

- Keep a single organization-wide role and add special-case connection checks. This leaves assignment, scope and evaluation coupled and makes future conditions inconsistent across endpoints.
- Separate roles and scoped bindings, but defer all conditions. This establishes scope but does not satisfy the selected conditional-grant model.
- Separate roles, bindings and a bounded condition language, with one resource-aware authorization contract (chosen). This adds migration and evaluation work but avoids embedding resource or time rules into role definitions.

## Decision

### Roles, memberships and bindings

- Keep the SQL permission catalog, `role_permissions`, custom roles, read-only system roles and startup enforced-key coverage. Role definitions contain permission keys and display metadata, never principals, resource selectors or conditions.
- Membership records participation in an organization, independently of roles. An active account with membership but no applicable binding has no protected capability. One principal may hold multiple bindings and roles in the same organization.
- M1 principals are existing local users identified by immutable IDs. Google/Keycloak authenticate those users; provider role/group claims do not create grants. Groups, service principals and agent grants remain their existing later scopes.
- A binding associates exactly one user, one role, one organization, a scope and an optional condition. Multiple rows represent multiple principals; this storage choice is not Google's multi-member policy document format.
- M1 scopes are organization and exact connection. Organization bindings cover applicable resources in that organization; connection bindings cover that connection and its policy, requests, executions and results through server-resolved ancestry. They never cover other connections or organization administration. Connection creation, users, roles, bindings, settings and audit use organization scope. Audit access remains organization-wide; binding a role to a connection does not grant audit access.
- Every permission enforcement site declares its resource type and applicable scope. Unsupported combinations grant nothing; they never fall back to organization scope. M3 query assets and later resources must define their mapping before using connection-scoped grants; there is no implicit ancestry from a referenced SQL object or arbitrary client-supplied ID.

### Conditions

Condition is a separate domain value owned by a binding version, with title, optional description, expression and language version. It is not part of Role and is not a shared mutable condition registry: changing one binding cannot silently change another grant.

Use a restricted CEL environment behind a consumer-owned evaluator port; `google/cel-go` is the chosen adapter. The domain imports no CEL library. Pin and directly declare the dependency when the adapter is implemented; this documentation change neither adds a dependency nor qualifies an evaluator release.

The initial attribute vocabulary is server-resolved `principal.id`, `principal.organization_id`, `resource.type`, `resource.id`, `resource.organization_id`, `resource.connection_id` when applicable, and `request.time`. IDs are opaque strings and time is a UTC timestamp. No attributes come directly from claimed browser roles, headers or submitted resource ancestry. There are no SQL text, parameters, credentials, result values, arbitrary labels, IdP claims, network attributes or external lookups in conditions.

Allow typed equality/inequality, ordering of timestamps, membership in bounded literal ID lists, Boolean composition and timestamp literals. Reject macros/comprehensions, dynamic evaluation, regex, custom I/O functions, unknown attributes and non-Boolean results. Type-check and validate the supported AST on writes, and reject an empty or invalid expression rather than treating it as absent. Only an explicit absent condition is unconditional. Unknown language versions cannot serve grants.

Source length, AST depth/node count, literal-list size, bindings examined, evaluation cost, deadline and concurrent evaluation have explicit finite limits owned by evaluator configuration. Implementation must select and measure those limits before acceptance; exceeding a validation limit rejects the write and exceeding a runtime limit never grants access. Compiled programs may be cached by immutable binding version and language version; authorization results must not be cached as session-wide permissions.

### Authorization semantics and resource resolution

For an authenticated active member, allow an operation only when at least one live binding matches the principal, organization and resource scope, its live role contains the required permission, and its condition is absent or evaluates to Boolean true. Conditions combine within their expression; applicable bindings combine by union. A false condition, missing/inapplicable attribute or evaluation error contributes no grant from that binding. A resolver/storage failure fails the entire operation closed with a generic server error, rather than falling back to previously resolved permissions.

The authorization decision also requires all fixed governance guards: organization isolation, requester/result ownership, no self-approval, active distinct approvers, quorum, approval expiry, immutable payload/config/policy, archive checks and single-use execution. Neither an administrator nor a true condition bypasses those guards. No explicit deny policies, principal access boundaries, role hierarchy or custom policy functions are introduced in M1. The management UI explains that adding a restrictive binding cannot narrow an existing broader grant.

The application authorization port takes principal, permission, a typed resource reference and trusted evaluation context. Transport admission alone is insufficient. Load only minimal metadata under an organization predicate to resolve ancestry before authorization; decrypt SQL, return details, access results or contact a target only after authorization. Foreign and inaccessible IDs retain generic non-enumerating responses.

List, count, picker, review eligibility and batch paths must evaluate the same rules per candidate. Apply safe indexed scope prefilters, then the same condition evaluator and existing visibility rules before pagination and totals. Never paginate an unfiltered list and merely hide rows afterward, and never expose an unfiltered total. Use one server time and policy revision for a page/count decision. A bounded scan that cannot produce an authorized page/count within its budget returns an explicit unavailable/error response, not a misleading total or an unbounded in-memory scan. Fence pagination continuation against principal, filter and policy revision changes.

`Auth.Me`/login capability data becomes advisory and resource-aware; one global permission array or role label cannot represent authority. Display multiple role bindings with scope/condition, and obtain authorized navigation, target choices and actions from server evaluation. Compatibility fields must never flatten a conditional or connection-only grant into unconditional authority. Open forms, in-flight responses and cached resources are invalidated on identity or authorization changes.

### Management and escalation protection

Add catalog keys `bindings.{list,get,create,update,delete}`; the seeded admin role receives them in the introducing migration. Membership creation and binding creation are distinct operations even when the UI offers them in one atomic user-creation workflow. Creating a user requires `users.create`; including initial bindings additionally requires `bindings.create` and the full delegation checks. Replace the planned `Users.AssignRole` surface with explicit binding operations; a new account can have no grant, or be created with validated initial bindings atomically with its setup token and audit evidence. Roles remain reusable definitions; show binding usage rather than a single member count.

Binding updates and revocation require the expected version. Updating a condition explicitly preserves, replaces or clears it; an omitted update field preserves the existing condition. Refuse stale versions and unsupported language/contract versions so an older client cannot silently strip a condition and create an unconditional grant.

M1 policy writes require an applicable unconditional organization-wide management grant. Additionally, the actor must hold every permission in the old and new affected roles through unconditional organization-wide bindings. Apply that comparison to role edits, binding creation/update/revocation and user disable/enable, including every live binding of an affected user. A narrower or conditional grant cannot be widened by copying its permission keys. Creating an empty role is permitted with the required management grant; editing it later repeats the full comparison. Delegated scoped/conditional policy administration and logical implication between arbitrary conditions are deferred.

Do not disable/enable oneself or change one's own bindings. Shared-role edits still require the complete old/new permission comparison and last-administrator check; changing a role is not an escape from binding safeguards. Role deletion is refused while any non-revoked binding references it, including an expired conditional binding; explicitly revoke that binding first.

At least one active organization member must retain all `users.*`, `roles.*` and `bindings.*` catalog permissions through unconditional organization-wide live bindings, possibly multiple roles. This defines the recovery administrator by capabilities, not a role name. Conditional grants never satisfy recovery protection, even when currently true. Bootstrap atomically creates membership and an unconditional organization binding to the role selected by `is_bootstrap_default`. This replaces the old last-admin predicate of only `users.update` plus `users.disable`.

### Revocation, approvals, execution and audit

Role/binding/account mutations and governance admission checks serialize through the same per-organization transaction lock. Acquire it before connection, request and execution locks; observe authoritative database wall-clock time after the locks, then recheck current bindings, role permissions, conditions and workflow guards before recording a decision or execution lease. Lock order must be shared by approval, administration, password setup and execution paths. Commit before target I/O; never hold the lock throughout SQL execution or automatically retry target SQL.

At execution admission, re-evaluate the requester's execute grant and each distinct approver's approve grant against the stored request/connection using the current trusted time. A revoked, out-of-scope or expired conditional grant cannot count toward quorum. Preserve `approval_invalidated` when an approved request falls below quorum. An authorization change does not alter the approved payload or inherit approval into a different resource.

Binding/role changes revoke affected users' sessions and invalidate authorization caches in the same metadata transaction. Time expiry requires no mutation or background revocation job: every protected decision evaluates time afresh. Recheck before disclosing each bounded result/CSV chunk; stop further output after revocation or expiry, fence obsolete browser completions, and never claim to recall bytes already delivered. Revocation after a committed execution lease does not undo target SQL or make it safe to replay; cancellation and uncertain outcomes retain ADR-0021 semantics.

Advance an organization authorization revision for role, binding and account changes. Audit policy mutations atomically with actor, resource scope, role/binding versions and revision, preserving before/after grant evidence. Authorization evidence records matched binding/role versions, evaluation time and reason codes alongside existing approval/execution events. Keep raw expression text, user identity strings and diagnostic internals out of ordinary logs; protected binding-version records retain expressions for authorized inspection. Retain the existing best-effort refusal audit policy and bounds rather than adding unbounded audit writes for every row.

### Persistence and migration

The logical target model is `roles`/`role_permissions`, independent `organization_memberships`, versioned `role_bindings`, binding-owned immutable condition versions and organization authorization revision. Bindings retain subject/role/scope IDs, optimistic version, creation/revocation timestamps and history. Composite organization FKs enforce that subject membership, role and connection belong to the same organization; scope shape checks and indexes cover authorization lookups and every FK. Use soft revocation and `RESTRICT`, without cascades, and classify/restrict immutable version and audit tables under the runtime-role rules.

Introduce additive migrations; do not rewrite existing migrations. Backfill exactly one unconditional organization binding for each existing membership's role, preserving current permissions and disabled-user refusal. Validate row mapping and effective-grant equivalence, then atomically switch readers/writers and retire `membership.role_id` as an authorization source. Never union legacy and new readers as a compatibility fallback; a half-migrated store refuses service. Preserve all request/approval/execution history and digests. The migration refuses readiness if it cannot preserve a recovery administrator; it does not silently add missing management rights to an arbitrary custom role. Seeded admin grants acquire new catalog permissions explicitly.

Older binaries are not supported against the migrated authorization schema; rollback requires the documented backup/restore procedure and the matching binary, not dual writes or silently dropping conditions. The migration/version guard must enforce that boundary. Existing PostgreSQL runtime/migration credential separation and no-MVP-RLS decision remain in force.

## Implementation impact

| Current surface | Required change |
| --- | --- |
| `internal/domain/identity/member.go`, `administration.go` | Separate membership/grant vocabulary; add typed scope, binding and condition values; replace flat delegation/admin predicates |
| `internal/app/authz`, transport authorization helpers | Resource-aware decision/evaluator ports, trusted context, explicit errors and decision evidence |
| `internal/infra/postgres/queries/rbac.sql`, administration persistence | Scoped binding resolution, version history, policy revision and transaction-locked management checks |
| Approval/lease queries and result/CSV services | Current resource/time-aware eligibility, shared lock order and bounded-output rechecks |
| Auth and administration protobufs, generated clients, SPA session/navigation | Multiple scoped grants, explicit Bindings service, condition validation and server-authorized actions |
| Bootstrap, migrations and `cmd/portcullis` | Grant-preserving backfill, atomic bootstrap binding, evaluator injection and readiness validation |

No runtime code, generated contract or migration is changed by this ADR. Historical tests establish only the previous model. M1 remains in progress until the replacement is implemented and qualified; M2/M3 sequencing remains unchanged.

## Acceptance and consequences

The model supports multiple roles and temporary connection access without duplicating role definitions. It increases list evaluation, migration and policy-management complexity; restricted conditions and conservative delegation intentionally keep the first boundary finite. Conditions do not replace connection execution policies or constitute SQL row-level security or data masking.

Scenario qualification must cover at least these distinct success cases: equivalent legacy backfill, two connection-specific roles for one user, an applicable timed grant, and union with another valid grant when one condition is false. Failure cases include foreign scope/principal, expired/missing/error conditions, widening through scoped permission flattening, concurrent removal versus approve/execute and removal of the last recovery administrator. Also verify overlapping unconditional grants, stale versions, unauthorized list totals/pickers, partial migration refusal, browser response fencing, CSV expiry/revocation, evaluator limits and unknown outcomes without replay. Follow the repository's DDD × TDD scenarios and real-engine/browser gates; both `make verify` and `make supply-chain` are required for milestone completion.

## Primary sources (verified 2026-10-16)

- [Google IAM allow policies](https://docs.cloud.google.com/iam/docs/allow-policies): role bindings associate principals, roles and optional conditions; multiple grants can coexist and policy writes use concurrency metadata.
- [Google IAM Conditions](https://docs.cloud.google.com/iam/docs/conditions-overview): CEL conditions constrain individual grants; another unconditional grant remains effective. Portcullis's vocabulary and limits are its own subset.
- [Google IAM overview](https://docs.cloud.google.com/iam/docs/overview): Google documents eventual consistency; Portcullis instead preserves its transactional admission/revocation contract.
- [CEL overview](https://cel.dev/overview/cel-overview) and [cel-go](https://github.com/google/cel-go): embedded parse/check/evaluate model; adapter limits and restricted functions still need implementation qualification.
- [OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html): deny by default, least privilege and checks on every request for the specific resource.
