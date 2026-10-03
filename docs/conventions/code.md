# Code conventions (Go)

## Layered DDD
`transport → app → domain ← infra`; `platform` is cross-cutting.
Dependencies point **inward**.
`internal/domain/` imports nothing outward (no app/infra/transport, no third-party infra SDKs) — only stdlib and other domain code.
Domain declares **ports** (interfaces); `infra/` implements them; `app/` (use cases) and `transport/` (Connect RPC + HTTP) orchestrate.
See [ARCHITECTURE.md](../ARCHITECTURE.md).

## Dependency inversion & wiring (ports and adapters)
We aim for **clean architecture in the ports & adapters (hexagonal) sense**: the domain and use cases are the inside, every I/O technology is an adapter on the outside, and the boundary is always a port owned by the inside.
Go isn't a pure OOP language, but follow every SOLID principle — **loose coupling, high cohesion**.
Each type and function has one reason to change (SRP); extend behavior through registries, descriptors and fixtures instead of widening special cases (OCP); every implementation honors its port's full contract (LSP); ports stay narrow (ISP); and layers depend on abstractions (DIP).
Every new change and every refactor preserves these principles.
Depend on **abstractions, not concretions** (DIP): a layer takes the behavior it needs as a **small, consumer-defined interface** (ISP — define it in the package that *uses* it, list only the methods that package calls), and the concrete implementation is **injected** via the constructor.
"Accept interfaces, return structs."

- **Consumer-owned ports**
  - `app/auth` declares `PasswordHasher` and `CSRFProtector`; it never imports `infra/crypto`.
  - Crypto adapters satisfy those interfaces structurally without importing the app layer.
  - Domain persistence ports and PostgreSQL adapters follow the same rule.
- **Manual constructor injection**
  - Wire dependencies in `cmd/portcullis`; the composition root documents the wiring.
  - Consider compile-time tooling only when wiring outgrows one screen; avoid runtime or reflection DI containers.
  - Inject simple test fakes to keep unit tests independent of Argon2 and databases.

## File organization — two axes, chosen by package shape
Plan the split BEFORE creating files; prefer separation.

- **Multi-concept packages**
  - Keep each concept's types, constants, constructor, and methods together, such as `user.go`, `session.go`, or `email.go`.
  - Shared vocabulary uses `errors.go`, cross-concept IDs use `types.go`, and interfaces use `port.go` or `ports.go`.
  - Keep concept-specific constants with their concept; `MaxEmailLength` belongs beside `EmailTooLong` in `email.go`.
- **Single-concept packages**
  - Split by kind: interfaces in `ports.go`, types in `types.go`, constants in `const.go`, and sentinels in `errors.go`.
  - Keep the primary type's constructor and methods in the main file.

## Tests — TDD, black-box
- Default to external `package X_test` (black-box) — test through the exported surface.
- **White-box exceptions**
  - Use `package X` only for invariants unreachable through the exported surface, such as an internal memory bound or an unwired handler.
  - Name the file `*_internal_test.go` and explain why black-box access cannot reach the invariant.
  - Keep every other test black-box.
- Practice **TDD**; table-driven; add `t.Parallel()` where safe (not with `t.Setenv` or shared mutable DB state).
- Integration tests use **testcontainers** via `internal/infra/dbtest` (Docker required).
- **Scenario coverage**
  - Cover success, refusal/race/recovery failure, and adversarial requests.
  - Attack examples include foreign IDs, forged versions, cross-org resources, replay, compression bombs, control characters, and missing or mismatched CSRF headers.
  - Exercise server boundaries through raw Connect clients and headers; UI predicates do not verify authorization.
  - Every scenario has at least three or four distinct success cases and at least three or four distinct failure cases.
  - Choose cases that differ in kind (boundaries, inputs, lock or state shapes, attack angles), not repetitions of one example.
- **Behavioral red baseline**
  - A compile error is not a red; preserve old behavior behind a new signature until the test observes an incorrect value.
  - To strengthen existing behavior, revert the logic, write the failing scenario, then reimplement.
  - For a guard initially tested green, break it, verify failure, and restore it.
- Always `-count=1`: a cached pass is not a red baseline.

