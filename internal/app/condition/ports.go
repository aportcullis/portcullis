package condition

import "github.com/aportcullis/portcullis/internal/domain/identity"

// CheckedCondition is an opaque prepared artifact whose source is returned by value.
type CheckedCondition interface {
	Source() identity.Condition
}

// Preparer checks source against the restricted environment and explicit preparation limits.
type Preparer interface {
	PrepareCondition(identity.Condition, PreparationLimits) (CheckedCondition, error)
}

// Evaluator performs pure predicate evaluation without context, I/O or clock reads.
type Evaluator interface {
	EvaluateCondition(CheckedCondition, AttributeSnapshot, EvaluationLimits) Result
}
