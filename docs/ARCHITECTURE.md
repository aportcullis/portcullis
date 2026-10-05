# Code architecture

Portcullis ships as one Go binary with the SolidJS SPA embedded through `go:embed`. The diagrams below map the current repository: source dependencies, frontend layers and runtime calls have separate views because their arrows mean different things. For the recommended private-network production topology and remote access through Cloudflare WARP or Tailscale, see [Deployment architecture](operations/recommended-architecture.md).

## Go source dependencies

Arrows point from the importing package toward an inner dependency. `cmd/portcullis` is the composition root: it constructs concrete adapters, injects them into application services and mounts the generated Connect handlers. Its wiring is outside the dependency diagram; application services do not import their concrete adapters.

```text
┌──────────────────────────────────────────┐
│ transport/                               │
│ HTTP server + Connect RPC handlers       │
│ Decoding, auth and CSRF boundary         │
└──────────────────────────────────────────┘
                     │ imports
                     │
                     ▼
┌──────────────────────────────────────────┐    ┌──────────────────────────────────────────┐
│ app/                                     │    │ infra/                                   │
│ Use cases and consumer-owned ports       │    │ Implementations of inner ports           │
│ auth / accessrequest / execution         │◀───│ postgres / crypto / pgdialect            │
│ result / connection / connectionpolicy   │    │ dialectregistry / executionguard / ...   │
│ audit / authz / keyrotation / ...        │    │ External I/O and vendor SDKs             │
└──────────────────────────────────────────┘    └──────────────────────────────────────────┘
                     │ imports                                       │
                     │                                               │
                     │                                               │ imports domain
                     ▼                                               │
┌──────────────────────────────────────────┐                         │
│ domain/                                  │                         │
│ Entities, values and state machines      │                         │
│ Shared contracts and persistence ports   │◀────────────────────────┘
│ identity / access / connection / query   │
│ audit / encryption / setting             │
└──────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────────────────────┐
│ platform/  config / logging / health / assets / reqmeta / publicorigin                   │
│ Shared support for outer layers; setting vocabulary exception: ADR-0017                  │
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

The `infra → app` arrow represents dependencies on consumer-owned port declarations, such as the dialect registry's submission and execution interfaces. Adapters can also satisfy Go interfaces structurally without importing the declaring package; it does not mean every adapter imports `app`. The lower arrow represents dependencies on domain models and contracts.

- `transport → app → domain`: handlers translate protocol data and delegate use cases; services orchestrate domain behavior through injected ports.
- `domain/` imports only the standard library and other domain code, with no outward dependency on `app`, `infra`, `transport` or infrastructure SDKs.
- Shared persistence contracts live in `domain/`; narrow use-case ports live beside their consumer in `app/`. `infra/` implements both.
- `platform/` provides shared support. The [ADR-0017](adr/0017-runtime-settings-store.md) exception lets configuration and the logging vocabulary test reuse `domain/setting` descriptors without a dependency cycle.

## Frontend source dependencies

The SPA uses light Feature-Sliced Design. Arrows show the allowed downward import direction; callers may skip intermediate layers, so a feature can use a shared RPC client directly. Imports between different feature slices or between different entity slices are forbidden and checked by Oxlint. Generated types may be imported where needed; generated runtime clients are centralized in `shared/api`.

```text
┌──────────────────────────────────────────────────────────────────────────┐
│ web/src/app/                                                             │
│ Application wiring, router, session lifecycle, theme and layout          │
└──────────────────────────────────────────────────────────────────────────┘
                                     │ imports
                                     │
                                     ▼
┌──────────────────────────────────────────────────────────────────────────┐
│ web/src/pages/                                                           │
│ Route assembly: login, connections, requests and results                 │
└──────────────────────────────────────────────────────────────────────────┘
                                     │ imports
                                     │
                                     ▼
┌──────────────────────────────────────────────────────────────────────────┐
│ web/src/features/                                                        │
│ User actions: compose SQL, submit, approve, execute and explore          │
└──────────────────────────────────────────────────────────────────────────┘
                                     │ imports
                                     │
                                     ▼
┌──────────────────────────────────────────────────────────────────────────┐
│ web/src/entities/                                                        │
│ Entity state and API orchestration: session, connection, request         │
└──────────────────────────────────────────────────────────────────────────┘
                                     │ imports
                                     │
                                     ▼
┌──────────────────────────────────────────────────────────────────────────┐
│ web/src/shared/                                                          │
│ ui/ reusable components   lib/ helpers   api/ typed Connect clients      │
└──────────────────────────────────────────────────────────────────────────┘
                                     │ imports
                                     │
                                     ▼
