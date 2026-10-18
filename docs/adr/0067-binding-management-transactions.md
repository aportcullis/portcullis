# ADR-0067: Transactional binding management and recovery protection

- **Status:** Accepted
- **Date:** 2026-10-18
- **Refines:** [ADR-0062](0062-role-bindings-and-conditions.md)'s delegation and recovery guards, and [ADR-0066](0066-resource-binding-authorization-snapshots.md)'s read-only permission checkpoints.
- **Preserves:** [ADR-0064](0064-binding-schema-preparation-and-activation.md)'s inactive production authority and atomic cutover.

## Context

The prepared repository can resolve resource grants and CEL conditions, while production still authorizes membership roles. The next independently executable slice is Binding creation, update and revocation. It must qualify policy writes without exposing prepared grants as a second production authority or weakening the read-only runtime privileges. Both PRD translations already require conservative delegation, last-administrator protection, atomic evidence and session revocation; these product requirements remain unchanged.

Google IAM uses policy versions and etags to prevent lost updates, OWASP requires server-side authorization and least privilege, and PostgreSQL transaction and advisory locks coordinate competing writers. Portcullis keeps its expected binding version and stronger transactional revocation contract rather than adopting Google's eventual consistency or whole-policy replacement.

## Options

- Authorize against a previously completed resource checkpoint and then write: leaves a gap for concurrent revocation and cannot qualify recovery protection.
- Extend legacy single-role administration with temporary binding writes or a permission union: creates two authorities before activation.
- Introduce a transaction-scoped consumer port and Binding management service, with owner-only database qualification until atomic activation: keeps authorization and mutations together while preserving production grants.

## Decision

### Domain commands and consumer ports

Add BindingDefinition and BindingUpdate values to the identity domain. Definitions contain a subject, role, scope and explicit condition presence; persistence assigns identity and version. Update has no subject reassignment field. Omitted role/scope fields preserve existing values. Condition edits distinguish preserve, replace and clear; preserve/clear with supplied condition data and unsupported modes are refused. Unknown existing language/environment contracts cannot be silently cleared or rewritten by an older client.

The administration application owns a narrow repository callback port and a transaction-scoped interface for catalog, membership, role, grant, scope, clock, recovery and evidence operations. It separately consumes condition preparation and best-effort refusal-audit ports. Concrete PostgreSQL and CEL adapters are injected through constructors in qualification scenarios; the domain imports neither SDK.

Creation grants to another existing member, including a disabled member whose grants cannot serve until the account is active. Updates and revocation require an exact positive expected binding version. A revoked binding cannot be revived or revoked again; stale versions refuse rather than overwrite a later decision. Every accepted update appends a version even when supplied values equal the current values. Revocation appends a terminal version retaining its role, scope and condition evidence. A known-contract source need not parse successfully to be revoked safely; unsupported contracts are refused. Create/update prepare every resulting present condition through the same restricted CEL boundary, including an omitted condition being preserved; false conditions are valid data, and parser/budget failures never become unconditional grants.

### Conservative delegation and recovery

Resolve current catalog and account/membership state inside the locked transaction. Require all current enforcement keys and the planned `bindings.{list,get,create,update,delete}` keys before management can proceed. An absent authorization revision or incomplete catalog refuses this boundary as inactive. Neither the legacy membership role nor advisory session permissions is a grant source. The PostgreSQL adapter requires canonical UUID spellings at the policy boundary so SQL-equivalent aliases cannot split organization/session lock keys or evade self-binding comparisons.

A management gate comes only from live unconditional organization bindings of an active member. Every permission in both the old and new affected roles must be held at that same unrestricted scope, potentially through multiple roles. This applies even when removing access. A true conditional grant or connection grant cannot supply either management authority or an affected permission. Self-binding creation, modification and revocation are refused. Arbitrary scope/condition implication and delegated scoped administration remain deferred.

After tentatively appending the new version, require at least one active member holding every catalog `users.*`, `roles.*` and `bindings.*` permission through live unconditional organization bindings. Aggregate by member across multiple roles; role names, the legacy two-permission administrator predicate, inactive accounts, conditional grants and connection-only grants do not qualify. A failure rolls back the tentative version and head. Account and shared-role mutation paths must adopt the same complete affected-permission and recovery rules before activation.

### Transaction and clock ownership

