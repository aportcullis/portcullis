# ADR-0002: Statement classification table & test fixtures

- **Status:** Accepted — classification table and fixtures fixed.
  (Amended 2026-07-04: parsers are settled in ADR-0001, fixtures are now literal SQL, and the CTE-DML / `SELECT … INTO` structural question is closed. Amended 2026-07-19: PG reject fixtures #28/#29 added with the PG adapter implementation.)
- **Date:** 2026-06-27 (amended 2026-07-04, 2026-07-19)

## Context
Every approved statement is classified as `read`, `write`, or `ddl` before execution, and the class is matched against the per-connection policy.
The classifier is a **safety gate**: a misclassification can let a write reach a read-only connection.
ADR-0001 fixes the parsers and an **allow-list + fail-closed** model.
This ADR pins the concrete classification table and the fixtures all three engines must satisfy, so behavior is identical and testable across PostgreSQL, MySQL, and SQLite.

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
- **Transaction control** — `BEGIN`/`START TRANSACTION`/`COMMIT`/`ROLLBACK`/`SAVEPOINT`.
  (The server owns the transaction.)
- **Session/engine mutation** — `SET`, `RESET`, MySQL `USE`, writable `PRAGMA`.
- **File/network/utility** — `COPY … (FROM|TO|PROGRAM)`, `SELECT … INTO OUTFILE/DUMPFILE`, `LOAD DATA`, `ATTACH`/`DETACH`.
- **Side-effecting introspection** — `EXPLAIN ANALYZE` (executes the statement).
- **Opaque effect** — `CALL`, `DO`, stored-procedure invocation, and any vendor-specific command not on the allow-list.
- **Function calls outside the function allow-list** (added 2026-07-24) — a statement referencing any function not on the per-dialect allow-list is rejected, including every user-defined function and every schema-qualified name.
  See "Function effects" below.
- **Transaction-incompatible DDL** — PG `CREATE INDEX CONCURRENTLY` / `DROP INDEX CONCURRENTLY` (added 2026-07-20): every execution is wrapped in a transaction (PRD §8.2), which PostgreSQL forbids for these forms, so an admitted statement could never succeed — reject at classification, not at runtime.
- **Unparsed / ambiguous** — anything the parser cannot fully resolve.

### Nested DDL statements (2026-09-30)
`CREATE SCHEMA` may contain `CREATE TABLE`, `CREATE VIEW`, `CREATE INDEX`, `CREATE SEQUENCE`, `CREATE TRIGGER`, and `GRANT` as schema elements.
Each element passes the same statement classifier as a top-level command before the whole schema is admitted as `ddl`.
In particular, wrapping `GRANT`, a trigger call, or `CREATE INDEX CONCURRENTLY` cannot waive its normal rejection.
The full-tree function/operator sweep still checks expressions in every admitted element.

| # | Literal input | Engines | Expected |
|---|---|---|---|
| 53 | `CREATE SCHEMA s CREATE TABLE t (id int) GRANT SELECT ON t TO PUBLIC` | PG | reject (not allowlisted) |
| 54 | `CREATE SCHEMA s CREATE TABLE t (id int) CREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION public.f()` | PG | reject (not allowlisted) |
| 55 | `CREATE SCHEMA s CREATE TABLE t (id int) CREATE INDEX CONCURRENTLY i ON t (id)` | PG | reject (non-transactional) |
| 56 | `CREATE SCHEMA s CREATE TABLE t (id int) CREATE VIEW v AS SELECT id FROM t` | PG | `ddl` |

### DDL query-body validation (2026-10-03)

PostgreSQL `CREATE TABLE AS`, materialized-view creation through that node, and `CREATE VIEW` require their query body to pass the complete read-expression vocabulary, including unknown-node and locking checks. The independent whole-tree function/operator sweep still applies afterward. `SELECT INTO` is DDL only when it contains no data-modifying CTE or row locking.

For M1, refuse a single statement combining object creation with nested DML instead of silently selecting DDL alone. Read/write/DDL have independently configured permission/quorum, so one class cannot represent both requirements safely. Separate the changes into independently approved requests until an explicit multi-effect policy is designed. This does not change M5 migration classification across separate statements.

Regression scenarios reject CTAS with a DELETE CTE, SELECT INTO with an UPDATE CTE, views with a DELETE CTE, CTAS with FOR UPDATE, and CTAS/views with XMLPARSE (an expression outside the admitted vocabulary). Ordinary read-only CTAS/CTEs, views and SELECT INTO remain DDL. These are classification boundary tests; acceptance by the parser does not prove that PostgreSQL permits every rejected form to execute.

Sources checked 2026-10-03: [PostgreSQL data-modifying CTEs](https://www.postgresql.org/docs/current/queries-with.html#QUERIES-WITH-MODIFYING), [CREATE TABLE AS](https://www.postgresql.org/docs/current/sql-createtableas.html).

### Pinned edge-case fixtures (literal; must hold on every engine where the syntax exists)
The suite assumes a table `t(id integer, v text)`.
`PG`/`MY`/`SQ` mark engine applicability; a fixture without a mark runs on all three.

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
| 28 | `SELECT id FROM t FOR UPDATE` | PG | reject (locking — takes row locks; fails in a read-only txn, not allow-listed) |
| 29 | `EXPLAIN (ANALYZE) SELECT id FROM t` | PG | reject (side-effecting — parenthesized option form of #8) |
| 30 | `CREATE INDEX CONCURRENTLY idx ON t (v)` | PG | reject (non-transactional — cannot run inside the transaction every execution gets) |
| 31 | `DROP INDEX CONCURRENTLY idx` | PG | reject (non-transactional — same rule as #30) |

These fixtures live as a shared table-driven test suite; each engine's adapter runs its rows of the same matrix.
A statement not explicitly expected in the suite defaults to **reject** — new allow-listed forms enter only with a new fixture row here.

### Function effects (revised 2026-07-24 — the earlier premise was wrong)
The original text here said a `SELECT` that merely *calls functions* stays `read` because "volatile functions are backstopped at execution time by the server-enforced read-only transaction". **That premise is false.** PostgreSQL's `READ ONLY` mode is explicitly *"a high-level notion of read-only that does not prevent all writes to disk"*; it disallows a fixed list of **commands** (`INSERT`/`UPDATE`/`DELETE`/`MERGE`/`COPY FROM` to non-temp tables, all `CREATE`/`ALTER`/`DROP`, `COMMENT`, `GRANT`, `REVOKE`, `TRUNCATE`, and `EXPLAIN ANALYZE`/`EXECUTE` of those) — not function side effects.
So `SELECT dblink_exec('…','insert …')`, `SELECT pg_notify(…)`, `SELECT set_config(…)`, advisory-lock and server-file/admin functions all pass a read-only transaction, and an approved **read** could write (PRD §4.3/§8.2 promise the class gate is real).

**Revised again 2026-07-25 (external review round 4).** The two-layer text below previously named the executor's `provolatile` check as the second *boundary*.
It is not one, and the first layer was applied to the wrong scope.
Both are corrected here.

1. **Classification-time name allow-list — a coarse pre-filter over EVERY class (fail-closed, this slice).** Each dialect carries an explicit allow-list of pure/standard builtins.
   Any `FuncCall` whose name is not on it — every user-defined function, every unknown builtin, and every **schema-qualified** name (`public.f`, `pg_catalog.f`) — is a `Rejection`, exactly like an unknown statement node.
   The same rule covers **operators**: `A_Expr` names are checked against an operator allow-list, because `CREATE OPERATOR` binds an arbitrary function to a symbol, so an unchecked operator is an unchecked function call.
   This sweep is **class-independent and runs before the class is decided**.
   Earlier text justified skipping DDL subtrees with "`ddl` is already the highest class, so nothing inside can escalate it" — true of *escalation*, false of *effects*: it silently exempted `CREATE TABLE t AS SELECT dblink_exec(…)`, `CREATE INDEX i ON t ((dblink_exec(…)))`, and `ALTER TABLE … SET DEFAULT pg_notify(…)` from the allow-list.
   A `ddl` class is a statement of privilege, never a waiver of the effect check.
2. **The real boundary is the target database account (PRD §8.1).** What ultimately bounds an approved statement is what its login role is *allowed to do* on the target: no `EXECUTE` on `dblink_exec`/admin functions, no `CREATE` in schemas it should not write, no superuser.
   Portcullis's parser is a governance filter on *intent*; the database's own privilege system is the enforcement.
3. **Execution-time catalog identity verification (implemented M1, ADR-0021).** Resolve explicitly referenced names to visible candidate **OIDs** under pinned `search_path=pg_catalog,public` and require every candidate to be a bootstrap built-in in pg_catalog.
   This conservative proof refuses any untrusted overload, operator implementation, or explicit type, even where argument resolution would choose a trusted candidate.
   It does not claim to expose the exact selected OID: PostgreSQL's wire protocol does not return analyzed expression identities.
   Catalog changes and indirectly invoked behavior remain subject to trusted target administration and least target privilege.
   Name equality is not identity: PostgreSQL resolves calls by *argument types* and `search_path` order, so `lower(some_custom_type)` can bind a user-defined overload whose name is on the list.

**`provolatile` is NOT part of that boundary.** PostgreSQL states the volatility category is *"a promise to the optimizer about the behavior of the function"*, and that filtering on it is *"not a completely bulletproof test, since such functions could still call `VOLATILE` functions that modify the database"*.
The server never enforces the declaration, so anyone who can create a function can declare a side-effecting body `STABLE`.
Checking `provolatile` remains worthwhile as a **hygiene check** against honestly-declared volatile builtins slipping into a `read`; it must never be described as a control.

**Residual risk the classifier cannot close** (deliberately deferred to layers 2–3): overload resolution by argument type, user-defined casts (`TypeCast` to a user type invokes a cast function) and user-defined aggregates, and functions reached *indirectly* — an allow-listed function whose own body calls something else.
A name-based filter is blind to all three because they are decided by the catalog, not by the text.

### CTE-DML and `… INTO` detectability (closed 2026-07-04)
The former open item — "can each parser expose data-modifying CTEs and `INTO` targets?" — is resolved by the ADR-0001 picks:
- **PostgreSQL** (`pgplex/pgparser`): the AST is the PG parse tree, so CTE bodies are `CommonTableExpr` nodes (walk each for DML) and `SELECT … INTO` carries an `IntoClause` — both structurally visible, exactly as PostgreSQL itself classifies them.
- **MySQL** (tidb parser): `WITH` clauses hang off the DML/SELECT AST nodes and `SELECT … INTO OUTFILE/DUMPFILE` is an explicit AST field; both are walkable.
- **SQLite** (engine authorizer): classification is not tree-walking at all — the engine reports the *effects* (action codes) of the fully resolved statement, so a write hidden in a CTE surfaces as `SQLITE_INSERT/UPDATE/DELETE` regardless of nesting.
  The fail-closed rule stands regardless: if an adapter cannot prove a form's class from the structures above, that form is rejected — never approximated.

## Consequences
- The fixture matrix is the contract the three per-dialect parsers (ADR-0001) must pass; it is part of the cross-engine contract test suite and the acceptance gate for any parser bump.
- `read` PRAGMA / `SHOW`-like introspection that varies per engine is added to the allow-list only with an explicit fixture, never by default.
- The function allow-list is a maintenance surface: a legitimate query using an unlisted builtin is rejected until the list (and a fixture) admits it.
  That is the intended direction of failure — a rejected read is a support ticket, an unnoticed write through `dblink_exec` is a governance breach.
- The operator allow-list is the same trade at a higher traffic volume: every comparison, arithmetic, pattern, and JSON operator a real query uses must be listed, and an exotic-but-legitimate one is rejected until a fixture admits it.
- The effect sweep is one pass over the whole tree for every statement, independent of the class walk.
  That is a second traversal of the same nodes — accepted deliberately: fusing it into the class walk is what produced the DDL exemption above.

## Sources (function-effects revision, checked 2026-07-24; boundary revision 2026-07-25)
- PostgreSQL `CREATE SCHEMA` — nested statement vocabulary (checked 2026-09-30): https://www.postgresql.org/docs/current/sql-createschema.html
- PostgreSQL `SET TRANSACTION` — the `READ ONLY` command list and the explicit "high-level notion of read-only that does not prevent all writes to disk": https://www.postgresql.org/docs/current/sql-set-transaction.html
- `dblink_exec` — executes arbitrary commands (including writes) on a remote database from inside a `SELECT`: https://www.postgresql.org/docs/current/contrib-dblink-exec.html
- System administration functions (`pg_notify`, `set_config`, advisory locks, server-file access) — side effects reachable from a plain `SELECT`: https://www.postgresql.org/docs/current/functions-admin.html
- Function volatility categories — the source of "a promise to the optimizer" and "not a completely bulletproof test, since such functions could still call `VOLATILE` functions that modify the database", i.e. why `provolatile` is a hygiene check and not a boundary: https://www.postgresql.org/docs/current/xfunc-volatility.html
- Function/operator resolution by argument type and `search_path` — why the spelling on the allow-list does not identify the function that will actually run: https://www.postgresql.org/docs/current/typeconv-func.html
- `CREATE OPERATOR` — an operator symbol is a user-suppliable binding to an arbitrary function, hence the operator allow-list: https://www.postgresql.org/docs/current/sql-createoperator.html
