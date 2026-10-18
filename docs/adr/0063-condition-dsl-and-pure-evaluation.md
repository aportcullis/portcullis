# ADR-0063: Condition DSL and pure evaluation boundary

- **Status:** Accepted
- **Date:** 2026-10-16
- **Complements:** [ADR-0062](0062-role-bindings-and-conditions.md); its scopes, grant union, language restrictions, delegation, revocation and migration contracts remain binding.

## Context

The owner clarified the authorization design: Role and Binding are structured data, Condition is a CEL expression, and condition evaluation is a pure function. ADR-0062 selects bounded CEL but does not explicitly separate preparation, evaluation and application orchestration or define how users enter conditions without learning the language.

CEL's documented processing model separates parsing and type checking from repeated evaluation against supplied variables. Portcullis uses that model behind its own ports; choosing CEL does not require implementing the application in a functional language or moving the authorization workflow into expressions.

## Options

- Put executable callbacks or a general policy DSL in Role and Binding. This couples persisted grants to application code and obscures the independently managed permission and resource scope.
- Let expressions fetch attributes or read the current clock during evaluation. This makes decisions depend on hidden state and makes approval, list and execution checks difficult to reproduce.
- Persist Role/Binding as data, prepare restricted CEL separately, and evaluate explicit immutable inputs without side effects (chosen).

## Decision

### Data and expression responsibilities

Role stores permission keys and metadata. Binding stores subject, role, organization/resource scope, optional Condition and version. Neither stores executable Go functions, closures, scripts or callbacks. Condition remains the binding-owned versioned expression value of ADR-0062; a missing condition is an explicit unconditional grant, while invalid or empty source is refused.

CEL expresses only the condition predicate. Resource-scope matching, role permission lookup, binding union and fixed governance guards remain typed authorization/application behavior. A true predicate does not itself grant a role, approve a request, authorize a result or execute SQL. No second custom expression language or scripting runtime is introduced.

### Preparation and pure evaluation

Use separate consumer-owned ports for condition preparation and evaluation, without exposing CEL SDK types to the domain. Preparation parses and type-checks against the versioned, restricted environment, validates the permitted AST and resource limits, and produces an immutable checked condition. Condition writes and controlled reload prepare programs outside the authorization evaluation path; the evaluator never parses unchecked source. Persist the expression and language/environment version as authoritative data; any stored checked representation is a derived artifact that must be validated against that source and environment before use. Unknown or incompatible versions cannot grant access.

The semantic evaluation contract is:

```text
EvaluateCondition(checkedCondition, attributeSnapshot, evaluationLimits)
    -> Match | NoMatch | EvaluationError(reasonCode)
```

All inputs are explicit and immutable for one invocation. The snapshot includes the trusted principal/resource attributes and `request.time` already resolved by the caller; limits include the deterministic cost budget. For the same checked condition, environment version, snapshot and deterministic limits, evaluation produces the same logical result or reason-coded error. `Match` requires Boolean true, `NoMatch` means Boolean false, and missing required attributes, incompatible programs or exhausted evaluation cost produce an error. The caller maps an error to no grant from that binding under ADR-0062; it never converts an error into an unconditional binding or bypasses another workflow guard.

The evaluator does not read a clock, environment variables, process-global user state, database, filesystem or network; generate randomness; mutate inputs; acquire application locks; or write audit events, logs or metrics. No callback or custom CEL function may reintroduce those effects. Attribute loading and authorization-result caching are not hidden inside evaluation. Immutable compiled-program caching belongs to preparation/orchestration, keyed by binding version, source identity and language/environment version; it does not cache time-sensitive decisions.

The outer service owns operational deadlines, cancellation, concurrency limits and observability. A cancelled or timed-out invocation is refused by that wrapper; it does not turn into a successful semantic result. This operational failure is distinct from reproducibility of a completed pure evaluation. Do not claim that arbitrary deadline interruptions are deterministic merely because the predicate is pure.

### Application orchestration

The application service resolves authenticated membership, applicable binding/role data and minimal resource ancestry through repositories. It chooses the authoritative time and authorization revision, constructs the snapshot, obtains the prepared condition and invokes the evaluator. It then combines matching grants and enforces the fixed organization/ownership/approval/execution rules before recording evidence or performing I/O.

Within mutation/approval/execution admission, observe time after the transaction locks and use one snapshot for that decision, as required by ADR-0062. Each later protected operation or bounded output chunk obtains a fresh snapshot; do not freeze the original approval time or login-time attributes into future execution authority. Collection evaluation uses the same page/count time and revision with each candidate's own verified resource attributes. Pure evaluation does not weaken transactional revalidation or resource resolution.

### Condition entry

M1 management UI offers forms for supported time and resource-type conditions, alongside the Binding's separate organization/connection scope selector. It generates CEL through a typed template builder with correct string escaping and explicit UTC timestamp conversion, shows a readable summary, and sends the result through the same server-side preparation and validation as API-authored CEL. Browser validation is advisory; API clients cannot bypass the allowed attribute/function set or limits.

Using those forms requires no CEL syntax knowledge. A general expression editor is not required for M1. Forms and any later editor operate on the same versioned Condition value, with no second policy representation that can diverge from persisted CEL. When a valid stored expression cannot be represented by the current form, preserve it and require an explicit replace/clear action before changing it; never silently simplify it or drop the condition. Invalid or unsupported input never falls back to unconditional access. Time and resource inputs are product choices; compiler diagnostics must be safe and understandable rather than exposing evaluator internals.

## Consequences and acceptance

This is a clarification of planned M1 architecture; no evaluator, condition UI or new runtime dependency is implemented by this document. Use Go's existing ports/adapters and constructor injection. Only predicate evaluation needs the pure-function contract; repositories, transaction coordination, auditing and target adapters retain their existing responsibilities.

Acceptance scenarios must demonstrate at least four distinct successful behaviors: a supported form produces a grant only within its scope and time; form-generated and equivalent API-authored CEL agree; repeated identical snapshots reproduce the same result; and a fresh snapshot stops a formerly matching grant at the exact expiry boundary. Failure cases include missing attributes, unknown environment/program versions, invalid/non-Boolean expressions at write time, deterministic cost exhaustion and attempted expression I/O. Also verify input immutability, cancelled/deadline-exceeded wrapper refusal, unchanged preservation of expressions the form cannot represent, explicit condition removal, escaping of hostile text, and composition with another valid binding without bypassing fixed governance guards. Follow the repository's scenario TDD and full verification gates when implementing this boundary.

## Primary sources (verified 2026-10-16)

- [CEL overview](https://cel.dev/overview/cel-overview): application-defined expression environments and separate parse/check/evaluate phases support preparation followed by repeated evaluation against supplied variables.
- [cel-go](https://github.com/google/cel-go): embedding and checked-program execution; Portcullis's no-I/O functions and deterministic input/budget contracts are additional application restrictions.
- [Google IAM Conditions](https://docs.cloud.google.com/iam/docs/conditions-overview): condition expressions constrain individual bindings and are evaluated against supplied attributes. The Role/Binding management and governance boundaries remain Portcullis decisions.
