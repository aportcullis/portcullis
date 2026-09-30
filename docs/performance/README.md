# Performance and deployment sizing

No measured minimum or recommended production specification has been published yet.
The initial k6 suite measures the current governance APIs; execution and result processing remain pending.
See [ADR-0020](../adr/0020-scenario-load-testing.md).

## Run the suite

`k6 run governance.ts` executes TypeScript directly without a bundling step. k6 removes types during execution; `make load-typecheck` separately runs TypeScript 7 `tsc` with strict checking before `make load-test`.

The suite separates workload configuration (`config.ts`), typed RPC contracts/client (`contracts.ts`, `client.ts`), fixture validation (`fixtures.ts`) and user journeys (`journeys.ts`).
`governance.ts` schedules journeys and records outcomes.

Use k6 **2.3.0**, TypeScript **7.0.2** and an isolated Portcullis installation with synthetic accounts and an active test connection.
Create one requester per VU using the existing test/admin provisioning process.
Account-management RPC provisioning is not implemented yet.
Obtain their session and CSRF cookie values from Login and copy the fixture shape in [fixtures.example.json](../../tests/load/fixtures.example.json) into `tests/load/fixtures.local.json`.
Keep the file private; it is gitignored.
Log in only once per account because a new login revokes its previous session.

Requesters need `requests.list/get/create`.
Reviewers need `requests.get/approve`; their connection must permit read with exactly one required approval.
A fixture must reference a registered active connection.
The suite does not create users, change policies, execute SQL or delete audit history.

```sh
BASE_URL=http://localhost:8080 MODE=smoke JOURNEY=browse make load-test
BASE_URL=http://localhost:8080 MODE=smoke JOURNEY=submit make load-test
BASE_URL=http://localhost:8080 MODE=smoke JOURNEY=review make load-test
BASE_URL=http://localhost:8080 MODE=load JOURNEY=mixed VUS=50 THINK_SECONDS=45 make load-test
BASE_URL=http://localhost:8080 MODE=stress JOURNEY=mixed VUS=100 make load-test
BASE_URL=http://localhost:8080 MODE=soak JOURNEY=mixed VUS=50 DURATION=1h make load-test
BASE_URL=http://localhost:8080 MODE=arrival JOURNEY=browse VUS=50 RATE=1 make load-test
```

`mixed` sends one review journey per five iterations per VU; the others browse.
Closed-mode think time defaults to 45 seconds.
`RATE` counts journeys/second, not HTTP requests/second.
A browse journey calls Me, List and ListRequestableConnections; a review journey creates, submits, reads as reviewer, approves, reads as requester and cancels.
Login preparation is excluded from latency samples.
Tokens are sent explicitly as cookies with CSRF headers, including in local HTTP testing; capacity certification must use the intended HTTPS deployment.

`load`/`soak` use a fixed active-user count; `stress` ramps through 25%, 50%, 100% of VUS and back to zero.
`arrival` sustains its start rate independently of response latency.
Every mode exits nonzero if a threshold fails.
The smoke mode runs three journeys per VU.

Docker alternative (macOS; use a reachable host address on other platforms):

```sh
docker run --rm -v "$PWD:/work:ro" -w /work \
  grafana/k6:2.3.0@sha256:9c2dee7f8ed74d317e4027c06a10f169b625638189de8d4555d0b3486a5aeb34 \
  run -e BASE_URL=http://host.docker.internal:8080 -e JOURNEY=browse tests/load/governance.ts
```

To retain aggregate results without payloads:

```sh
mkdir -p tests/load/results
MODE=load JOURNEY=mixed VUS=50 k6 run --summary-export=tests/load/results/summary.json tests/load/governance.ts
```

Use a unique output filename per run.
Do not enable HTTP debug logging or store raw responses.
Sessions must remain valid for the entire run; an expired session is a fixture failure, not proof of resource exhaustion.

## Capacity experiment

### Query workloads before Execute RPC

`make query-bench` runs the real PostgreSQL execution adapter and a raw pgconn reference against the same synthetic dataset.
Both open a fresh connection, begin a read-only transaction, consume every row and commit/close.
Raw pgconn counts wire payload bytes without converting rows into domain cells; it is a reference path, not a production bypass.

[Initial measurements and findings](benchmarks/2026-09-30-query-workloads.md): 100k/1m-row warm-cache runs, with latency, allocation totals and unresolved large-cell limits.

```sh
make query-bench
PORTCULLIS_BENCH_ROWS=1000000 make query-bench
```

