package query

import (
	"errors"
	"fmt"
)

// Sentinel errors shared across the query vocabulary. No Error() string in this file may include SQL text or parameter values: both are sensitive until redacted (PRD §8.1, §8.4, ADR-0016). ExecError.Message is the one value-bearing FIELD — requester-facing transport only, never its Error().
var (
	ErrEmptyStatement     = errors.New("input contains no statement")
	ErrMultipleStatements = errors.New("input contains more than one statement")
	ErrUnknownParameter   = errors.New("statement references an unknown parameter")
	ErrUnusedParameter    = errors.New("provided parameter is not referenced by the statement")
	ErrPositionalParams   = errors.New("positional $N parameters are not accepted; use :name")
	ErrInvalidParamName   = errors.New("invalid or duplicate parameter name")
	ErrInvalidParamValue  = errors.New("invalid parameter value")
	ErrResponseLimit      = errors.New("query: target response exceeds local memory limit")
)

// ParseFailure reports that the input did not parse. It carries only a byte offset — parser error messages can quote the input and are never propagated (ADR-0016).
type ParseFailure struct {
	Position int
}

func (e *ParseFailure) Error() string {
	return fmt.Sprintf("sql parse failed at byte offset %d", e.Position)
}

// Rejection reports that a parsed statement is refused regardless of policy (ADR-0002 always-reject list and the fail-closed default).
type Rejection struct {
	Reason RejectReason
}

func (e *Rejection) Error() string {
	return fmt.Sprintf("statement rejected: %s", e.Reason)
}

// ExecError drops row-bearing detail, hint, and context fields. Message may contain input values and is requester-only; Error() and audit expose SQLSTATE alone (ADR-0016).
type ExecError struct {
	SQLState string
	Message  string
	Position int
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("execution failed (SQLSTATE %s)", e.SQLState)
}
