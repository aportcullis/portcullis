// Package query holds the engine-agnostic vocabulary for governed SQL
// execution: statement classification (ADR-0002), typed parameters (PRD §4.2),
// redaction output (PRD §8.4, ADR-0016), and the result-stream contract
// (ADR-0005). Dialect adapters in infra produce these values; upper slices
// (access requests, execution, audit) consume them.
package query

// StatementClass is the privilege class of a single SQL statement (ADR-0002).
// The class of a statement is the highest-privilege effect anywhere in it.
type StatementClass string

const (
	ClassRead  StatementClass = "read"
	ClassWrite StatementClass = "write"
	ClassDDL   StatementClass = "ddl"
)

// Valid reports whether c is one of the three fixed classes.
func (c StatementClass) Valid() bool {
	switch c {
	case ClassRead, ClassWrite, ClassDDL:
		return true
	}
	return false
}

// Statement is an opaque handle to one successfully parsed SQL statement.
// A dialect's ParseSingle produces it and the same dialect's Classify consumes
// it; passing a Statement from another dialect fails closed with a Rejection.
type Statement interface {
	// Text returns the exact single-statement SQL the handle was parsed from.
	Text() string
}

// RejectReason says why a statement was refused regardless of policy
// (ADR-0002 "always reject" and the fail-closed default).
type RejectReason string

const (
	RejectEmpty           RejectReason = "empty"
	RejectMultiStatement  RejectReason = "multi_statement"
	RejectTxnControl      RejectReason = "txn_control"
	RejectSessionMutation RejectReason = "session_mutation"
	RejectFileAccess      RejectReason = "file_access"
	RejectExplainAnalyze  RejectReason = "explain_analyze"
	RejectOpaqueCall      RejectReason = "opaque_call"
	RejectLocking         RejectReason = "locking"
	// RejectNonTransactional marks a statement PostgreSQL forbids inside a
	// transaction block (CREATE/DROP INDEX CONCURRENTLY): every execution is
	// wrapped in one (PRD §8.2), so it could never succeed.
	RejectNonTransactional RejectReason = "non_transactional"
	RejectNotAllowlisted   RejectReason = "not_allowlisted"
)
