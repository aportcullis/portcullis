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
