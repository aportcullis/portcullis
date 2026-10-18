# ADR-0066: Bounded binding repositories and resource authorization snapshots

- **Status:** Accepted
- **Date:** 2026-10-18
- **Refines:** [ADR-0062](0062-role-bindings-and-conditions.md) and [ADR-0065](0065-restricted-cel-condition-evaluation.md)'s resource resolution and grant orchestration.
- **Preserves:** [ADR-0064](0064-binding-schema-preparation-and-activation.md)'s read-only preparation and atomic activation.

## Context

The domain and pure CEL evaluator are available, but prepared bindings must not become a second source alongside membership roles. The next slice needs a repository and resource permission service that can be qualified against real PostgreSQL and CEL without prematurely changing production authorization. Both PRD translations retain their final activation and governance requirements.

Google IAM documents additive bindings with optional conditions, OWASP recommends checking access to each resource with denial by default, and PostgreSQL documents stable snapshots under repeatable read. These establish the standard approach; the resource vocabulary, inactive qualification boundary and limits below are Portcullis decisions.

## Options

- Combine legacy membership permissions with scoped grants: preserves a broad bypass and violates atomic activation.
- Switch selected endpoints first: leaves collection, approval, administration and output readers with different authorities.
- Qualify a separate read-only snapshot repository and resource service, then switch all consumers at activation: retains current authority and makes the new boundary independently testable.

## Decision

### Consumer ports and coherent reads

The authorization application owns a narrow resource repository port and condition preparation/evaluation port. Its target contains organization, resource type and identity only. Callers cannot supply a connection ancestor. The PostgreSQL adapter resolves identity-only metadata without decrypting credentials, request payloads or result values.

Read account status, independent membership, organization authorization revision, database wall-clock time, resource ancestry and live candidate grants in one read-only repeatable-read transaction. An absent revision refuses binding authorization as inactive; it never substitutes zero or consults the legacy resolver. Missing or foreign membership/resources deny. Driver errors become a safe unavailable error, and cancellation propagates without driver/source details. Returned snapshots are values owned by that decision, without a shared permission or expression-result cache.

Resolve connection and policy targets by connection identity; request ancestry comes from the stored request. Execution identity is its request ID. A result inherits connection identity through its execution and request, with organization predicates on every join. Refuse missing or ambiguous ancestry rather than selecting a convenient match. User targets require independent membership, roles must be live, and binding/audit targets resolve their stored IDs. Organization and settings targets use the organization root ID. These are metadata references, not authorization to read protected content. Add migration `0024_binding_result_ancestry_index.sql` with a non-unique execution index on result/organization identity for the new reverse lookup; retaining non-uniqueness allows the resolver to detect ambiguous historical metadata. An execution-plan scenario disables sequential scans and checks both identities participate in an index condition, qualifying lookup index eligibility independently of capacity.

Only the current non-revoked binding version and live role/permission rows contribute. Preserve binding condition presence, source and language/environment versions. Incomplete persisted condition shape is a storage error; unsupported or invalid predicate source contributes no grant. Scope, organization, principal, requested permission and role/binding versions are checked before considering any granting prefix.

### Permission checkpoints

The resource service consumes the repository and actual bounded CEL wrapper through constructor injection. It validates permission keys against the loaded catalog and an explicit permission/resource registry. Unknown combinations deny even when the key exists in the catalog. Applicable live bindings contribute additively; false, invalid, missing-attribute or failed conditions do not invalidate an independent valid grant. Repository, snapshot or total-budget failure closes the whole decision with no legacy fallback.

Connection verbs apply to connection candidates, except creation which targets the organization. Testing a new unsaved connection configuration also targets the organization; a connection-scoped grant cannot authorize that operation. Policy verbs target policy metadata. Request listing/approval/rejection target requests; creation targets a connection or draft request, execution targets a request or execution, and reading may target a request, execution or result. User/role collection and creation verbs target the organization; individual verbs target the corresponding user or role. Audit verbs target event candidates. Unimplemented settings, saved-query and binding permission operations have no implicit mapping.

Authorization-policy writes additionally require an unconditional organization binding for the management permission. This checkpoint does not perform delegation: administration must still compare every old/new affected permission and enforce self-change and recovery-administrator guards in its future mutation transaction.

A successful decision exposes resolved resource identity, revision, database evaluation time and matched binding/role versions. It is a permission checkpoint, **not** a workflow admission or reusable execution lease. Archive, result ownership/expiry, self-approval, quorum, payload integrity and single-use execution guards remain mandatory. Collection verbs are per-candidate checks; this service does not establish authorized pagination, counts or target pickers.

### Finite budgets

Use isolated typed `PORTCULLIS_RESOURCE_AUTHORIZATION_MAX_BINDINGS` and `_TIMEOUT` settings. Defaults are 64 relevant grant candidates and 250 ms for the entire snapshot/preparation/evaluation decision; absolute ceilings are 1,024 candidates and five seconds. Reject zero, negative, malformed and excessive settings. The database query fetches at most the configured count plus one; overflow is an explicit error even if a returned prefix contains an unconditional grant. SQL joins may inspect additional rows, so the total context deadline also bounds database work; the row bound is not a query-plan scan guarantee. Check elapsed operational deadlines as well as cancellation signals before granting, including unconditional grants. Existing CEL source, evaluation and concurrency limits still apply to each condition. Synchronous pure work remains bounded rather than forcibly preempted.

### Activation boundary

Keep runtime table privileges, bootstrap, sessions, transport, collections, governance admission and output on the existing membership authority for now. The additive result-ancestry index introduces no Binding write API, revision seeding, production backfill, mixed grant source or composition-root switch. Qualification fixtures may create owner-only bindings and revisions in disposable databases, including copies made from current membership roles solely to compare grant sets.

The later atomic activation must integrate a transaction-bound reader with the shared organization lock and fresh post-lock database time. A completed read-only checkpoint must not be reused across a concurrent administration mutation to authorize writes, execution admission or another output chunk. Activation still requires every reader/writer, delegation/recovery checks, grant-preserving migration and resource-aware API/UI contracts from ADR-0062/0064 together.

## Consequences and acceptance

This slice makes resource grant resolution independently executable while preserving current production permissions. It does not establish binding activation or M1 completion. Scenario coverage includes unconditional and conditional organization/connection grants, additive roles, request/execution/result ancestry, disabled accounts, invalid/expired source, foreign identities, soft-deleted permissions/roles, live head revocation, candidate overflow, cancellation and a committed policy change between snapshot reads. A disposable current-membership copy compares the full catalog for default, custom and empty roles. Existing whole-application tests continue checking the active legacy behavior.

Require observed behavioral red tests, defect-injection checks for initially green guards, database-backed race checks, `make verify`, `make supply-chain` and staged-diff review before committing. Read-only snapshot consistency does not establish linearized workflow revocation; that remains an activation acceptance requirement.

## Primary sources (verified 2026-10-18)

- [Google IAM allow policies](https://docs.cloud.google.com/iam/docs/allow-policies): bindings associate principals, roles and optional conditions.
- [OWASP authorization guidance](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html): deny by default and validate resource access on every request.
- [PostgreSQL transaction isolation](https://www.postgresql.org/docs/current/transaction-iso.html): repeatable-read statements share a transaction snapshot.
- [PostgreSQL multicolumn indexes](https://www.postgresql.org/docs/current/indexes-multicolumn.html): equality constraints on leading B-tree columns bound an identity lookup.
