package connectionpolicy

import (
	"time"
)

// Service implements the connection-policy use cases (ADR-0015). Its
// constructor and methods live in service.go; collaborators are the
// consumer-defined ports (ports.go).
type Service struct {
	repo Repository
	now  func() time.Time
}

// ClassRuleInput is the wire shape of one statement class's gate.
type ClassRuleInput struct {
	Allowed           bool
	RequiredApprovals int
}

// UpdateParams is a FULL replacement of the connection's policy (snapshot
// semantics, like a config replace — no partial patch, so the version pin is
// unambiguous). ExpectedVersion is the version the caller read; a mismatch
// fails with connection.ErrPolicyConflict.
type UpdateParams struct {
	ExpectedVersion     int64
	Read                ClassRuleInput
	Write               ClassRuleInput
	DDL                 ClassRuleInput
	QueryTimeoutSeconds int
	MaxRows             int
	MaxResultBytes      int64
}
