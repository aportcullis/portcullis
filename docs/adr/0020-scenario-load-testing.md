# ADR-0020: Scenario load testing and measured deployment sizing

- **Status:** Accepted
- **Date:** 2026-09-30

## Context
PRD §2.4 targets p95 below 500 ms for major non-query APIs with 50 concurrent users.
A team-size claim also needs activity rate, workload, data volume and hardware.
Execution/results are not wired yet; testing only today's governance APIs cannot establish complete-product capacity.

## Options
1. Microbenchmarks only: useful for optimization, insufficient for capacity.
2. Browser-only load: measures rendering but makes API load generation expensive.
3. k6 HTTP journeys plus existing Playwright correctness tests and resource observations.

## Decision
Choose option 3.
Author native k6 TypeScript modules and run separate strict type checking with pinned TypeScript 7.0.2 (`tsc`) and `@types/k6` 2.3.0. k6 transpiles `.ts` at runtime but does not check types.
Use Connect unary JSON over HTTP with session cookies and CSRF, keep normal authorization/rate limits, and fail on incorrect workflow transitions.
JSON covers ordinary short requests; maximum-size payload benchmarking must also use the SPA's binary encoding before making transport-capacity claims.

Provide smoke, steady load, stepped stress, soak and arrival-rate modes.
Closed VU journeys model active users with think time; open arrival-rate runs expose throughput failure without slowing arrivals when the server slows.
Require zero dropped iterations for arrival runs.
Do not equate a VU with a registered employee.

The initial suite covers browsing and create→submit→optional distinct-user review/approve→get→cancel.
Sessions are prepared outside measurement; login/Argon2 bursts need a separate future profile.
Execution→result page/sort/CSV, timeout/cancel and result eviction join the suite when their APIs exist.
Never substitute a successful create/submit for a measured execution.

Add a separate real-PostgreSQL query-workload benchmark before Execute RPC exists.
Compare the adapter with raw pgconn using fresh connections, read-only transactions and full stream consumption for both.
Cover indexed/unindexed lookups, deep OFFSET/keyset, aggregate/sort/join, narrow/JSON results, wide results and an oversized single cell at explicit data scales.
Record instrumented DB execution time separately from client consumption, plus per-query percentiles, payload bytes and Go allocation totals.
These adapter-only measurements exclude approval, lease, audit, result encryption/storage and HTTP/browser delivery; they cannot certify product capacity or memory peaks.

Capacity gates are p95 <500 ms overall and per exercised control-plane RPC, HTTP failure rate <1%, zero journey failures, all semantic checks passing and completed journeys >0.
Record throttling separately while retaining it as failure; do not hide 429 responses or disable the limiter to claim production capacity.
Today's authenticated IP budget is 5 requests/s with a burst of 240, so an office NAT can be the bottleneck independently of CPU/RAM.
Changing that budget requires measured workload evidence and a separate policy decision.

Use isolated test installations and synthetic data.
A fixture assigns one distinct requester to each VU; reviews use a different authorized approver with quorum 1.
Pre-issued sessions avoid login-rate limits and same-user session rotation during a run.
Cancellation closes test requests but does not delete their audit history; reset the isolated dataset between comparable runs.
Never write session tokens or payloads to reports.

## Consequences

### Validated RPC boundaries (2026-10-03)

The owner requested generic async RPCs, Zod validation and no type or non-null assertions. Define method-specific input/output schemas with Zod 4.6.5 and infer the load client contracts from them. Share request construction, headers, CSRF, timeouts, metrics and response validation across `rpc` and `rpcAsync`; `executeAsync` delegates to the latter.
Requests reject unknown fields; response schemas validate known fields and project them while tolerating additive fields. Normalize protobuf-omitted arrays/scalars, preserve int64 decimal strings and oneof cell types, and retain genuinely optional messages/timestamps. Validate fixtures before workload execution. Generic types provide compile-time checks; Zod validates network data at runtime.
Load lint rejects type assertions, non-null assertions and explicit `any`.

