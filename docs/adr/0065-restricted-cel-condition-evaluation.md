# ADR-0065: Versioned condition models and restricted pure CEL evaluation

- **Status:** Accepted
- **Date:** 2026-10-18
- **Refines:** [ADR-0062](0062-role-bindings-and-conditions.md)'s adapter module selection and finite language limits; [ADR-0063](0063-condition-dsl-and-pure-evaluation.md)'s preparation/evaluation boundary.
- **Preserves:** [ADR-0064](0064-binding-schema-preparation-and-activation.md)'s inactive schema and atomic authorization cutover.

## Context

The binding schema is prepared, while current authorization still uses membership roles. The next implementation slice needs domain data and independently qualified predicate evaluation without activating bindings or presenting a true condition as an authorization decision.

Upstream moved the canonical CEL Go module to `cel.dev/cel-go` in v0.32.0. The `github.com/google/cel-go` repository now directs new integrations to that module. Pin `cel.dev/cel-go v0.32.0` directly instead of adding a new dependency on the compatibility module named in ADR-0062. Existing tools may continue requiring the older module transitively; do not replace their imports or change unrelated dependencies.

## Decision

### Domain data and ports

Keep the existing permission-bundle Role and introduce independent Membership, RoleBinding, ResourceScope, ResourceReference and Condition values in the identity domain. Binding versions carry IDs, scope, an explicit condition-presence flag and copied condition data; no executable function or SDK type is persisted in them. Incomplete condition data cannot masquerade as absence. Scope applicability checks principal, organization, revocation and supported server-resolved ancestry; it deliberately does not evaluate permissions or conditions or bypass fixed workflow guards.

The initial contract identifiers are `cel-v1` and `portcullis-v1`. Consumer-owned preparation/evaluation ports live in the condition application package. Checked artifacts expose a value copy of their source, while their AST and required-attribute set remain private to the CEL adapter. Evaluation rejects foreign artifacts without invoking their callbacks. Stored source and contract versions remain authoritative; this slice adds no serialized checked-program store or result cache.

### Restricted language and pure invocation

Expose only ADR-0062's seven scalar attributes. Clear all macros, validate parsed syntax before type checking, then require a Boolean result and restrict checked overloads: equality/inequality on matching strings, Booleans or timestamps; timestamp ordering; string membership in bounded literal string lists; Boolean composition; and RFC3339 timestamp literals. Refuse receiver methods, indexing, maps, comprehensions, presence macros, dynamic evaluation, arithmetic, ternaries, regex and undeclared attributes/functions. Check every referenced attribute before evaluation, including attributes hidden behind Boolean short circuiting. Empty, missing, inapplicable or excessive referenced attributes never produce Match.

Resolve timestamp literals during preparation with explicit UTC parsing and range checks, then lower them into private typed timestamp constants. Require explicit RFC3339 zones, at most nanosecond precision and UTC instants in years 1–9999. This avoids string-to-timestamp conversion or host-local-zone resolution during evaluation. Normalize supplied request time to UTC before inspecting it; never obtain time inside the adapter.

For each invocation, plan the already checked AST with its explicit cost budget and construct invocation-local activation/cost state. This planning performs no source parsing, type checking, external lookup, clock read or application side effect; no shared mutable program/result cache is hidden inside evaluation. The checked artifact is immutable after preparation and can be evaluated concurrently or by another adapter with the same fixed environment. Match, NoMatch and reason-coded EvaluationError remain predicate evidence, not permission grants.

### Finite budgets and outer operation

| Budget | Default | Absolute configuration ceiling |
| --- | --- | --- |
| CEL source bytes | 4,096 | 65,536 |
| Parsed AST nodes | 256 | 4,096 |
| Parsed AST depth | 32 | 64 |
| Entries per literal ID list | 32 | 256 |
| CEL actual cost per invocation | 1,000 | 100,000 |
| Bytes per referenced string attribute | 256 | 4,096 |
| Operation timeout, including admission wait | 20 ms | 1 s |
| Concurrent preparation/evaluation calls per service | 16 | 256 |

All limits must be positive and within the ceilings. The parser additionally uses the absolute source/node/recursion ceilings and eight recovery attempts; parser-level malformed or excessive syntax is refused without leaking diagnostic internals. Configured AST/list/source exhaustion and runtime cost/attribute exhaustion return stable safe refusal codes. Language/environment or library changes that alter accepted expressions, predicate semantics or deterministic cost behavior require explicit version review and scenario requalification.

The isolated `PORTCULLIS_CONDITION` loader binds typed nested preparation/evaluation settings, timeout and concurrency; constructors also accept validated explicit configuration. Only the outer application service reads contexts/clocks and owns admission, cancellation, deadlines and the semaphore. It checks cancellation before admission and after bounded synchronous work, keeps the permit until that work finishes, and refuses a completed Match after cancellation or expiry. It does not spawn detached evaluation goroutines or claim to interrupt pure work at an arbitrary instruction. Source, AST, attribute and cost bounds limit that work independently of operational cancellation. Binding-scan limits remain part of the future resource-aware authorization service.

## Qualification and rollout

Scenario tests cover organization/exact-connection applicability, independent condition values, explicit condition absence, typed principal/resource/time predicates, repeated identical snapshots, exact expiry boundaries, preserved source identity, missing attributes under short circuiting, unsupported contract versions and foreign programs, unknown and effectful syntax, invalid timestamps, quoted hostile identities, recovery after budget refusal and concurrent immutable evaluation with distinct budgets. Separate service scenarios cover injected ports, cancellation before/after evaluation, service/parent deadlines, saturated admission and typed configuration. Mutation checks observed failures when syntax/type restrictions, cost limits, referenced-attribute checks, post-evaluation cancellation or environment loading were removed; the restored implementation passes.

Uncached race-detector runs qualify shared checked artifacts and wrapper admission. On the local Apple M3 Pro with Go 1.27.2, two 200 ms benchmark samples measured a representative predicate at 2.30–2.31 µs, preparation at 31.3–32.4 µs, and six full 32-entry ID-list predicates at 16.8–17.1 µs. These are local microbenchmarks of the selected defaults, not production capacity or deadline guarantees. Full verification and supply-chain gates remain required for the commit.

This slice exposes no new RPC, form, binding writer or authorization grant. It does not backfill or activate migration 0023, switch current membership authorization, implement additive grant resolution or qualify the full replacement workflow. The real adapter is injected into the application wrapper in integration scenarios; composition-root wiring waits for a resource-aware consumer in the atomic cutover work, rather than constructing an unused service or partially activating authority. Both PRD translations retain final M1 acceptance requirements and now link this concrete contract.

## Primary sources (verified 2026-10-18)

- [CEL Go v0.32.0 release](https://github.com/cel-expr/cel-go/releases/tag/v0.32.0): canonical module move, release identity and concurrency/cost fixes.
- [CEL Go compatibility repository](https://github.com/google/cel-go): canonical-module guidance and preparation/evaluation model.
- [Pinned CEL environment/program options](https://github.com/cel-expr/cel-go/blob/v0.32.0/cel/options.go): macro, parser and cost-limit controls; the adapter applies a stricter grammar and typed contract.
- [CEL overview](https://cel.dev/overview/cel-overview): application-defined environments and preparation followed by evaluation against supplied variables.
