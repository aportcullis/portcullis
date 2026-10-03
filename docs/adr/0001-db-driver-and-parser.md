# ADR-0001: Managed-DB driver & SQL parser

- **Status:** Accepted — drivers and parsers settled.
  (Amended 2026-07-04: PG/SQLite parser picks closed, minimum engine versions pinned; the remaining acceptance gate is the ADR-0002 fixture suite passing during adapter work. Amended 2026-07-19: PG gate CLOSED — pgplex/pgparser v0.2.0 pinned in go.mod, the PG-applicable ADR-0002 fixtures pass; MySQL/SQLite gates remain open until those adapters land in M2.)
- **Date:** 2026-06-27 (amended 2026-07-04, 2026-07-19)

> **Scope amendment, 2026-10-03:** [ADR-0025](0025-sql-database-first-expansion.md) removes SQLite from supported targets. SQLite choices below are historical and do not authorize an adapter. PostgreSQL/MySQL choices remain applicable.

## Context
For each managed database engine (PostgreSQL, MySQL, SQLite) two components must be chosen, and both sit behind the `QueryDialect` boundary so the upper layers never branch on engine.

1. **Native driver** — executes the approved statement.
   Constraints:
   - **CGO-free, single static binary** (the product ships as one Go binary).
   - **Context-based cancellation** (server-enforced query timeout and user cancel).
   - Maintainability and a sane security-update path.
2. **SQL parser** — backs `QueryDialect.ParseSingle` / `Classify`.
   Requirements:
   - Decide whether the input is **exactly one statement**.
   - Classify it as **read / write / ddl**.
   - **Reject when classification is uncertain (fail-closed).** Keyword/regex-only classification is explicitly disallowed because the classification is a safety gate that decides what reaches a production database.

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
| `pgplex/pgparser` | PG | none | Pure-Go goyacc port of the **PG 17.7 grammar**; AST mirrors `parsenodes.h`; 99.6% on the ~45k-statement PG regression suite; Apache-2.0; thread-safe |
| `auxten/postgresql-parser` | PG | none | Pure-Go, but derived from the **CockroachDB v20.1 grammar** — not the PostgreSQL parse tree; grammar drift vs modern PG |
| `modernc.org/sqlite` (internal parser) | SQLite | none | Same stack as the chosen driver |
| `GoSQLX` | PG/MySQL/SQLite/MSSQL | none | Single multi-dialect AST; simpler integration but edge-case fidelity and maturity unproven |

Two directions:
- **(A) Per-dialect parsers** — best fidelity per engine, normalized into our domain types.
  Higher integration cost (three libraries, three ASTs).
- **(B) Single GoSQLX** — one AST, simplest integration, but classification (a safety path) would depend on a less-proven, broad parser.

## Decision
- **Driver: settled** as in the table above (`pgx` / `go-sql-driver/mysql` / `modernc.org/sqlite`).
- **Parser: per-dialect (A), settled 2026-07-04.** Classification is a safety gate guarding the target database, so fidelity outweighs integration convenience.
  - **PostgreSQL = `github.com/pgplex/pgparser`.** It is the only pure-Go parser whose AST is the actual PostgreSQL parse tree (`parsenodes.h`-compatible, PG 17.7), so data-modifying CTEs (`CommonTableExpr` bodies), `SELECT … INTO` (`IntoClause`), and utility statements are visible exactly as PG sees them.
    Entry point: `parser.Parse(sql)` returning the statement list (top-level count > 1 ⇒ reject).
    `auxten/postgresql-parser` is **rejected** — a CockroachDB-grammar derivative cannot anchor a PG safety gate.
    The residual 0.4% regression-suite gap is covered by the allow-list model: what doesn't parse is rejected, never guessed.
  - **MySQL = `github.com/pingcap/tidb/pkg/parser`** (the parser sub-module inside the TiDB monorepo — the standalone `pingcap/parser` repo is deprecated).
    MySQL 8.0-compatible, goyacc-based, actively maintained; import only the sub-module, never top-level TiDB.
  - **SQLite = the SQLite engine itself via `modernc.org/sqlite/lib`** (same stack as the driver — no second parser): single-statement check via `sqlite3_prepare_v2`'s unparsed tail (non-empty tail beyond trailing `;`/whitespace ⇒ reject), classification via the **authorizer callback's action codes** during prepare (`SQLITE_READ`/`SQLITE_SELECT` ⇒ read; `SQLITE_INSERT`/`UPDATE`/`DELETE` ⇒ write; `SQLITE_CREATE_*`/`DROP_*`/ `ALTER_TABLE` ⇒ ddl; `SQLITE_ATTACH`/`DETACH`/`PRAGMA`/`TRANSACTION`/`SAVEPOINT` and any unlisted action code ⇒ reject), cross-checked with `sqlite3_stmt_readonly` (read claims must be readonly; note `stmt_readonly` alone is insufficient — it returns true for transaction control, which the authorizer rejects first).
    Prepare-for-classification runs against the target file read-only and the statement is never stepped.