The PostgreSQL adapter starts an explicit read-committed write transaction and acquires the existing administration advisory lock class 5 for the organization, then locks its authorization revision. Reads after a lock wait see newly committed authority. A repeatable-read snapshot established before that wait is inappropriate for policy mutation admission; ADR-0066's completed read-only checkpoint remains separate.

Lock the binding head for update and membership, role and connection references with key-share locks before mutation. Load roles through a dedicated query that has no `membership.role_id` dependency. Before observing mutation time, acquire the affected subject's existing session-rotation advisory lock class 2 and lock its unrevoked session rows in identity order. This serializes with current login rotation and prevents a session-row wait from predating the mutation timestamp. Lock order is organization, revision/head and references, subject session coordination, session rows, then `ObserveWallClock`. Future admission/session consumers must preserve this order at cutover; no lock is held during target SQL.

Use that one post-lock database instant for binding creation/version/revocation, session revocation and success audit. Insert immutable versions and advance the expected organization/subject-pinned head; never update or delete version evidence. Revoke the affected user's sessions, increment the locked organization revision once and append success audit in the same metadata transaction. A storage, CAS, recovery, audit, cancellation or deadline failure rolls back the transaction. Do not automatically replay mutations after an uncertain commit response; clients reload state and present the current version.

Success evidence records actor, before/after binding and role identities/versions, scope, condition presence/contract, revocation state, revision and revoked-session count. Raw condition text remains in protected version history and is omitted from ordinary audit metadata and logs. Privilege refusals reuse administration's existing bounded best-effort audit policy after rollback. Safe public errors omit driver details and source text.

### Operational bounds and inactive composition

Use isolated typed `PORTCULLIS_BINDING_MANAGEMENT_TIMEOUT` with a two-second default and five-second absolute ceiling. It bounds locked management work and lock waits; the existing detached refusal audit retains its separate five-second bound. Check elapsed deadlines as well as cancellation signals before committing. CEL preparation retains ADR-0065's source/AST/deadline/concurrency bounds; synchronous work remains bounded rather than forcibly preempted. Database aggregation stays under the transaction context deadline and does not materialize all organization members in application memory.

Add no activation migration, production catalog grants, revision seed, backfill, runtime binding writes or transport/UI endpoints. Existing runtime privileges still refuse these management transactions. Disposable qualification databases may seed the planned catalog and owner-created policy grants, then exercise the actual service and adapters as owner; this is not a production compatibility mode. Production composition, membership assignments, user/role administration, sessions and governance readers retain their current authority.

Atomic activation still requires every policy writer and reader, bootstrap/recovery validation, grant-preserving backfill, authorized collections/counts/output, transaction-bound workflow admission, session/API/UI contracts and least-privilege runtime grants together. These management commands cannot be exposed selectively before that boundary. They do not establish M1 completion.

## Consequences and acceptance

Scenarios cover unconditional/conditional organization and connection creation, empty roles, immutable condition preservation/replacement/clearing, terminal revocation, safe removal of invalid known-contract source, exact versions, self/foreign IDs, old/new permission dominance, conditional/scoped delegation refusal, disabled actors, complete-catalog recovery and split roles. PostgreSQL scenarios additionally cover rollback at audit/revision/session writes, independent membership without the legacy role column, unchanged legacy grant sets, management-to-resource-reader freshness, competing optimistic edits and competing removal/conditioning of recovery grants.

Coordinate real lock waits to qualify reauthorization, cancellation and post-organization/session-lock timestamps. Inject defects into initially green guards and verify failure before restoration. Require targeted uncached database/race scenarios, `make verify`, separate `make supply-chain` and a staged-diff review before commit. Existing browser scenarios continue qualifying the active legacy system, not a partially activated Binding API.

## Primary sources (verified 2026-10-18)

- [Google IAM allow policies](https://docs.cloud.google.com/iam/docs/allow-policies): binding/condition separation and version/etag concurrency controls.
- [OWASP authorization guidance](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html): least privilege and server-side checks for each protected operation.
- [PostgreSQL explicit locking](https://www.postgresql.org/docs/current/explicit-locking.html): transaction-scoped advisory and row locks, including key-share semantics.
- [PostgreSQL transaction isolation](https://www.postgresql.org/docs/current/transaction-iso.html): read-committed statements observe newly committed state after waiting.
- [PostgreSQL UUID input/output](https://www.postgresql.org/docs/current/datatype-uuid.html): input accepts alternative spellings while output uses the standard form.
