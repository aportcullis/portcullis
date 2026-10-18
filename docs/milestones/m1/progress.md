# M1 progress

**Status: In progress · scope reviewed 2026-10-16 · verification reviewed 2026-10-18.**
Scope and acceptance live in [scope.md](scope.md); dated test evidence lives in [validation.md](validation.md).

## Established implementation

- PostgreSQL connection/policy → immutable request → distinct approval → single-use execution → bounded encrypted results/CSV → audit is implemented.
- Historical `make verify` evidence includes real-browser native CSV saving/readback in the recorded Docker-hosted Chromium environment.
- User/role domain, application services and PostgreSQL administration persistence are present.
- Password setup token issuance and persistence are implemented.
- Result controls have aligned search, copy/export and paging controls; Text values wrap without dropping content at tested viewport widths in both themes.
- Result tables use subtle dashed column separators and show a sorting arrow only on the active column. Table headers cycle sorting without a separate ordering selector.

Per [ADR-0053](../../adr/0053-user-and-role-administration.md), user and role administration is M1 scope.
Per [administration use cases](../../../internal/app/administration/users.go) and [administration persistence](../../../internal/infra/postgres/administration_store.go), backend components exist without establishing a complete administration UI.

## Accepted authorization replacement

[ADR-0062](../../adr/0062-role-bindings-and-conditions.md) separates membership, roles, scoped bindings and conditions within M1. The current code still resolves one membership role into flat organization permissions; no binding schema, condition evaluator or resource-aware replacement is delivered by the documentation decision. The existing administration services and historical governance evidence therefore do not qualify the new boundary. Scope and both PRD translations now require grant-preserving migration, resource-aware list/action checks, conservative policy management and concurrent revocation/expiry scenarios.

[ADR-0063](../../adr/0063-condition-dsl-and-pure-evaluation.md) clarifies Role/Binding as data, Condition as a restricted CEL DSL, and evaluation as a pure function over checked programs and trusted immutable inputs. Preparation, attribute/time resolution, auditing and condition forms remain separate responsibilities; this clarification adds no runtime implementation or qualification evidence.

[ADR-0064](../../adr/0064-binding-schema-preparation-and-activation.md) starts implementation with [migration 0023](../../../migrations/0023_binding_schema_preparation.sql): organization-pinned binding heads, binding-owned condition/version history and authorization revision storage. These tables stay empty and read-only to runtime during preparation; current membership grants remain authoritative. Backfill and catalog grants wait for the atomic reader/writer cutover so ongoing legacy administration cannot leave stale copied bindings. That preparation did not deliver a CEL evaluator, binding use cases, scoped authorization or activation.

[ADR-0065](../../adr/0065-restricted-cel-condition-evaluation.md) adds domain Membership/RoleBinding/Condition and typed resource scopes, [consumer-owned condition ports and wrapper](../../../internal/app/condition/service.go), and the [restricted CEL adapter](../../../internal/infra/celcondition/adapter.go). Source/type/attribute/budget validation, explicit UTC time, immutable checked artifacts and outer cancellation/deadline/concurrency have scenario and race coverage. This qualifies the predicate slice only; production authorization still uses membership roles, and binding use cases, forms, grant resolution, composition-root cutover and backfill remain pending.

[ADR-0066](../../adr/0066-resource-binding-authorization-snapshots.md) adds the read-only [Binding repository](../../../internal/infra/postgres/binding_store.go) and [resource permission service](../../../internal/app/authz/resource_service.go), connected to real CEL through consumer-owned ports. Qualification covers server-resolved ancestry, bounded additive grants, current role/head changes, coherent revision snapshots and disposable grant-equivalence copies for default/custom/empty roles. Production membership authority is preserved. Binding administration, authorized collections/output, transaction-bound workflow admission and atomic backfill/cutover remain pending; a read-only permission checkpoint is not a workflow lease.

[ADR-0067](../../adr/0067-binding-management-transactions.md) adds the [Binding management use cases](../../../internal/app/administration/bindings.go) and [transactional repository](../../../internal/infra/postgres/binding_management_store.go). Qualification covers create/update/revoke, explicit condition edits, old/new unrestricted delegation, full-catalog recovery protection across roles, immutable history, atomic session/revision/audit changes and real lock contention. Disposable databases demonstrate independence from the legacy role column and unchanged legacy grants, with new grant changes immediately reflected by the qualified resource reader. Runtime binding writes, production catalog/backfill, account/shared-role conversion, workflow/collection/output admission and API/UI cutover remain pending.

[ADR-0068](../../adr/0068-binding-authority-account-and-role-management.md) adds [independent account mutations](../../../internal/app/administration/policy_accounts.go) and [shared-role mutations](../../../internal/app/administration/policy_roles.go) through Binding authority. Qualification covers grantless membership/setup creation, complete live-binding delegation, disable/enable/setup reissue, old/new role permissions, bounded binding-subject session fanout, recovery protection against concurrent Role/Binding changes, rollback and fresh admission after lock waits. Production remains on existing grants; bootstrap/session and password-setup completion conversion, authorized list/get contracts, workflow/collection/output admission, transport/UI, backfill and atomic activation remain pending.

[ADR-0069](../../adr/0069-prepared-administration-rpc-contracts.md) adds generated user/role/binding RPC contracts, actual management handlers, shared TypeScript clients and organization-pinned read snapshots with bounded pages and revision checks. Raw TLS/Connect scenarios qualify sessions/CSRF, conditions, complete delegation, recovery races, session revocation, source preservation and rollback. Production routes remain unavailable after normal authentication until atomic activation; API qualification does not deliver management screens, migrated bootstrap/session admission, governance/output cutover or production backfill.

## Verification status

The recorded PostgreSQL governance tests passed in the documented Docker-browser environment.
The [2026-10-18 verification](validation.md#verification--2026-10-18) passed both required gates after the dependency security update, including all 38 real-binary browser scenarios. The resource-aware Role/Binding/Condition replacement remains inactive and is not qualified by those existing scenarios; ADR-0065 records separate predicate-slice evidence.
This evidence covers the tested journey and does not establish completion of the entire M1 scope or capacity qualification.
Per [scope.md](scope.md), release readiness requires all milestone completion criteria and both verification gates.
