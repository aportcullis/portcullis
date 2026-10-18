// Package celcondition implements the restricted condition ports with CEL.
package celcondition

import (
	"cel.dev/cel-go/cel"

	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Adapter prepares checked conditions and evaluates supplied immutable inputs.
type Adapter struct{ environment *cel.Env }

// checkedCondition keeps the checked AST private and returns only a copy of source data.
type checkedCondition struct {
	source     identity.Condition
	checked    *cel.Ast
	attributes []string
}

// Source returns persisted source identity without exposing mutable SDK state.
func (checked *checkedCondition) Source() identity.Condition { return checked.source }

// New constructs the fixed language environment without external functions or lookups.
func New() (*Adapter, error) {
	options := make([]cel.EnvOption, 0, 5+len(attributeNames))
	options = append(options,
		cel.ClearMacros(), cel.ParserExpressionSizeLimit(condition.MaxSourceBytes),
		cel.ParserRecursionLimit(condition.MaxASTDepth), cel.ExpressionNodeLimit(condition.MaxASTNodes),
		cel.ParserErrorRecoveryLimit(8),
	)
	for _, name := range attributeNames {
		attributeType := cel.StringType
		if name == "request.time" {
			attributeType = cel.TimestampType
		}
		options = append(options, cel.Variable(name, attributeType))
	}
	environment, err := cel.NewEnv(options...)
	if err != nil {
		return nil, condition.PreparationError{Reason: condition.ReasonUnsupportedVersion}
	}
	return &Adapter{environment: environment}, nil
}

// PrepareCondition validates source before producing an immutable checked artifact.
func (adapter *Adapter) PrepareCondition(source identity.Condition, limits condition.PreparationLimits) (condition.CheckedCondition, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if source.LanguageVersion != identity.ConditionLanguageCELV1 || source.EnvironmentVersion != identity.ConditionEnvironmentV1 {
		return nil, condition.PreparationError{Reason: condition.ReasonUnsupportedVersion}
	}
	if len(source.Expression) > limits.MaxSourceBytes {
		return nil, condition.PreparationError{Reason: condition.ReasonLimitExceeded}
	}
	if source.Validate() != nil {
		return nil, condition.PreparationError{Reason: condition.ReasonInvalidCondition}
	}
	parsed, issues := adapter.environment.Parse(source.Expression)
	if issues != nil && issues.Err() != nil {
		return nil, condition.PreparationError{Reason: condition.ReasonInvalidCondition}
	}
	validation := treeValidation{limits: limits, attributes: make(map[string]bool)}
	if reason := validation.validateExpression(parsed.NativeRep().Expr(), 1); reason != condition.ReasonNone {
		return nil, condition.PreparationError{Reason: reason}
	}
	checked, issues := adapter.environment.Check(parsed)
	if issues != nil && issues.Err() != nil || checked == nil || !checked.OutputType().IsExactType(cel.BoolType) || !validateCheckedTypes(checked.NativeRep(), checked.NativeRep().Expr()) {
		return nil, condition.PreparationError{Reason: condition.ReasonInvalidCondition}
	}
	lowerTimestampLiterals(checked.NativeRep().Expr())
	required := make([]string, 0, len(validation.attributes))
	for _, name := range attributeNames {
		if validation.attributes[name] {
			required = append(required, name)
		}
	}
	return &checkedCondition{source: source, checked: checked, attributes: required}, nil
}