## Development order — DDD × TDD × ports (the cycle)
Every feature follows this loop; don't write the implementation first:
1. **Domain model** — value objects/entities + vocabulary in `internal/domain/<context>/` (file-split applies from the first file).
2. **Port** — the consumer-defined interface in the package that will *use* it (`ports.go`, ISP: only the methods that package calls).
3. **Red** — write the test as a **scenario**: TDD here exists to verify *scenarios* (the use-case's observable behavior — e.g. "bootstrap → wrong password → unknown email → login → logout emits exactly these audit events"), not implementation details.
   Fakes implement the ports; run the test and watch it fail.
4. **Green** — implement the use case / adapter until the scenario passes.
5. **Refactor** — with the scenario as the safety net; then wire the real adapter in `cmd/portcullis` and cover it with an integration/e2e test.

A build that is red mid-cycle is expected; finish the loop before `make verify`.

## Concurrent state changes

Shared workflow state is protected by database transactions, conditional state/version updates and unique constraints; a process-local mutex does not coordinate separate application instances. When both rows are required, acquire the connection before the request, and the request before its execution record (ADR-0021). Review membership locks, cascade updates and implicit foreign-key locks alongside explicit locks before adding a path.

Keep metadata transactions short: commit lease acquisition before target SQL or other external I/O. Propagate cancellation/deadlines through lock waits and roll back refused operations. Fence heartbeat and completion by owner and attempt; a competing or late report must preserve the committed terminal outcome and its audit evidence. Never retry target SQL merely because metadata locking or completion reporting failed.

Verify observable contention scenarios with Testcontainers and `-race`: competing approval/execute calls, owner cancellation, stale ownership and simultaneous terminal reports. Coordinate competing callers explicitly and bound their contexts; a passing race detector alone does not establish database transaction correctness.

## Naming — the name is the interface
A name must say what the thing is *about*, on its own, at the call site.
`can` does not (a permission check? a feature flag?) — `hasPermission` does.
A bare `requests` does not in a product full of HTTP requests — `accessRequests` does.
Prefer the domain noun over a generic one, spell it out instead of abbreviating, and name an injected callback after the question it answers (`PermissionCheck`, not `CanFn`).

- **Functions describe their operation and subject.**
  - A reader must know what the function does from its name alone, without opening its body or comment.
  - Prefer `readCSRFTokenCookie` over `csrfToken` and `toConnectionStatementClass` over `mapClass`.
  - Use `is`, `has`, or `can` for boolean predicates; use `validate` for checks returning an error.
  - Include units or data sources when they affect behavior, such as `countReasonCodePoints` or `loadRequestViewInTransaction`.
  - Keep established interface names and standard constructors when their package or receiver already provides the subject.
- **Variables and parameters reveal their role.**
  - Never use single-letter names such as `i`, `j`, `n`, `e`, `v` or `x`; abbreviate no further than `idx` or `ind` for an index.
  - Use `idx` for an index and `rowIdx`/`columnIdx` for distinct dimensions; use `attempt` for retries.
  - Prefer `parameter`, `target`, `requestID`, or `statementClass` over contextless single letters or abbreviations.
  - Include units where needed, such as `byteCount` and `timeoutSeconds`.
  - Apply this to callbacks and local helpers as well as public declarations; standard receivers and test handles may stay short when their role is clear.

The same applies to types you *imply* rather than write: do not leave a library's default generic in place when the real argument is known (a `RouteSectionProps` defaulting to `unknown` states nothing; the route's actual loader-data type does).
Rename at the definition and every call site in one pass, while the tree compiles.

## Comments point only at things the repository ships
Keep comments minimal; explain only what the code or names do not make clear.
Use one short sentence describing the declaration's behavior in Go doc comments and TypeScript JSDoc so IDE hovers are useful.
Express operations through names instead of explaining an ambiguous name in a comment.
Keep detailed rationale in ADRs; retain a short inline comment only for a non-obvious constraint or invariant.
Break comment lines only at sentence endings, never to wrap a sentence to a fixed width.
Describe the current behavior or reason; omit review history, reviewer names, and finding numbers.
Group documentation prose into paragraphs by topic; do not automatically insert a line break after every sentence. Preserve Markdown lists, tables, and code blocks.
For long list items, state the rule in a short parent bullet and put conditions, examples, and exceptions in sub-bullets.

A committed file may cite a stable product requirement section (`PRD §4.3`) or link to the [Korean](../product/prd.ko.md) or [English](../product/prd.en.md) PRD.
Avoid line-number references.
Never name, link or quote a file matched by `.gitignore` in a committed file, even when it exists locally; a fresh clone does not have it, and private notes may hold unpublished findings. The ignore files themselves are the only exception.
Keep paired translations consistent according to the [documentation policy](../README.md#language-policy).

## Minimize hardcoding
Read application and test-harness settings through isolated Viper loaders and typed structs, then validate before use. Reuse `config.NewEnvironmentLoader` for struct binding, environment key replacement and decode hooks; keep consumer-specific defaults and validation with the consumer. Direct process-environment inspection is reserved for low-level driver-environment isolation, not configuration reads.

Select resources by their domain role or identity, not a fixed collection position. Use named fields/variables for fixed roles (such as metadata and target databases) and typed keyed collections for identity-based assignments. Keep indices when order is the actual contract, such as result paging, and translate external positional formats once at the boundary.

Catalogs, roles, and configuration load from the **database/config at startup**, not Go constants (e.g. the permission catalog and role assignments are seeded in SQL).
Only fixed domain enums the code branches on (e.g. `UserStatus`) live in code. **Never hardcode role names** — resolve via DB flags (e.g. `is_bootstrap_default`).
