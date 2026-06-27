# ADR-0001: Managed-DB driver & SQL parser

- **Status:** Accepted — drivers settled; parser direction = per-dialect. Specific PG/SQLite library picks remain POC items.
- **Date:** 2026-06-27

## Context
For each managed database engine (PostgreSQL, MySQL, SQLite) two components must be chosen,
and both sit behind the `QueryDialect` boundary so the upper layers never branch on engine.

1. **Native driver** — executes the approved statement. Constraints:
   - **CGO-free, single static binary** (the product ships as one Go binary).
   - **Context-based cancellation** (server-enforced query timeout and user cancel).
   - Maintainability and a sane security-update path.
2. **SQL parser** — backs `QueryDialect.ParseSingle` / `Classify`. Requirements:
   - Decide whether the input is **exactly one statement**.
   - Classify it as **read / write / ddl**.
   - **Reject when classification is uncertain (fail-closed).** Keyword/regex-only
     classification is explicitly disallowed because the classification is a safety gate
     that decides what reaches a production database.

Library landscape was verified on the web on 2026-06-27 (sources below).

## Options

### Driver
| Engine | Candidate | CGO | Cancel | Verdict |
|---|---|---|---|---|
| PostgreSQL | `jackc/pgx` | none | yes | **Adopt** (shared with the metadata store) |
| MySQL | `go-sql-driver/mysql` | none | yes | **Adopt** (pure Go, standard `database/sql`) |
| SQLite | `modernc.org/sqlite` | **none** | yes | **Adopt** (C→Go transpiled port, SQLite 3.5x) |
| SQLite (alt) | `mattn/go-sqlite3` | **CGO** | yes | Reject (violates single-binary constraint) |

The CGO-free constraint makes the driver layer effectively a single choice per engine, so it is settled.

### Parser
| Candidate | Scope | CGO | Notes |
|---|---|---|---|
| `pg_query_go` | PG | **CGO** | Bundles the PG server parser; highest fidelity but violates the constraint → **excluded** |
| `pingcap/tidb` parser (`pkg/parser`) | MySQL | none | MySQL 8.0 compatible, strongest DDL+DML coverage, most widely adopted |
| `pgparser` / `auxten/postgresql-parser` | PG | none | Pure-Go (goyacc). `pgparser` reports 99.6% on the PG17 regression suite |
| `modernc.org/sqlite` (internal parser) | SQLite | none | Same stack as the chosen driver |
| `GoSQLX` | PG/MySQL/SQLite/MSSQL | none | Single multi-dialect AST; simpler integration but edge-case fidelity and maturity unproven |

Two directions:
- **(A) Per-dialect parsers** — best fidelity per engine, normalized into our domain types. Higher integration cost (three libraries, three ASTs).
- **(B) Single GoSQLX** — one AST, simplest integration, but classification (a safety path) would depend on a less-proven, broad parser.

## Decision
- **Driver: settled** as in the table above (`pgx` / `go-sql-driver/mysql` / `modernc.org/sqlite`).
- **Parser: per-dialect (A), recommended.** Classification is a safety gate guarding the
  target database, so fidelity outweighs integration convenience.
  - MySQL = `pingcap/tidb` parser (lead candidate)
  - PostgreSQL = `pgparser` preferred, `auxten/postgresql-parser` as fallback — pick via POC
  - SQLite = `modernc.org/sqlite` internal parser preferred — confirm via POC
- **Classification safety model = allow-list + fail-closed.** Only statements we explicitly
  understand and permit are classified; multi-statement input, anything uncertain, and any
  non-allow-listed form are rejected. Dangerous utility forms (CTE-wrapped DML,
  `SELECT ... INTO`, `COPY ... PROGRAM`, `INTO OUTFILE`, `LOAD DATA`, `ATTACH`,
  writable `PRAGMA`, side-effecting `EXPLAIN ANALYZE`) are pinned as **reject fixtures** —
  carried into ADR-0002.

## Consequences
- A thin adapter normalizes the three parser ASTs behind `QueryDialect.Classify`. The
  allow-list model keeps the safety risk low even if a parser is imperfect (unknown → reject).
- **Open items (close before flipping to Accepted, via POC):**
  1. Final PG parser pick — `pgparser` vs `auxten`.
  2. Whether the `modernc` internal parser is sufficient for SQLite classification.
  3. Whether all three parsers pass the ADR-0002 classification fixtures.
- If the POC shows per-dialect is overkill, GoSQLX can be reconsidered; for now fidelity wins.

## Sources (checked 2026-06-27)
- `modernc.org/sqlite` — pure-Go, CGO-free, context support: https://pkg.go.dev/modernc.org/sqlite
- `pg_query_go` requires CGO: https://github.com/pganalyze/pg_query_go
- Pure-Go PG parsers (pgparser / auxten / GoSQLX): https://www.bytebase.com/blog/top-open-source-sql-parsers/
- `pingcap/tidb` parser (MySQL, `pkg/parser`): https://pkg.go.dev/github.com/pingcap/tidb/parser
