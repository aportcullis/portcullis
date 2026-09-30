# Query workload observations — 2026-09-30

The current PostgreSQL adapter handles ordinary reads with modest latency in this local warm-cache test.
Deep OFFSET and large result values deserve explicit treatment before recommending production capacity.
These are sequential adapter-only measurements, not the full governed Execute path.

## Method

- Host reported by Go: Apple M3 Pro, darwin/arm64; benchmark GOMAXPROCS=11.
  Go 1.27.1; base HEAD `a7d3dac` with uncommitted working-tree changes.
  PostgreSQL 18.4 ran in Docker with the repository's pinned image.
  No fixed container CPU/RAM/storage budget was imposed.
- Synthetic data: 100k orders/1k customers; a second run used 1m orders.
  Tables have PK and customer/id indexes, plus deliberately unindexed fields.
- Each sub-benchmark ran 20 iterations, repeated three times.
  All queries passed read classification and returned their expected row counts.
  Each execution opened a new connection and consumed the full result in a read-only transaction.
- Percentiles below are the range of the three per-repeat p95 values, not a pooled 60-sample percentile.
  Caches are warm; repetitions share the seeded benchmark process/database.
  Sample size is small and OS/VM scheduling produced noticeable outliers.
- The raw reference uses pgconn and counts text-format values without domain-cell conversion.
  Both paths include connection, transaction and transfer time.
  Their runs are sequential, so differences are observational, not a precise isolated overhead estimate.
- `db_exec_ms` in raw artifacts is one instrumented EXPLAIN ANALYZE observation.
  It excludes ordinary client transmission; the wide-result cases show why it is insufficient on its own.

## Adapter latency

| Workload | 100k rows, p95 ms | 1m rows, p95 ms |
|---|---:|---:|
| PK lookup (1 row) | 4.12–5.43 | 5.09–9.40 |
| Unindexed lookup (1 row) | 9.65–10.64 | 25.30–26.40 |
| First indexed page (100 rows) | 4.95–14.32 | Not run |
| 90%-deep OFFSET (100 rows) | 12.03–13.60 | 229.50–319.20 |
| Same-location keyset (100 rows) | 4.27–9.27 | 4.23–8.80 |
| Full-table GROUP BY (100 groups) | 15.46–16.06 | 49.55–121.00 |
| Unindexed top-N sort (100 rows) | 11.94–12.77 | 42.14–49.93 |
| Indexed customer join | 4.06–4.76 | Not run |
| 10k narrow rows | 6.45–7.12 | Not run |
| 10k JSON rows | 8.45–12.19 | Not run |
| 2000 wide rows | 21.54–41.45 | Not run |
| Single oversized cell | 23.86–26.94 | Not run |

## Result-size and allocation observations

| Workload | Value payload | Total Go allocation per execution |
|---|---:|---:|
| 10k narrow rows | 0.07 MiB | 1.19–1.19 MiB |
| 10k JSON rows | 0.33 MiB | 1.45–1.45 MiB |
| 2000 wide rows | 23.44 MiB | 23.93–23.97 MiB |
| Single oversized cell | 27.47 MiB | 35.51–40.31 MiB |

Allocation totals (`B/op`) are not peak heap/RSS and cannot be multiplied by concurrent users to estimate required RAM.
Payload counts exclude protocol/result-envelope/encryption overhead.
Results are streamed and discarded here; result storage, encryption, sorting/CSV and browser rendering are not measured.

## Assessment and follow-up

1. Indexed lookups, keyset pages and indexed joins look reasonable as a starting point.
   This is not an arbitrary-query latency guarantee or evidence for 50 simultaneous executions.
2. Deep OFFSET deteriorated markedly at 1m rows while the equivalent keyset page stayed small.
   Keep query-result paging on the stored snapshot as already planned; do not rerun source SQL for each page.
   For metadata/history tables, measure deep pages before the PRD's existing local keyset migration option is needed.
3. Full scans, aggregates and sorts depend primarily on the target DB's data and plan.
   A small final LIMIT or result count does not bound the work done before results can be emitted.
   Keep control-plane latency separate from query latency.
4. The 23.4 MiB wide result allocated about 24 MiB while streaming; encryption/persistence will add work.
   The 27.5 MiB single cell exceeded the planned default 25 MiB result budget and allocated roughly 36–40 MiB.
   The current adapter still receives/decodes that cell before any future service-level cap can examine it.
   Result caps, oversized-cell handling and execution concurrency need scenario verification in the executor slice; fast local completion does not make this safe for unlimited parallel execution.
5. Add simultaneous fast/slow queries, cancellation, timeouts, row/byte caps and real result-store workloads once Execute RPC exists.
   Repeat on fixed candidate hardware with representative target DB latency, TLS and data skew before publishing recommended specs.

## Reproduce and inspect

```sh
make query-bench
PORTCULLIS_BENCH_ROWS=1000000 go test ./internal/infra/pgdialect -run '^$' \
  -bench '^BenchmarkQueryWorkloads$/(indexed_point|unindexed_point|deep_offset|keyset_page|aggregate|unindexed_sort)$' \
  -benchtime=20x -count=3 -benchmem
```

[100k-row raw measurements](query-workloads-100k.txt), [1m-row raw measurements](query-workloads-1m.txt), [benchmark scenarios](../../../internal/infra/pgdialect/execute_bench_test.go).
