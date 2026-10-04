// Package dialecttest carries the cross-engine dialect contract suite: the ADR-0002 classification fixture matrix as data plus the runner every dialect adapter must pass. New allow-listed forms enter classification only by adding a fixture row here (mirroring the ADR table — keep both in sync).
package dialecttest

import (
	"slices"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// Engine marks fixture applicability (ADR-0002 PG/MY/SQ columns).
type Engine string

const (
	PG     Engine = "postgresql"
	MySQL  Engine = "mysql"
	SQLite Engine = "sqlite"
)

// Expectation is a fixture's required outcome: a class, or a rejection with its reason (parse-level refusals map to RejectEmpty/RejectMultiStatement).
type Expectation struct {
	Class  query.StatementClass
	Reject bool
	Reason query.RejectReason
}

// Fixture is one row of the ADR-0002 matrix. Engines nil means every engine.
type Fixture struct {
	N       int
	SQL     string
	Engines []Engine
	Expect  Expectation
}

// AppliesTo reports whether the fixture runs on the given engine.
func (f Fixture) AppliesTo(engine Engine) bool {
	return f.Engines == nil || slices.Contains(f.Engines, engine)
}

func class(c query.StatementClass) Expectation { return Expectation{Class: c} }
func reject(r query.RejectReason) Expectation  { return Expectation{Reject: true, Reason: r} }

// Fixtures returns a copy of the pinned matrix (docs/adr/0002-statement-classification.md, fixtures #1–#56).
func Fixtures() []Fixture {
	return slices.Clone(fixtures)
}

var fixtures = []Fixture{
	{N: 1, SQL: "SELECT id, v FROM t WHERE id = 1;", Expect: class(query.ClassRead)},
	{N: 2, SQL: "WITH x AS (SELECT id FROM t) SELECT * FROM x", Expect: class(query.ClassRead)},
	{N: 3, SQL: "WITH x AS (DELETE FROM t WHERE id = 1 RETURNING id) SELECT * FROM x", Engines: []Engine{PG}, Expect: class(query.ClassWrite)},
	{N: 4, SQL: "INSERT INTO t (id, v) VALUES (1, 'a') RETURNING id", Engines: []Engine{PG, SQLite}, Expect: class(query.ClassWrite)},
	{N: 5, SQL: "INSERT INTO t (id, v) VALUES (1, 'a')", Expect: class(query.ClassWrite)},
	{N: 6, SQL: "UPDATE t SET v = 'b' WHERE id = 1; DELETE FROM t WHERE id = 2;", Expect: reject(query.RejectMultiStatement)},
	{N: 7, SQL: "EXPLAIN SELECT id FROM t", Engines: []Engine{PG, MySQL}, Expect: class(query.ClassRead)},
	{N: 8, SQL: "EXPLAIN ANALYZE UPDATE t SET v = 'b'", Engines: []Engine{PG}, Expect: reject(query.RejectExplainAnalyze)},
	{N: 9, SQL: "EXPLAIN QUERY PLAN SELECT id FROM t", Engines: []Engine{SQLite}, Expect: class(query.ClassRead)},
	{N: 10, SQL: "SELECT id INTO new_t FROM t", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 11, SQL: "CREATE TABLE new_t AS SELECT id FROM t", Expect: class(query.ClassDDL)},
	{N: 12, SQL: "SELECT id FROM t INTO OUTFILE '/tmp/x'", Engines: []Engine{MySQL}, Expect: reject(query.RejectFileAccess)},
	{N: 13, SQL: "LOAD DATA INFILE '/tmp/x' INTO TABLE t", Engines: []Engine{MySQL}, Expect: reject(query.RejectFileAccess)},
	{N: 14, SQL: "COPY t TO PROGRAM 'cat'", Engines: []Engine{PG}, Expect: reject(query.RejectFileAccess)},
	{N: 15, SQL: "ATTACH DATABASE '/tmp/x.db' AS x", Engines: []Engine{SQLite}, Expect: reject(query.RejectFileAccess)},
	{N: 16, SQL: "PRAGMA journal_mode = WAL", Engines: []Engine{SQLite}, Expect: reject(query.RejectSessionMutation)},
	{N: 17, SQL: "BEGIN", Expect: reject(query.RejectTxnControl)},
	{N: 18, SQL: "SET search_path TO public", Engines: []Engine{PG}, Expect: reject(query.RejectSessionMutation)},
	{N: 19, SQL: "USE mydb", Engines: []Engine{MySQL}, Expect: reject(query.RejectSessionMutation)},
	{N: 20, SQL: "CALL p()", Engines: []Engine{PG, MySQL}, Expect: reject(query.RejectOpaqueCall)},
	{N: 21, SQL: "SELECT id FROM t;", Expect: class(query.ClassRead)},
	{N: 22, SQL: "  -- just a comment", Expect: reject(query.RejectEmpty)},
	{N: 23, SQL: "", Expect: reject(query.RejectEmpty)},
	{N: 24, SQL: "VALUES (1)", Engines: []Engine{PG, SQLite}, Expect: class(query.ClassRead)},
	{N: 25, SQL: "TRUNCATE TABLE t", Engines: []Engine{PG, MySQL}, Expect: class(query.ClassDDL)},
	{N: 26, SQL: "REPLACE INTO t (id, v) VALUES (1, 'a')", Engines: []Engine{MySQL, SQLite}, Expect: class(query.ClassWrite)},
	{N: 27, SQL: "MERGE INTO t USING t s ON t.id = s.id WHEN MATCHED THEN DO NOTHING", Engines: []Engine{PG}, Expect: class(query.ClassWrite)},
	{N: 28, SQL: "SELECT id FROM t FOR UPDATE", Engines: []Engine{PG}, Expect: reject(query.RejectLocking)},
	{N: 29, SQL: "EXPLAIN (ANALYZE) SELECT id FROM t", Engines: []Engine{PG}, Expect: reject(query.RejectExplainAnalyze)},
	// #30/#31: CONCURRENTLY forms cannot run inside a transaction block, and every execution is wrapped in one (PRD §8.2) — reject at classification so an unexecutable statement never reaches the approval flow.
	{N: 30, SQL: "CREATE INDEX CONCURRENTLY idx ON t (v)", Engines: []Engine{PG}, Expect: reject(query.RejectNonTransactional)},
	{N: 31, SQL: "DROP INDEX CONCURRENTLY idx", Engines: []Engine{PG}, Expect: reject(query.RejectNonTransactional)},
	// #32–#37 reject function side effects that READ ONLY does not prevent, including unlisted and qualified names (ADR-0002).
	{N: 32, SQL: "SELECT count(*) FROM t", Expect: class(query.ClassRead)},
	{N: 33, SQL: "SELECT dblink_exec('dbname=x', 'INSERT INTO t VALUES (1)')", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 34, SQL: "SELECT pg_notify('chan', 'payload')", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 35, SQL: "SELECT set_config('work_mem', '1MB', false)", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 36, SQL: "SELECT my_udf(id) FROM t", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 37, SQL: "SELECT pg_catalog.length(v) FROM t", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	// #38–#41 require function-effect checks inside DDL queries, expressions, and defaults (ADR-0002).
	{N: 38, SQL: "CREATE TABLE leaked AS SELECT dblink_exec('dbname=x', 'INSERT INTO t VALUES (1)')", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 39, SQL: "CREATE INDEX i ON t ((dblink_exec('dbname=x', 'INSERT INTO t VALUES (1)')))", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 40, SQL: "ALTER TABLE t ALTER COLUMN v SET DEFAULT pg_notify('chan', 'payload')", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 41, SQL: "CREATE VIEW v AS SELECT pg_notify('chan', 'payload')", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	// #42/#43: plain DDL still classifies as ddl — the sweep must not turn the forms the matrix above allows into false rejections.
	{N: 42, SQL: "CREATE TABLE t (id int)", Expect: class(query.ClassDDL)},
	{N: 43, SQL: "ALTER TABLE t ADD COLUMN c int", Expect: class(query.ClassDDL)},
	// #44–#46: operators are function calls in disguise — CREATE OPERATOR binds an arbitrary function to a symbol — so operator names pass the same allow-list. #44 pins that the everyday vocabulary (concatenation, LIKE, BETWEEN, IN, comparison) stays `read`.
	{N: 44, SQL: "SELECT v || 'x' FROM t WHERE v LIKE 'a%' AND id BETWEEN 1 AND 9 AND id IN (1, 2)", Expect: class(query.ClassRead)},
	{N: 45, SQL: "SELECT a ### b FROM t", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 46, SQL: "SELECT a OPERATOR(public.###) b FROM t", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	// #47–#51 gate ORDER BY USING operators outside A_Expr, including window and aggregate sorts.
	{N: 47, SQL: "SELECT id FROM t ORDER BY id USING ###", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 48, SQL: "SELECT id FROM t ORDER BY id USING OPERATOR(public.###)", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 49, SQL: "SELECT count(*) OVER (ORDER BY id USING ###) FROM t", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 50, SQL: "SELECT array_agg(v ORDER BY v USING ###) FROM t", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 51, SQL: "SELECT id FROM t ORDER BY id USING <", Engines: []Engine{PG}, Expect: class(query.ClassRead)},
	// #52: CREATE TABLE … AS EXECUTE invokes a session-local prepared statement the classifier cannot see — the same opaque-invocation family as CALL/DO — and no such statement can exist on the executor's fresh connection, so an admitted form could be approved yet never succeed (the #30 rule).
	{N: 52, SQL: "CREATE TABLE r AS EXECUTE p", Engines: []Engine{PG}, Expect: reject(query.RejectOpaqueCall)},
	// Schema elements obey the same statement gate as top-level commands.
	{N: 53, SQL: "CREATE SCHEMA s CREATE TABLE t (id int) GRANT SELECT ON t TO PUBLIC", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 54, SQL: "CREATE SCHEMA s CREATE TABLE t (id int) CREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION public.f()", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 55, SQL: "CREATE SCHEMA s CREATE TABLE t (id int) CREATE INDEX CONCURRENTLY i ON t (id)", Engines: []Engine{PG}, Expect: reject(query.RejectNonTransactional)},
	{N: 56, SQL: "CREATE SCHEMA s CREATE TABLE t (id int) CREATE VIEW v AS SELECT id FROM t", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	// #87–#92: a subquery comparison carries its operator in SubLink.OperName, outside A_Expr, so it obeys the same operator gate.
	{N: 87, SQL: "SELECT id FROM t WHERE id ### ANY (SELECT id FROM t)", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 88, SQL: "SELECT id FROM t WHERE id ### ALL (SELECT id FROM t)", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 89, SQL: "SELECT id FROM t WHERE id ### SOME (SELECT id FROM t)", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 90, SQL: "SELECT id FROM t WHERE id OPERATOR(public.=) ANY (SELECT id FROM t)", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 91, SQL: "SELECT id FROM t WHERE id = ANY (SELECT id FROM t)", Engines: []Engine{PG}, Expect: class(query.ClassRead)},
	{N: 92, SQL: "SELECT id FROM t WHERE id IN (SELECT id FROM t)", Engines: []Engine{PG}, Expect: class(query.ClassRead)},
	// #93–#118: RENAME, DROP and COMMENT act only on the object kinds CREATE admits, and ALTER TABLE admits only an explicit subcommand list on a plain table — cluster-level objects, routines, triggers, policies and ownership/security toggles fail closed.
	{N: 93, SQL: "ALTER TABLE t RENAME TO t2", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 94, SQL: "ALTER TABLE t RENAME COLUMN v TO w", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 95, SQL: "ALTER INDEX i RENAME TO j", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 96, SQL: "ALTER TABLE t RENAME CONSTRAINT c TO d", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 97, SQL: "DROP TABLE t", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 98, SQL: "DROP MATERIALIZED VIEW IF EXISTS m, n", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 99, SQL: "COMMENT ON COLUMN t.v IS 'value'", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 100, SQL: "ALTER TABLE t ALTER COLUMN v SET NOT NULL, ALTER COLUMN v DROP DEFAULT, DROP COLUMN c", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 101, SQL: "ALTER TABLE t ADD CONSTRAINT c CHECK (id > 0), ALTER COLUMN v TYPE varchar(10)", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 102, SQL: "ALTER ROLE postgres RENAME TO pwned", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 103, SQL: "ALTER DATABASE postgres RENAME TO x", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 104, SQL: "ALTER FUNCTION public.f(int) RENAME TO lower", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 105, SQL: "ALTER FOREIGN TABLE f RENAME COLUMN a TO b", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 106, SQL: "DROP TRIGGER tr ON t", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 107, SQL: "DROP POLICY p ON t", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 108, SQL: "DROP EVENT TRIGGER e", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 109, SQL: "DROP EXTENSION dblink", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 110, SQL: "COMMENT ON DATABASE postgres IS 'x'", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 111, SQL: "ALTER TABLE t DISABLE TRIGGER ALL", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 112, SQL: "ALTER TABLE t OWNER TO attacker", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 113, SQL: "ALTER TABLE t DISABLE RULE r", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 114, SQL: "ALTER TABLE t REPLICA IDENTITY FULL", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 115, SQL: "ALTER TABLE t SET ACCESS METHOD heap", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 116, SQL: "ALTER TABLE t NO FORCE ROW LEVEL SECURITY", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 117, SQL: "ALTER TABLE t ADD COLUMN c int, DISABLE ROW LEVEL SECURITY", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 118, SQL: "ALTER TYPE ty ADD ATTRIBUTE a int", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	// #119–#132: exclusion-constraint operators obey the operator gate, explicit operator classes are refused, and access methods are limited to the built-in index methods and the heap table method — each names catalog code the statement would run.
	{N: 119, SQL: "CREATE INDEX i ON t USING hash (v)", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 120, SQL: "CREATE TABLE r (p int, EXCLUDE (p WITH =))", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 121, SQL: "CREATE TABLE h (v int) USING heap", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 122, SQL: "CREATE TABLE p (v int) PARTITION BY RANGE (v)", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 123, SQL: "ALTER TABLE t ADD CONSTRAINT x EXCLUDE USING gist (v WITH &&)", Engines: []Engine{PG}, Expect: class(query.ClassDDL)},
	{N: 124, SQL: "CREATE TABLE r (p int, EXCLUDE USING gist (p WITH OPERATOR(evil.&&)))", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 125, SQL: "ALTER TABLE t ADD CONSTRAINT x EXCLUDE (v WITH ###)", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 126, SQL: "CREATE INDEX i ON t (v evil_ops)", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 127, SQL: "CREATE INDEX i ON t USING evil_am (v)", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 128, SQL: "CREATE TABLE p (v int) PARTITION BY RANGE (v evil_ops)", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 129, SQL: "CREATE TABLE h (v int) USING evil_tam", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 130, SQL: "CREATE TABLE r (p int, EXCLUDE USING evil_am (p WITH =))", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 131, SQL: "CREATE TABLE c USING evil_tam AS SELECT id FROM t", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
	{N: 132, SQL: "CREATE INDEX i ON t (v text_pattern_ops)", Engines: []Engine{PG}, Expect: reject(query.RejectNotAllowlisted)},
}
