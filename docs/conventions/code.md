# Code conventions (Go)

## Layered DDD
`transport → app → domain ← infra`; `platform` is cross-cutting. Dependencies point **inward**.
`internal/domain/` imports nothing outward (no app/infra/transport, no third-party infra SDKs) —
only stdlib and other domain code. Domain declares **ports** (interfaces); `infra/` implements them;
`app/` (use cases) and `transport/` (Connect RPC + HTTP) orchestrate. See
[../ARCHITECTURE.md](../ARCHITECTURE.md).

## File organization — split by kind
Within a package, separate declarations: interfaces → `port.go`/`repository.go`; custom types →
`types.go`; constants → `const.go`; sentinel errors → `errors.go`; the primary type's constructor and
methods in the main file. Prefer separation.

## Tests — TDD, black-box
- Always external `package X_test`.
- Practice **TDD**; table-driven; add `t.Parallel()` where safe (not with `t.Setenv` or shared
  mutable DB state).
- Integration tests use **testcontainers** via `internal/infra/dbtest` (Docker required).

## Minimize hardcoding
Catalogs, roles, and configuration load from the **database/config at startup**, not Go constants
(e.g. the permission catalog and role assignments are seeded in SQL). Only fixed domain enums the
code branches on (e.g. `UserStatus`) live in code. **Never hardcode role names** — resolve via DB
flags (e.g. `is_bootstrap_default`).
