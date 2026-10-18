package celcondition

import (
	"errors"
	"unicode/utf8"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/interpreter"

	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// attributeNames is the entire versioned environment; no functions can resolve additional data.
var attributeNames = []string{
	"principal.id", "principal.organization_id", "resource.type", "resource.id",
	"resource.organization_id", "resource.connection_id", "request.time",
}

// EvaluateCondition evaluates an adapter-owned checked AST using fresh invocation-local state.
func (adapter *Adapter) EvaluateCondition(prepared condition.CheckedCondition, attributes condition.AttributeSnapshot, limits condition.EvaluationLimits) condition.Result {
	if limits.Validate() != nil {
		return condition.Result{Reason: condition.ReasonInvalidLimits}
	}
	checked, ok := prepared.(*checkedCondition)
	if !ok || checked == nil || checked.checked == nil {
		return condition.Result{Reason: condition.ReasonUnsupportedProgram}
	}
	if checked.source.LanguageVersion != identity.ConditionLanguageCELV1 || checked.source.EnvironmentVersion != identity.ConditionEnvironmentV1 {
		return condition.Result{Reason: condition.ReasonUnsupportedVersion}
	}
	activation := make(map[string]any, len(checked.attributes))
	for _, name := range checked.attributes {
		value, reason := attributeValue(name, attributes, limits.MaxAttributeBytes)
		if reason != condition.ReasonNone {
			return condition.Result{Reason: reason}
		}
		activation[name] = value
	}
	// Planning a checked AST neither reparses source nor performs I/O; each call owns its cost tracker.
	program, err := adapter.environment.Program(checked.checked, cel.CostLimit(limits.CostBudget))
	if err != nil {
		return condition.Result{Reason: condition.ReasonEvaluationFailed}
	}
	value, _, err := program.Eval(activation)
	if err != nil {
		var cancelled interpreter.EvalCancelledError
		if errors.As(err, &cancelled) && cancelled.Cause == interpreter.CostLimitExceeded {
			return condition.Result{Reason: condition.ReasonLimitExceeded}
		}
		return condition.Result{Reason: condition.ReasonEvaluationFailed}
	}
	switch value {
	case types.True:
		return condition.Result{Outcome: condition.Match}
	case types.False:
		return condition.Result{Outcome: condition.NoMatch}
	default:
		return condition.Result{Reason: condition.ReasonEvaluationFailed}
	}
}

// attributeValue returns only supplied scalar data and refuses missing or excessive referenced attributes.
func attributeValue(name string, attributes condition.AttributeSnapshot, maxBytes int) (any, condition.ReasonCode) {
	var value string
	switch name {
	case "principal.id":
		value = string(attributes.PrincipalID)
	case "principal.organization_id":
		value = string(attributes.PrincipalOrganizationID)
	case "resource.type":
		value = string(attributes.Resource.Type)
	case "resource.id":
		value = attributes.Resource.ID
	case "resource.organization_id":
		value = string(attributes.Resource.OrganizationID)
	case "resource.connection_id":
		if attributes.Resource.Validate() != nil {
			return nil, condition.ReasonMissingAttribute
		}
		value = attributes.Resource.ConnectionID
	case "request.time":
		observed := attributes.RequestTime.UTC()
		if observed.IsZero() || observed.Year() < 1 || observed.Year() > 9999 {
			return nil, condition.ReasonMissingAttribute
		}
		return observed, condition.ReasonNone
	default:
		return nil, condition.ReasonMissingAttribute
	}
	if value == "" {
		return nil, condition.ReasonMissingAttribute
	}
	if len(value) > maxBytes {
		return nil, condition.ReasonLimitExceeded
	}
	if !utf8.ValidString(value) {
		return nil, condition.ReasonEvaluationFailed
	}
	return value, condition.ReasonNone
}