- **Minimum engine versions (managed targets):**
  - PostgreSQL **≥ 14** — the oldest community-supported major as of 2026-07 (13 went EOL 2025-11); when 14 hits EOL (2026-11-12) the floor moves with the support window.
  - MySQL **≥ 8.0**, with **8.4 LTS as the primary CI target** (8.0 is in Oracle sustaining support; both are covered by the tidb parser).
  - SQLite = **whatever the pinned `modernc.org/sqlite` embeds** (3.5x) — the engine ships inside the binary, so there is no external version to negotiate; the managed target is a file, and format compatibility follows the embedded engine.
  - The metadata database is unaffected: PostgreSQL 18 per the deploy stack.
- **Library version pinning:** exact versions live in `go.mod` (single source of truth) and are Renovate-managed (`docs/conventions/tooling.md`); a parser bump must re-run the ADR-0002 fixture suite before merge.
- **Classification safety model = allow-list + fail-closed.** Only statements we explicitly understand and permit are classified; multi-statement input, anything uncertain, and any non-allow-listed form are rejected.
  Dangerous utility forms (CTE-wrapped DML, `SELECT ... INTO`, `COPY ... PROGRAM`, `INTO OUTFILE`, `LOAD DATA`, `ATTACH`, writable `PRAGMA`, side-effecting `EXPLAIN ANALYZE`) are pinned as **reject fixtures** — carried into ADR-0002.

## Consequences
- A thin adapter normalizes the three parser surfaces behind `QueryDialect.Classify`.
  The allow-list model keeps the safety risk low even if a parser is imperfect (unknown → reject).
- **Open items closed 2026-07-04** (PG pick = pgplex/pgparser; SQLite = engine authorizer via `modernc.org/sqlite/lib`).
  The one remaining acceptance gate: all three adapters pass the ADR-0002 classification fixture suite during adapter work — a failure reopens the pick by amendment here, it is not silently patched around.
- **PG gate closed 2026-07-19.** `github.com/pgplex/pgparser v0.2.0` is pinned in `go.mod`; the PG adapter (`internal/infra/pgdialect`) runs every PG-applicable ADR-0002 fixture, including #28/#29 added with the adapter, via the shared `internal/infra/dialecttest` suite.
  The parser exposes no deparser or AST walk helper and its lexer skips comments — properties the redactor design depends on (ADR-0016).
  MySQL/SQLite gates stay open for M2; a parser bump still re-runs the fixture suite before merge (Renovate-tracked).
- If adapter work shows per-dialect is overkill, GoSQLX can be reconsidered; for now fidelity wins.

## Sources (checked 2026-06-27; amendment items re-verified 2026-07-04)
- `modernc.org/sqlite` — pure-Go, CGO-free, context support: https://pkg.go.dev/modernc.org/sqlite
- `pg_query_go` requires CGO: https://github.com/pganalyze/pg_query_go
- Pure-Go PG parsers (pgparser / auxten / GoSQLX): https://www.bytebase.com/blog/top-open-source-sql-parsers/
- `pgplex/pgparser` — PG 17.7 `parsenodes.h`-compatible AST, 99.6% regression pass, Apache-2.0: https://github.com/pgplex/pgparser
- `auxten/postgresql-parser` derives from CockroachDB v20.1: https://github.com/auxten/postgresql-parser
- `pingcap/tidb` parser (MySQL, monorepo `pkg/parser`; standalone repo deprecated): https://github.com/pingcap/tidb/tree/master/pkg/parser
- SQLite `sqlite3_stmt_readonly` (true for transaction control — why the authorizer leads): https://sqlite.org/c3ref/stmt_readonly.html
- `modernc.org/sqlite/lib` exposes `Xsqlite3_stmt_readonly` / authorizer: https://pkg.go.dev/modernc.org/sqlite/lib
- PostgreSQL support windows (13 EOL 2025-11; 14 EOL 2026-11): https://endoflife.date/postgresql
- MySQL support (8.0 sustaining, 8.4 LTS): https://endoflife.date/mysql