Use a local JSON codec for syntax conversion and schema validation. JSON.parse/stringify stay inside that boundary because they implement JSON syntax; neither a generic assertion nor passing an object to k6 replaces validation. Contract errors contain no payload or credential values. Use k6 refined response types and bounded binary reads for CSV framing instead of assertions.
Zod’s standard base64 validator calls browser `atob`, which k6 does not supply; use a portable padded-base64 string refinement and verify it inside k6 rather than weakening the schema or adding browser globals.

k6 does not resolve npm imports. Bundle TypeScript sources and Zod locally using pinned esbuild 0.28.2 with ESM/neutral platform, and leave only k6 built-ins external. No remote module imports, direct package implementation paths, install-script approvals or trust-policy exceptions are introduced.
Zod has no installation script; esbuild's installation script remains denied, using its integrity-checked optional platform package instead. Node declarations supply shared schema/test types, not Node runtime dependencies in the k6 bundle. Generated bundles are ignored. `make load-check` runs strict checking, contract tests and bundling in the existing verification gate.
Product requirements are unchanged.

Existing performance evidence used the preceding client. Contract and real-server smoke tests verify correctness after this change; repeat controlled capacity measurements before applying old load-generator performance conclusions to the new client.

Primary references checked 2026-10-03: [Zod codecs](https://zod.dev/codecs), [k6 asyncRequest JSON/form behavior](https://grafana.com/docs/k6/latest/javascript-api/k6-http/asyncrequest/), [k6 module bundling](https://grafana.com/docs/k6/latest/using-k6/modules/), [esbuild bundling](https://esbuild.github.io/getting-started/#bundling), and [ProtoJSON](https://protobuf.dev/programming-guides/json/).

### Named resource assignments (2026-10-03)

Keep metadata and governed target database references in named variables rather than positional array slots; the container collection exists only for cleanup, including partial startup failure. Assign synthetic identities to explicit k6 virtual-user IDs in a read-only map, reject unassigned IDs and validate required approvers before review scenarios.
The JSON fixture format stays unchanged: its ordered entries are assigned to one-based virtual-user IDs once at the loading boundary. This preserves per-VU isolation without numeric offsets at journey call sites. Product requirements are unchanged. References: [Go named fields and declarations](https://go.dev/doc/effective_go), [Zod required properties](https://zod.dev/api).

- Add `make load-test` separately from `make verify`; smoke can become a CI gate once reproducible account provisioning exists.
  Long stress/soak and hardware sizing run on controlled benchmark hosts.
- Compare candidate combined app/metadata-DB hosts at 2 vCPU/4 GiB, 4 vCPU/8 GiB and 8 vCPU/16 GiB with explicit app/DB resource allocation.
  These are experiment tiers, not recommended minimums.
- Publish recommended specs only after representative datasets, full execution/results coverage and three repeatable passing runs with resource headroom.
  Record target DB and load generator separately.
- A laptop smoke validates script integration, not support for a stated number of employees.
  Existing alpha priorities remain unchanged.

## Sources (checked 2026-09-30)
- [k6 scenarios](https://grafana.com/docs/k6/latest/using-k6/scenarios/).
- [Constant arrival rate](https://grafana.com/docs/k6/latest/using-k6/scenarios/executors/constant-arrival-rate/) and [ramping VUs](https://grafana.com/docs/k6/latest/using-k6/scenarios/executors/ramping-vus/).
- [Thresholds](https://grafana.com/docs/k6/latest/using-k6/thresholds/) and [cookies](https://grafana.com/docs/k6/latest/using-k6/cookies/).
- [k6 v2.3.0](https://github.com/grafana/k6/releases/tag/v2.3.0).
- ADR-0006 (session rotation), ADR-0010 (IP admission), ADR-0011 (result quotas).

- [k6 native TypeScript support](https://grafana.com/docs/k6/latest/using-k6/javascript-typescript-compatibility-mode/).
- [TypeScript 7 announcement](https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/).