┌──────────────────────────────────────────────────────────────────────────┐
│ web/src/gen/                                                             │
│ Generated protobuf contracts; runtime imports through shared/api         │
└──────────────────────────────────────────────────────────────────────────┘
```

Reusable components live in individual `shared/ui/<component>/` directories with an implementation, public API, example and README. Theme tokens and application layout live in `app/`; business actions stay in `features/`. See the [UI kit catalog](../web/src/shared/ui/README.md) and [frontend conventions](conventions/frontend.md) for reuse and customization.

## Governed query runtime

Here arrows mean runtime calls, not source imports. The metadata database and the governed target are different roles: the former stores Portcullis state, while the latter receives approved user SQL. This view focuses on execution and result exploration; other RPCs reuse the same transport and application boundaries. The HTTPS entry follows the recommended deployment; the binary's internal listener is configured separately.

```text
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│ Browser: SolidJS SPA                                                                     │
│ shared/api -> same-origin Connect RPC with session cookies and CSRF header               │
└──────────────────────────────────────────────────────────────────────────────────────────┘
                                             │ HTTPS at private ingress
                                             ▼
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│ internal/transport/server + internal/transport/connectapi                                │
│ HTTP limits, interceptors, authentication and procedure-specific authorization           │
└──────────────────────────────────────────────────────────────────────────────────────────┘
                                             │ validated request
                                             ▼
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│ internal/app/execution + internal/app/result                                             │
│ Govern approved SQL through leases; persist and explore encrypted result snapshots       │
│ Call injected ports; domain models enforce workflow invariants                           │
└──────────────────────────────────────────────────────────────────────────────────────────┘
                                             │ runtime calls through ports
                                             │
                       ┌─────────────────────┴──────────────────────┐
                       ▼                                            ▼
┌──────────────────────────────────────────┐    ┌──────────────────────────────────────────┐
│ infra/postgres + infra/crypto            │    │ infra/dialectregistry -> infra/pgdialect │
│ Metadata, leases, audit and snapshots    │    │ SQL execution through a target adapter   │
│ Encryption and credential decoding       │    │ PostgreSQL registered in the binary      │
└──────────────────────────────────────────┘    └──────────────────────────────────────────┘
                       │                                            │
                       ▼                                            ▼
┌──────────────────────────────────────────┐    ┌──────────────────────────────────────────┐
│ Metadata PostgreSQL                      │    │ Governed target PostgreSQL               │
│ App state + encrypted snapshots          │    │ Approved user SQL under target policy    │
└──────────────────────────────────────────┘    └──────────────────────────────────────────┘
```

Submission and approval run through `app/accessrequest` before execution. `app/execution` checks the stored approval unit and acquires a database-backed execution lease before target I/O. Lease acquisition commits before the target query; completion is fenced by owner and attempt, and late reports preserve the committed terminal outcome. These boundaries coordinate shared workflow state across callers; a process-local mutex alone cannot provide that protection.

`app/result` encrypts bounded snapshots for persistence and serves paging, sorting, filtering and CSV export from the stored result, without rerunning the target SQL. Result processing has a bounded worker pool. Startup and periodic maintenance reconcile execution leases and purge expired snapshots. See [ADR-0021](adr/0021-governed-query-execution.md) and the [concurrency conventions](conventions/code.md#concurrent-state-changes).

The production composition root currently registers PostgreSQL as the target adapter. MySQL adapter code is absent from the current tree and will be introduced in M2; the engine-selection boundary currently serves PostgreSQL execution. Saved query assets, BI charts, dashboards, schema governance and agent integration follow the [PRD roadmap](product/prd.en.md).

## Directory map

| Path | Responsibility |
|---|---|
| `cmd/portcullis` | Composition root, startup, migration mode and key-rotation command wiring |
| `internal/transport/server` | HTTP limits, health endpoints, RPC mounts and embedded SPA delivery |
| `internal/transport/connectapi` | Connect handlers, protocol conversion and security interceptors |
| `internal/app` | `auth`, `authz`, `audit`, `auditevent`, `connection`, `connectionpolicy`, `accessrequest`, `execution`, `result`, `keyrotation` |
| `internal/domain` | `identity`, `audit`, `connection`, `access`, `query`, `encryption`, `setting` models and contracts |
| `internal/infra` | PostgreSQL repositories, crypto, SQL dialects, dialect registry, execution guard, Google OIDC and test adapters |
| `internal/platform` | Viper configuration, structured logging, health, embedded assets and request metadata |
| `proto/`, `gen/`, `web/src/gen/` | Protobuf source and committed generated Go/TypeScript contracts |
| `internal/infra/postgres/queries/`, `internal/infra/postgres/db/` | SQL query source and committed sqlc output |
| `migrations/` | Embedded SQL migrations, applied in migration mode or opted-in startup |
| `web/` | SolidJS SPA, unit tests and browser E2E harness |
| `tests/load/` | TypeScript k6 workloads and the isolated Go fixture server |

## Build and verification boundaries

`make generate` regenerates protobuf clients and sqlc repositories from their source contracts; generated files are committed and must not be edited by hand. `make web` builds the SPA into `internal/platform/assets/dist`, which the Go binary embeds. The browser receives those static assets and calls same-origin Connect endpoints; it does not connect directly to either database.

Scenario unit tests exercise domain and application behavior through exported APIs and fake ports. Testcontainers integration tests verify real adapters and database contention. Playwright drives the real binary and databases through the browser; k6 exercises workload scenarios. See the [tooling guide](conventions/tooling.md) for the verification gates.

## File organization

Multi-concept packages keep each concept's types and behavior together; single-concept packages split interfaces, types, constants and errors by kind. Tests default to external `package X_test`; narrowly justified white-box tests use `*_internal_test.go`. The [code conventions](conventions/code.md) define the development order, DDD × TDD cycle and immediate review before each small commit.
