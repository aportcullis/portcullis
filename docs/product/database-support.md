# Database feature support

**Updated: 2026-10-03.** This table describes Portcullis product support, not whether an engine can run in Docker. PostgreSQL is the verified development alpha. MySQL is the next committed target; it is not selectable or supported yet. [ADR-0025](../adr/0025-sql-database-first-expansion.md) limits committed targets to PostgreSQL/MySQL and removes SQLite from support scope.

## Committed target feature matrix

**Verified** means covered by the PostgreSQL M1 gate. **Planned** means required for MySQL acceptance, with no product implementation claim. Engine CLI experiments do not count as Portcullis acceptance.

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
| Browser WebMCP query assistance | Planned (after MySQL parity) | Planned (after MySQL parity) |
| Charts/dashboards | Later candidate | Later candidate |
| Git-sourced schema migration governance | Planned (M5; object matrix pending) | Planned (M5; object matrix pending) |
| Native DB client proxy / temporary web SQL console | Proxy deferred; console planned (M4) | Proxy deferred; console planned (M4) |

Verified does not mean every SQL statement, extension or engine version is accepted. Only explicitly classified forms pass; transaction/session control, native file/network I/O, multiple statements and unclassified forms are rejected. Cancellation is an attempt, not a rollback guarantee. MySQL support requires the complete shared security/behavior gate before UI exposure.

## Version and evidence boundaries

| Engine | Current evidence | Version scope |
| --- | --- | --- |
| PostgreSQL | [Full M1 verification](../operations/m1-validation.md), including actual CSV saving/readback | PostgreSQL 18 in the tested stack; ADR-0001's ≥14 design floor is not an all-version certification |
| MySQL | Native CLI engine experiments only; product adapter pending | MySQL 8.4 LTS is the primary planned CI target; measured experiment was 8.4.10, not a production-version recommendation |

The M1 gate establishes correctness for its recorded environment. Capacity/soak qualification is separate. Library (M3), including PostgreSQL/MySQL Bridge parity and WebMCP, remains the MVP boundary.

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
