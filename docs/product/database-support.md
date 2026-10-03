# Database feature support

**Updated: 2026-10-03.** This table describes Portcullis product support, not whether an engine can run in Docker. PostgreSQL is the verified development alpha. MySQL is the next committed target; it is not selectable or supported yet. [ADR-0025](../adr/0025-sql-database-first-expansion.md) limits committed targets to PostgreSQL/MySQL and removes SQLite from support scope.

## Committed target feature matrix

**Verified** means covered by the PostgreSQL M1 gate. **Planned** identifies future milestone scope, with no product implementation claim. MySQL governance parity requires the shared acceptance gate; later review/preview features have their own gates. Engine CLI experiments do not count as Portcullis acceptance.

| Feature | PostgreSQL | MySQL |
| --- | --- | --- |
| Register/test/edit/archive connections; encrypted credentials | Verified | Planned |
| TLS certificate verification; explicit audited relaxation | Verified | Planned |
| Single-statement AST classification; fail-closed rejection | Verified | Planned |
| Named, typed parameter binding and SQL redaction | Verified | Planned |
| Read/write/DDL policies and approval quorums | Verified | Planned |
| Draft, immutable submission, distinct approval/rejection | Verified | Planned |
| Requester-only single-use execution; target/policy revalidation | Verified | Planned |
| Read-only enforcement and governed DML/DDL | Verified | Planned; implicit-commit DDL warning required |
| Exact integer/decimal/NULL results and typed cells | Verified | Planned |
| Server-enforced row/byte/protocol limits | Verified | Planned |
| Encrypted expiring snapshots; paging/sorting/filtering | Verified | Planned |
| Requester-only CSV download and saved-file readback | Verified | Planned |
| Timeout/cancel attempts; unknown outcome without automatic rerun | Verified | Planned |
| Organization isolation, permissions and append-only audit | Verified | Planned |
| Real-engine integration tests and real-binary browser E2E | Verified (Testcontainers; Docker Chromium) | Planned (Testcontainers) |
| Saved queries, versions, favorites and sharing | Planned (M3) | Planned (M3) |
| Deterministic SQL review facts | Planned (M2 after parity) | Planned (M2 after parity) |
| Basic EXPLAIN for supported read queries | Planned (M2; no ANALYZE) | Planned (M2; no ANALYZE) |
| Schema migration status and SQL dry-run preview | Planned (M3; no apply) | Planned (M3; no apply) |
| Deterministic schema impact facts | Planned (M3; estimates/unknowns) | Planned (M3; estimates/unknowns) |
| EXPLAIN ANALYZE / execute-then-rollback query dry-run | Deferred; no support claim | Deferred; no support claim |
| Server-side sensitive-data masking (cells/API/CSV/metadata) | Planned (M4; separate from SQL audit redaction) | Planned (M4) |
| Agent registration, scoped grants and revocation | Planned (M6 after masking gate) | Planned (M6 after masking gate) |
| Registered-agent WebMCP integration | Deferred to M6 after M5, masking and registration | Deferred to M6 after M5, masking and registration |
| Charts/dashboards | Later candidate | Later candidate |
| Git-sourced schema migration governance | Planned (M5; object matrix pending) | Planned (M5; object matrix pending) |
| Native DB client proxy / temporary web SQL console | Proxy deferred; console planned (M4) | Proxy deferred; console planned (M4) |

Verified does not mean every SQL statement, extension or engine version is accepted. Only explicitly classified forms pass; transaction/session control, native file/network I/O, multiple statements and unclassified forms are rejected. Cancellation is an attempt, not a rollback guarantee. MySQL support requires the complete shared security/behavior gate before UI exposure.

## Version and evidence boundaries

| Engine | Current evidence | Version scope |
| --- | --- | --- |
| PostgreSQL | [Full M1 verification](../operations/m1-validation.md), including actual CSV saving/readback | PostgreSQL 18 in the tested stack; ADR-0001's ≥14 design floor is not an all-version certification |
| MySQL | Native CLI engine experiments only; product adapter pending | MySQL 8.4 LTS is the primary planned CI target; measured experiment was 8.4.10, not a production-version recommendation |

The M1 gate establishes correctness for its recorded environment. Capacity/soak qualification is separate. Library (M3), including PostgreSQL/MySQL parity, basic SQL review/EXPLAIN and schema preview, remains the MVP boundary. WebMCP is excluded from MVP under [ADR-0026](../adr/0026-review-tools-before-agent-integration.md).

## Additional engines and excluded scope

| Engine | Product status | Potential feature scope / condition |
| --- | --- | --- |
| MariaDB | Candidate; native CLI experiment passed | Evaluate the complete MySQL-style governance journey with an independent parser, type, TLS and security matrix; no product support yet |
| ClickHouse | Research candidate | Consider governed analysis reads first; write/DDL and transaction guarantees require a separate decision |
| SQL Server | Research candidate | T-SQL connection/classification/execution/result contracts; appropriate x86-64 CI required |
| TiDB | Research candidate | Independently qualify MySQL-compatible SQL, types, locks and DDL before feature claims |
| CockroachDB | Research candidate | Independently qualify PostgreSQL-family contracts and transaction-retry behavior without automatic SQL reruns |
| SQLite | Excluded | No adapter or roadmap commitment; reintroduction requires a new scope decision |
| MongoDB | Outside the SQL-first scope | A separate document-operation governance model is required |

These candidate scopes are evaluation directions, not delivery promises. See [primary-source research and measured experiments](database-candidates.md) for evidence.
