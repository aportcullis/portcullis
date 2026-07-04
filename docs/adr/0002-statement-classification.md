# ADR-0002: Statement classification table & test fixtures

- **Status:** Accepted — classification table and fixtures fixed. (Amended 2026-07-04:
  parsers are settled in ADR-0001, fixtures are now literal SQL, and the CTE-DML /
  `SELECT … INTO` structural question is closed.)
- **Date:** 2026-06-27 (amended 2026-07-04)

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

### Pinned edge-case fixtures (literal; must hold on every engine where the syntax exists)
The suite assumes a table `t(id integer, v text)`. `PG`/`MY`/`SQ` mark engine applicability;
a fixture without a mark runs on all three.

| # | Literal input | Engines | Expected |
|---|---|---|---|
| 1 | `SELECT id, v FROM t WHERE id = 1;` | all | `read` |
| 2 | `WITH x AS (SELECT id FROM t) SELECT * FROM x` | all | `read` |
| 3 | `WITH x AS (DELETE FROM t WHERE id = 1 RETURNING id) SELECT * FROM x` | PG | `write` |
| 4 | `INSERT INTO t (id, v) VALUES (1, 'a') RETURNING id` | PG, SQ | `write` |
| 5 | `INSERT INTO t (id, v) VALUES (1, 'a')` | all | `write` |
| 6 | `UPDATE t SET v = 'b' WHERE id = 1; DELETE FROM t WHERE id = 2;` | all | reject (multi) |
| 7 | `EXPLAIN SELECT id FROM t` | PG, MY | `read` |
| 8 | `EXPLAIN ANALYZE UPDATE t SET v = 'b'` | PG | reject (side-effecting) |
| 9 | `EXPLAIN QUERY PLAN SELECT id FROM t` | SQ | `read` |
| 10 | `SELECT id INTO new_t FROM t` | PG | `ddl` (creates an object) |
| 11 | `CREATE TABLE new_t AS SELECT id FROM t` | all | `ddl` |
| 12 | `SELECT id FROM t INTO OUTFILE '/tmp/x'` | MY | reject (file) |
| 13 | `LOAD DATA INFILE '/tmp/x' INTO TABLE t` | MY | reject (file) |
| 14 | `COPY t TO PROGRAM 'cat'` | PG | reject (program) |
| 15 | `ATTACH DATABASE '/tmp/x.db' AS x` | SQ | reject (attach) |
| 16 | `PRAGMA journal_mode = WAL` | SQ | reject (session mutation) |
| 17 | `BEGIN` | all | reject (txn control) |
| 18 | `SET search_path TO public` | PG | reject (session mutation) |
| 19 | `USE mydb` | MY | reject (session mutation) |
| 20 | `CALL p()` | PG, MY | reject (opaque effect) |
| 21 | `SELECT id FROM t;` (single trailing `;`) | all | `read` |
| 22 | `  -- just a comment` | all | reject (empty) |
| 23 | `` (empty string) | all | reject (empty) |
| 24 | `VALUES (1)` | PG, SQ | `read` |
| 25 | `TRUNCATE TABLE t` | PG, MY | `ddl` |
| 26 | `REPLACE INTO t (id, v) VALUES (1, 'a')` | MY, SQ | `write` |
| 27 | `MERGE INTO t USING t s ON t.id = s.id WHEN MATCHED THEN DO NOTHING` | PG | `write` |

These fixtures live as a shared table-driven test suite; each engine's adapter runs its rows
of the same matrix. A statement not explicitly expected in the suite defaults to **reject** —
new allow-listed forms enter only with a new fixture row here.

### CTE-DML and `… INTO` detectability (closed 2026-07-04)
The former open item — "can each parser expose data-modifying CTEs and `INTO` targets?" — is
resolved by the ADR-0001 picks:
- **PostgreSQL** (`pgplex/pgparser`): the AST is the PG parse tree, so CTE bodies are
  `CommonTableExpr` nodes (walk each for DML) and `SELECT … INTO` carries an `IntoClause` —
  both structurally visible, exactly as PostgreSQL itself classifies them.
- **MySQL** (tidb parser): `WITH` clauses hang off the DML/SELECT AST nodes and
  `SELECT … INTO OUTFILE/DUMPFILE` is an explicit AST field; both are walkable.
- **SQLite** (engine authorizer): classification is not tree-walking at all — the engine
  reports the *effects* (action codes) of the fully resolved statement, so a write hidden in
  a CTE surfaces as `SQLITE_INSERT/UPDATE/DELETE` regardless of nesting.
The fail-closed rule stands regardless: if an adapter cannot prove a form's class from the
structures above, that form is rejected — never approximated.

## Consequences
- The fixture matrix is the contract the three per-dialect parsers (ADR-0001) must pass; it is
  part of the cross-engine contract test suite and the acceptance gate for any parser bump.
- `read` PRAGMA / `SHOW`-like introspection that varies per engine is added to the allow-list
  only with an explicit fixture, never by default.
