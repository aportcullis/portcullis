// Package dialecttest carries the cross-engine dialect contract suite: the
// ADR-0002 classification fixture matrix as data plus the runner every
// dialect adapter must pass. New allow-listed forms enter classification only
// by adding a fixture row here (mirroring the ADR table — keep both in sync).
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

// Expectation is a fixture's required outcome: a class, or a rejection with
// its reason (parse-level refusals map to RejectEmpty/RejectMultiStatement).
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

// Fixtures returns a copy of the pinned matrix
// (docs/adr/0002-statement-classification.md, fixtures #1–#29).
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
	// #30/#31: CONCURRENTLY forms cannot run inside a transaction block, and
	// every execution is wrapped in one (PRD §8.2) — reject at classification
	// so an unexecutable statement never reaches the approval flow.
	{N: 30, SQL: "CREATE INDEX CONCURRENTLY idx ON t (v)", Engines: []Engine{PG}, Expect: reject(query.RejectNonTransactional)},
	{N: 31, SQL: "DROP INDEX CONCURRENTLY idx", Engines: []Engine{PG}, Expect: reject(query.RejectNonTransactional)},
}
