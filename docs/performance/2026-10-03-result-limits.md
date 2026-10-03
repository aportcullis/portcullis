# Result limits and cancellation integration — 2026-10-03

This is a bounded functional smoke, not a concurrency, soak or production sizing qualification. The k6 2.3.0 bundle used one virtual user and three iterations against the real Go binary, two owned Testcontainers PostgreSQL databases, distinct synthetic requester/reviewer accounts and the least-privilege metadata runtime role. The target contains 100,000 synthetic rows; no production data was used.

## Artifact identity

- Application SHA-256: `c11dd6fb56c3d9472b91784d4a731120d9ffa69e2c5d089d023957ae6952cb16`.
- PostgreSQL image: `postgres:18.6-alpine3.24@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873`.
- Runtime: `linux/arm64`; application and databases ran through Docker on the development machine.
- Suite: `tests/load/dist/result-limits.js`, built by `make load-check`.

## Scenarios and results

| Scenario | Observed contract |
|---|---|
| 100,000-row query | Snapshot truncated to 10,000 rows; first page and total agree; CSV contains one header and 10,000 data rows |
| Wide text result | Result truncated below 1,000 rows, within the 25 MiB snapshot cap; bounded page and total agree |
| Active execution cancellation | Lease observed before Stop; durable `OUTCOME_UNKNOWN` and no result snapshot; cancellation settles within five seconds |

All three journeys completed, with 24/24 checks passing, zero HTTP failures and every configured threshold green. Control-plane p95 was **38.35 ms** and cancellation settlement was **2.03 s**. Execution latency is a separate metric and is excluded from the control-plane p95 threshold.

The standalone async execution bundle also passed all 12 checks using the shared cancellation helper. The harness subsequently exited successfully, removed its private session fixture and terminated its owned databases. These scenarios preserve conservative SQL outcome handling; they do not establish correct circuit-breaker classification for repeated cancellation, which remains a separate review finding. Summary artifacts are retained locally under `tests/load/results/` and excluded from version control.

## Reproduce

Start a fresh isolated fixture with `make load-server`, then run:

```sh
BASE_URL=http://127.0.0.1:18082 LOAD_FIXTURES="$PWD/tests/load/fixtures.local.json" JOURNEY=execute VUS=1 k6 run tests/load/dist/result-limits.js
BASE_URL=http://127.0.0.1:18082 LOAD_FIXTURES="$PWD/tests/load/fixtures.local.json" JOURNEY=execute VUS=1 k6 run tests/load/dist/async-execution.js
```

Use a new fixture for comparable measurements. The recorded Docker run placed k6 in the fixture container's network namespace so the application's loopback listener remained private. Clean up the harness and its databases after the run.
