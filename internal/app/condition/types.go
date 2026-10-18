// Package condition prepares condition source and wraps pure evaluation with operational bounds.
package condition

import (
	"time"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// AttributeSnapshot is an immutable-by-value set of trusted scalar inputs for one decision.
type AttributeSnapshot struct {
	PrincipalID             identity.UserID
	PrincipalOrganizationID identity.OrganizationID
	Resource                identity.ResourceReference
	RequestTime             time.Time
}

// PreparationLimits bound source parsing and the accepted expression tree.
type PreparationLimits struct {
	MaxSourceBytes int `mapstructure:"max_source_bytes"`
	MaxASTNodes    int `mapstructure:"max_ast_nodes"`
	MaxASTDepth    int `mapstructure:"max_ast_depth"`
	MaxListEntries int `mapstructure:"max_list_entries"`
}

// EvaluationLimits are explicit deterministic budgets for one pure invocation.
type EvaluationLimits struct {
	CostBudget        uint64 `mapstructure:"cost_budget"`
	MaxAttributeBytes int    `mapstructure:"max_attribute_bytes"`
}

// Outcome distinguishes a true predicate, a false predicate and an evaluation refusal.
type Outcome uint8

const (
	EvaluationError Outcome = iota
	Match
	NoMatch
)

// ReasonCode is safe diagnostic evidence without source, identity or SDK details.
type ReasonCode string

const (
	ReasonNone               ReasonCode = ""
	ReasonInvalidCondition   ReasonCode = "invalid_condition"
	ReasonUnsupportedVersion ReasonCode = "unsupported_version"
	ReasonInvalidLimits      ReasonCode = "invalid_limits"
	ReasonLimitExceeded      ReasonCode = "limit_exceeded"
	ReasonMissingAttribute   ReasonCode = "missing_attribute"
	ReasonUnsupportedProgram ReasonCode = "unsupported_program"
	ReasonEvaluationFailed   ReasonCode = "evaluation_failed"
	ReasonCancelled          ReasonCode = "cancelled"
	ReasonDeadlineExceeded   ReasonCode = "deadline_exceeded"
)

// Result is predicate evidence and never an authorization grant by itself.
type Result struct {
	Outcome Outcome
	Reason  ReasonCode
}

// PreparationError exposes only a stable reason code to callers.
type PreparationError struct{ Reason ReasonCode }

// Error returns a source-free preparation refusal.
func (failure PreparationError) Error() string { return "condition: " + string(failure.Reason) }
