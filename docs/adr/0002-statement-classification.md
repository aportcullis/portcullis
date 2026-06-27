# ADR-0002: Statement classification table & test fixtures

- **Status:** Accepted — classification table and reject-fixtures fixed. Per-engine parser structure confirmed via the fixture suite during adapter work.
- **Date:** 2026-06-27

## Context
Every approved statement is classified as `read`, `write`, or `ddl` before execution, and the
class is matched against the per-connection policy. The classifier is a **safety gate**: a
misclassification can let a write reach a read-only connection. ADR-0001 fixes the parsers and
an **allow-list + fail-closed** model. This ADR pins the concrete classification table and the
fixtures all three engines must satisfy, so behavior is identical and testable across
PostgreSQL, MySQL, and SQLite.

Principles:
- **Exactly one statement.** More than one top-level statement → reject (a single trailing `;` is fine).
- **Allow-list.** Only the forms below are classifiable; everything else → reject.
- **Fail-closed.** Parse error, ambiguity, or an unknown node anywhere in the tree → reject.
- The class is the **highest-privilege effect** present in the statement (a read wrapper around a write is a write).

## Decision

### Classification table
| Class | Allowed forms |
|---|---|
| `read` | `SELECT` (no data-modifying CTE, no `INTO`), `VALUES`, `TABLE x`, `WITH … SELECT` (all CTE terms read-only), `SHOW`, `EXPLAIN <read stmt>` **without** `ANALYZE` |
| `write` | `INSERT`, `UPDATE`, `DELETE`, `MERGE`, MySQL `REPLACE`, SQLite `UPSERT`, any `WITH … <DML>`, DML with `RETURNING` |
| `ddl` | `CREATE`, `ALTER`, `DROP`, `TRUNCATE`, `RENAME`, `COMMENT`, PG `SELECT … INTO <newtable>` (creates an object) |

### Always reject (regardless of policy)
- **Multiple statements** (top-level count > 1).
- **Transaction control** — `BEGIN`/`START TRANSACTION`/`COMMIT`/`ROLLBACK`/`SAVEPOINT`. (The server owns the transaction.)
- **Session/engine mutation** — `SET`, `RESET`, MySQL `USE`, writable `PRAGMA`.
- **File/network/utility** — `COPY … (FROM|TO|PROGRAM)`, `SELECT … INTO OUTFILE/DUMPFILE`, `LOAD DATA`, `ATTACH`/`DETACH`.
- **Side-effecting introspection** — `EXPLAIN ANALYZE` (executes the statement).
- **Opaque effect** — `CALL`, `DO`, stored-procedure invocation, and any vendor-specific command not on the allow-list.
- **Unparsed / ambiguous** — anything the parser cannot fully resolve.

### Pinned edge-case fixtures (must hold on all three engines where the syntax exists)
| Input (shape) | Expected |
|---|---|
| `SELECT … ;` | `read` |
| `WITH t AS (SELECT …) SELECT … FROM t` | `read` |
| `WITH t AS (DELETE … RETURNING …) SELECT … FROM t` | `write` |
| `INSERT … RETURNING …` | `write` |
| `UPDATE … ; DELETE …` (two statements) | reject (multi) |
| `EXPLAIN SELECT …` | `read` |
| `EXPLAIN ANALYZE UPDATE …` | reject (side-effecting) |
| PG `SELECT … INTO new_t FROM …` | `ddl` |
| MySQL `SELECT … INTO OUTFILE '…'` | reject (file) |
| MySQL `LOAD DATA INFILE …` | reject (file) |
| PG `COPY t TO PROGRAM '…'` | reject (program) |
| SQLite `ATTACH DATABASE '…' AS x` | reject (attach) |
| SQLite `PRAGMA journal_mode=WAL` (writable) | reject (session mutation) |
| `BEGIN; …` | reject (txn control) |
| trailing-only `;` after one statement | as the single statement's class |
| empty / comment-only input | reject |

These fixtures live as a shared table-driven test suite; each engine's adapter runs the same
matrix. A statement that is not explicitly expected in the suite defaults to **reject**.

## Consequences
- The fixture matrix is the contract the three per-dialect parsers (ADR-0001) must pass; it is
  part of the cross-engine contract test suite.
- `read` PRAGMA / `SHOW`-like introspection that varies per engine is added to the allow-list
  only with an explicit fixture, never by default.
- Open item: confirm each engine's parser exposes enough structure to detect data-modifying
  CTEs and `… INTO …` targets; if any cannot, those forms stay in **reject** until proven.