Default data is 100k orders, 1k customers, a primary-key index and a customer/id index.
Supported scales are 10k–1m orders.
Workloads include point lookup with/without an index, first page, 90%-deep OFFSET versus keyset, full-table aggregation, unindexed top-N sorting, an indexed join, 10k narrow/JSON rows, 2000 wide rows (~23.4 MiB of values) and a single ~27.5 MiB cell.
All workload SQL must pass the current read classifier before execution; expected row counts are checked.

`p50_ms`/`p95_ms` cover connect→query→full consumption→transaction completion.
`db_exec_ms` is one `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` observation per workload, excludes normal client transmission and has instrumentation overhead.
`payload_bytes/op` counts cell value bytes, not complete HTTP/encoded result size.
`B/op` counts total allocations, not peak RSS or concurrent memory requirements.
Neither this benchmark nor the adapter enforces the future result-store caps.
The oversized-cell case deliberately probes that gap on a synthetic isolated DB.

The benchmark uses warm caches and three 20-iteration repeats within one run; percentiles are exploratory rather than production tail-latency evidence.
Query plans, disk/network latency, TLS, concurrent executions and full result persistence need controlled-host runs after the executor lands.
See [PostgreSQL EXPLAIN](https://www.postgresql.org/docs/current/using-explain.html) and [LIMIT/OFFSET](https://www.postgresql.org/docs/current/queries-limit.html).

| Candidate host budget (app + metadata PostgreSQL) | Purpose | Status |
|---|---|---|
| 2 vCPU / 4 GiB RAM / SSD | Small-team baseline candidate | Unmeasured |
| 4 vCPU / 8 GiB RAM / SSD | Initial standard-host candidate | Unmeasured |
| 8 vCPU / 16 GiB RAM / SSD | Higher activity/results candidate | Unmeasured |

The generator and target database use separate hosts.
Pin architecture, versions, limits, storage latency and network topology; record how the combined CPU/RAM budget is allocated between app and metadata DB.
Start each tier at 10, 25, 50 and 100 active users with the same activity mix.
Repeat with 1k, 10k and 100k pre-existing requests/audit events; record the exact seed and row counts.
The current script adds requests and audit events, so reset to the same snapshot before each repeat.
The application must use its least-privilege runtime role and intended TLS configuration.

Measure CPU, RSS, container OOM/restarts, metadata DB connections and wait/lock times, storage I/O, journey throughput, per-RPC p50/p95/p99, failures, 429 count and dropped iterations.
Collect resource measurements on the benchmark host alongside k6; the script does not automatically collect them.

Compare office NAT traffic first: the current IP limiter sustains **5 authenticated requests/s** with a burst of 240.
Fifty users each browsing every 45 seconds offer roughly 3.3 RPC/s before response time; heavier review/results activity can exceed that budget.
A shared IP and many independent IPs are separate experiments.
Do not spoof forwarding headers or disable admission to conceal the office-NAT limit.

After the executor lands, add representative fast reads, slow reads, 10k-row/25 MiB caps, CSV/sort/filter and cancellation/unknown-outcome workloads.
Observe result-store quota eviction and worker saturation.
PostgreSQL target-query latency must be reported separately from the 500 ms control-plane target.
Admission/429 behavior under deliberate overload has its own expected-rejection profile; the initial suite treats any rejection as a failed capacity run.

## Publish a recommendation

Require three passing 10-minute steady runs and a passing 1-hour soak with no restarts/leaks and at least 30% CPU/RAM headroom at the target workload.
Keep this headroom rule provisional until measurements justify it.
Publish the largest passing activity profile, not just the largest attempted VU count.

Report: commit, k6/app/DB versions, hardware/resource split, dataset size, HTTPS/IP topology, concurrent active users, think time, offered and completed journeys/s, per-RPC latency, failure/429/drop counts and resource peaks.
Mark today's results **governance-only** until execution/results are covered.

Translate active users to team size only with an explicit observed active fraction: e.g. 50 active users at a measured 10% peak activity fraction corresponds to an illustrative 500-person organization.
It is not a support guarantee; query frequency and result size must also match the tested workload.

## Integration validation (2026-09-30)

k6 2.3.0 passed submit, distinct-reviewer approval and mixed smoke runs (three journeys each, one VU) against the real Go server and a fresh PostgreSQL 18 container.
A short arrival-rate browse run also passed, including zero dropped iterations.
Stress/soak configurations passed `k6 inspect`; their long runs have not been executed.
This used the local E2E stack with HTTP and a privileged metadata role, so it validates script integration only.
No production capacity or recommended specification is inferred from it.

After conversion to native TypeScript modules, TypeScript 7.0.2 strict checking and k6 module inspection passed.
Submit and mixed browse/distinct-reviewer approval smoke runs also passed against a new isolated stack.
