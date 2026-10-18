# ADR-0064: Prepare binding storage before atomic authorization activation

- **Status:** Accepted
- **Date:** 2026-10-18
- **Complements:** [ADR-0062](0062-role-bindings-and-conditions.md) and [ADR-0063](0063-condition-dsl-and-pure-evaluation.md); their authorization, evaluator and grant-preserving migration contracts remain binding.

## Context

The existing binary reads and writes membership role assignments throughout bootstrap, administration, review and execution. Copying those assignments into bindings before replacing all writers would create stale grants. Reading a union of both stores or maintaining compatibility dual writes is prohibited by ADR-0062.

The migration work therefore needs a safe first commit that introduces storage without claiming or activating the replacement authorization model. Existing migration-history checks already refuse databases containing a migration unknown to an older binary; preparation does not permit downgrade in place.

## Options

- Backfill immediately and keep updating membership roles until later. Bindings diverge from current authority and cannot be used safely at cutover.
- Ship the entire schema, evaluator, application and transport replacement in one concern. This prevents independently qualifying the persistence boundary.
- Prepare empty storage with runtime reads only, then backfill and activate when every reader/writer is ready (chosen).

## Decision

### Preparation

Add migration `0023_binding_schema_preparation.sql` without changing earlier migration files. It creates empty binding heads, immutable binding versions and organization authorization revision storage. Do not add binding catalog permissions, copy memberships, seed revisions, change bootstrap or switch any authorization reader/writer in preparation. Existing membership assignments remain the sole grant source during this stage; this is an inactive prepared schema, not a partially activated store.

A binding head identifies an organization member, its creation time and current version. Each version records the role, organization or exact-connection scope, optional condition source/metadata/language/environment versions, creation time and optional revocation time. Condition is a binding-owned value embedded in that immutable version, with no shared condition registry. Changing or explicitly clearing a condition and revoking a binding append snapshots and advance the head; previous expressions remain available as evidence.

Composite foreign keys pin subject membership, role, connection and the current version to the binding's organization. Version rows also retain the subject ID in their head reference, preventing head reassignment from rewriting whose grant the history describes. A deferred head-to-version reference permits creation of both records in one transaction but refuses an orphan head at commit. Scope checks distinguish organization from connection grants. Condition columns are either explicitly absent together or contain all required nonempty fields; SQL shape validation does not parse CEL or assert that source is executable. Version/language support and checked-source preparation remain the application/evaluator boundary of ADR-0063.

Use explicit timestamps supplied by future transaction orchestration, positive versions/revisions, `RESTRICT` foreign keys and non-partial indexes covering all references. Binding versions are sensitive immutable evidence. All three prepared tables allow runtime SELECT only; revoke INSERT, UPDATE, DELETE and administrative table privileges in the migration, and check those restrictions at boot, including inherited privilege drift. Keep the operational role-rotation statement list in ADR-0009 and its executable scenario synchronized with these new table classifications; rotating credentials must not restore writes. No Binding write API is exposed during preparation. The existing migration runner and privilege-check ports own this infrastructure step; new domain operations are introduced with their consumer-owned ports and scenario tests when binding use cases are implemented.

### Activation remains a separate required concern

Implement and qualify the domain/evaluator, scoped repositories, policy management, resource-aware admission and collection/output checks before activating bindings. Introduce binding catalog permissions and seeded administrator grants in the activation migration. Under the migration/admission coordination boundary, validate recovery administrators, backfill one unconditional organization binding per current membership role, validate mapping and effective grants, initialize authorization revisions, and retire the membership role column as an authority in the same cutover. Grant runtime only the newly required head/revision writes and version INSERT; immutable version UPDATE/DELETE remain forbidden. A failed activation rolls back and refuses service rather than enabling a mixed source.

Every path that creates or mutates membership roles, resolves permissions or checks requester/approver eligibility must switch together, including bootstrap, user administration, shared-role edits, sessions, transport, result/CSV output and browser capability data. Prepared bindings never participate as an additional compatibility grant source. No background synchronization, dual writes or fallback union is allowed. Activation must account for a preparation database whose membership assignments changed after migration 0023; it backfills authoritative assignments at activation time.

Backup/restore with the matching binary remains the rollback mechanism. An older binary must not be made compatible by deleting a recorded migration version, and preparation does not weaken migration checksums or runtime credential separation.

## Consequences and acceptance

This split adds no user-visible authorization capability but makes persistence independently reviewable and keeps current grants unchanged. It does not deliver backfill, activation, CEL validation/evaluation, binding APIs or condition forms, and does not establish M1 completion. Both PRD translations retain the same final acceptance requirements; this decision only defines implementation sequencing.

Preparation scenarios cover populated-schema upgrades with unchanged legacy grants and no premature backfill, unconditional organization bindings, multiple connection bindings for one member, preserved condition source after later revisions and soft revocation history. Refusal scenarios cover foreign membership/role/connection references, invalid scope/condition shapes, orphan current versions and runtime mutation or privilege drift. Apply the full verification and supply-chain gates before committing; activation requires its own grant-equivalence, recovery, concurrency, expiry and browser scenarios from ADR-0062/0063.

## Primary sources (verified 2026-10-18)

- [PostgreSQL constraints](https://www.postgresql.org/docs/current/ddl-constraints.html): composite foreign keys enforce related identities and CHECK expressions require explicit null handling; cross-row restrictions belong in relational constraints.
- [PostgreSQL CREATE TABLE](https://www.postgresql.org/docs/current/sql-createtable.html): deferred foreign-key constraints permit commit-time validation of circular references.
- [Google IAM allow policies](https://docs.cloud.google.com/iam/docs/allow-policies): binding scope and conditions determine grants; Portcullis's migration sequencing and transactional activation are application-specific requirements.
