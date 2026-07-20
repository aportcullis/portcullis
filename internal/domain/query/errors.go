package query

import (
	"errors"
	"fmt"
)

// Sentinel errors shared across the query vocabulary. No Error() string in
// this file may include SQL text or parameter values: both are sensitive
// until redacted (PRD §8.1, §8.4, ADR-0016). ExecError.Message is the one
// value-bearing FIELD — requester-facing transport only, never its Error().
var (
	ErrEmptyStatement     = errors.New("input contains no statement")
	ErrMultipleStatements = errors.New("input contains more than one statement")
	ErrUnknownParameter   = errors.New("statement references an unknown parameter")
	ErrUnusedParameter    = errors.New("provided parameter is not referenced by the statement")
	ErrPositionalParams   = errors.New("positional $N parameters are not accepted; use :name")
	ErrInvalidParamName   = errors.New("invalid or duplicate parameter name")
	ErrInvalidParamValue  = errors.New("invalid parameter value")
)

// ParseFailure reports that the input did not parse. It carries only a byte
// offset — parser error messages can quote the input and are never
// propagated (ADR-0016).
type ParseFailure struct {
	Position int
}

func (e *ParseFailure) Error() string {
	return fmt.Sprintf("sql parse failed at byte offset %d", e.Position)
}

// Rejection reports that a parsed statement is refused regardless of policy
// (ADR-0002 always-reject list and the fail-closed default).
type Rejection struct {
	Reason RejectReason
}

func (e *Rejection) Error() string {
	return fmt.Sprintf("statement rejected: %s", e.Reason)
}

// ExecError is the redacted form of a target-database execution error:
// SQLSTATE, the primary message, and the 1-based statement position. Detail,
// hint, and context fields are dropped before construction — they can embed
// row data (ADR-0016). The primary Message itself can quote input values
// (e.g. 22P02 "invalid input syntax for type integer: …"), so it is carried
// as a FIELD for the requester-facing transport only and deliberately kept
// out of Error() — the string a log line would capture. Audit records keep
// SQLState alone.
type ExecError struct {
	SQLState string
	Message  string
	Position int
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("execution failed (SQLSTATE %s)", e.SQLState)
}
