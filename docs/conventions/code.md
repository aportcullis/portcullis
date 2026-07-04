# Code conventions (Go)

## Layered DDD
`transport → app → domain ← infra`; `platform` is cross-cutting. Dependencies point **inward**.
`internal/domain/` imports nothing outward (no app/infra/transport, no third-party infra SDKs) —
only stdlib and other domain code. Domain declares **ports** (interfaces); `infra/` implements them;
`app/` (use cases) and `transport/` (Connect RPC + HTTP) orchestrate. See
[../ARCHITECTURE.md](../ARCHITECTURE.md).

## Dependency inversion & wiring (ports and adapters)
We aim for **clean architecture in the ports & adapters (hexagonal) sense**: the domain and use
cases are the inside, every I/O technology is an adapter on the outside, and the boundary is always
a port owned by the inside. Go isn't a pure OOP language, but follow the OOP/SOLID ideas where they
fit — **loose coupling, high cohesion**. Depend on **abstractions, not concretions** (DIP): a layer takes the behavior it needs as
a **small, consumer-defined interface** (ISP — define it in the package that *uses* it, list only the
methods that package calls), and the concrete implementation is **injected** via the constructor.
"Accept interfaces, return structs."

- Example: `app/auth` declares `PasswordHasher` / `CSRFProtector` ports and never imports
  `infra/crypto`; `infra/crypto` provides `Argon2Hasher` / `CSRFProtector` adapters that satisfy them
  structurally (so infra doesn't import app either). Same shape as `domain` ports ↔ `infra/postgres`
  adapters.
- **Manual constructor injection, no DI framework.** Wire dependencies explicitly at the composition
  root (`cmd/portcullis`) — the wiring doubles as documentation. Revisit a tool (Google Wire,
  compile-time) only if that wiring outgrows one screen; avoid runtime/reflection DI containers.
- Benefit beyond layering: tests inject trivial fakes (no real Argon2/DB), so unit tests stay fast.

## File organization — split by kind
Within a package, separate declarations: interfaces → `port.go`/`repository.go`; custom types →
`types.go`; constants → `const.go`; sentinel errors → `errors.go`; the primary type's constructor and
methods in the main file. Prefer separation.

## Tests — TDD, black-box
- Default to external `package X_test` (black-box) — test through the exported surface.
- **White-box (`package X`) is allowed only** to assert an invariant with no observable black-box
  surface — e.g. a memory bound in an unexported map / key derivation, or an unexported handler
  with no wired route. Name such files `*_internal_test.go` and open with a comment stating why
  the black-box surface can't reach it. Everything else stays black-box.
- Practice **TDD**; table-driven; add `t.Parallel()` where safe (not with `t.Setenv` or shared
  mutable DB state).
- Integration tests use **testcontainers** via `internal/infra/dbtest` (Docker required).

## Development order — DDD × TDD × ports (the cycle)
Every feature follows this loop; don't write the implementation first:
1. **Domain model** — value objects/entities + vocabulary in `internal/domain/<context>/`
   (file-split applies from the first file).
2. **Port** — the consumer-defined interface in the package that will *use* it
   (`ports.go`, ISP: only the methods that package calls).
3. **Red** — write the test as a **scenario**: TDD here exists to verify *scenarios*
   (the use-case's observable behavior — e.g. "bootstrap → wrong password → unknown email →
   login → logout emits exactly these audit events"), not implementation details. Fakes
   implement the ports; run the test and watch it fail.
4. **Green** — implement the use case / adapter until the scenario passes.
5. **Refactor** — with the scenario as the safety net; then wire the real adapter in
   `cmd/portcullis` and cover it with an integration/e2e test.

A build that is red mid-cycle is expected; finish the loop before `make verify`.

## Minimize hardcoding
Catalogs, roles, and configuration load from the **database/config at startup**, not Go constants
(e.g. the permission catalog and role assignments are seeded in SQL). Only fixed domain enums the
code branches on (e.g. `UserStatus`) live in code. **Never hardcode role names** — resolve via DB
flags (e.g. `is_bootstrap_default`).
