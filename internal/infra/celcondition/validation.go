package celcondition

import (
	"slices"

	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/operators"
	"cel.dev/cel-go/common/types"

	"github.com/aportcullis/portcullis/internal/app/condition"
)

// treeValidation bounds and validates parsed syntax before CEL type checking.
type treeValidation struct {
	limits     condition.PreparationLimits
	nodes      int
	attributes map[string]bool
}

// validateExpression accepts only the bounded predicate grammar and records required attributes.
func (validation *treeValidation) validateExpression(expression ast.Expr, depth int) condition.ReasonCode {
	if reason := validation.countNode(depth); reason != condition.ReasonNone {
		return reason
	}
	switch expression.Kind() {
	case ast.LiteralKind:
		switch expression.AsLiteral().(type) {
		case types.String, types.Bool:
			return condition.ReasonNone
		}
	case ast.IdentKind:
		if slices.Contains(attributeNames, expression.AsIdent()) {
			validation.attributes[expression.AsIdent()] = true
			return condition.ReasonNone
		}
	case ast.SelectKind:
		name, reason := validation.attributePath(expression.AsSelect(), depth)
		if reason != condition.ReasonNone {
			return reason
		}
		if slices.Contains(attributeNames, name) {
			validation.attributes[name] = true
			return condition.ReasonNone
		}
	case ast.ListKind:
		list := expression.AsList()
		if list.Size() > validation.limits.MaxListEntries {
			return condition.ReasonLimitExceeded
		}
		if len(list.OptionalIndices()) != 0 {
			return condition.ReasonInvalidCondition
		}
		for _, element := range list.Elements() {
			if element.Kind() != ast.LiteralKind {
				return condition.ReasonInvalidCondition
			}
			if _, ok := element.AsLiteral().(types.String); !ok {
				return condition.ReasonInvalidCondition
			}
			if reason := validation.validateExpression(element, depth+1); reason != condition.ReasonNone {
				return reason
			}
		}
		return condition.ReasonNone
	case ast.CallKind:
		call := expression.AsCall()
		if !isSupportedCall(call) {
			return condition.ReasonInvalidCondition
		}
		for _, argument := range call.Args() {
			if reason := validation.validateExpression(argument, depth+1); reason != condition.ReasonNone {
				return reason
			}
		}
		return condition.ReasonNone
	}
	return condition.ReasonInvalidCondition
}

// countNode refuses configured node/depth exhaustion before further traversal.
func (validation *treeValidation) countNode(depth int) condition.ReasonCode {
	validation.nodes++
	if validation.nodes > validation.limits.MaxASTNodes || depth > validation.limits.MaxASTDepth {
		return condition.ReasonLimitExceeded
	}
	return condition.ReasonNone
}

// attributePath resolves only static dotted names and counts their operand nodes.
func (validation *treeValidation) attributePath(selection ast.SelectExpr, depth int) (string, condition.ReasonCode) {
	if selection.IsTestOnly() {
		return "", condition.ReasonInvalidCondition
	}
	if reason := validation.countNode(depth + 1); reason != condition.ReasonNone {
		return "", reason
	}
	operand := selection.Operand()
	switch operand.Kind() {
	case ast.IdentKind:
		return operand.AsIdent() + "." + selection.FieldName(), condition.ReasonNone
	case ast.SelectKind:
		prefix, reason := validation.attributePath(operand.AsSelect(), depth+1)
		return prefix + "." + selection.FieldName(), reason
	default:
		return "", condition.ReasonInvalidCondition
	}
}

// isSupportedCall excludes receiver methods, dynamic evaluation and every non-predicate function.
func isSupportedCall(call ast.CallExpr) bool {
	if call.IsMemberFunction() {
		return false
	}
	arguments := call.Args()
	switch call.FunctionName() {
	case operators.LogicalAnd, operators.LogicalOr:
		return len(arguments) >= 2
	case operators.LogicalNot:
		return len(arguments) == 1
	case operators.Equals, operators.NotEquals, operators.Less, operators.LessEquals, operators.Greater, operators.GreaterEquals:
		return len(arguments) == 2
	case operators.In:
		return len(arguments) == 2 && arguments[1].Kind() == ast.ListKind
	case "timestamp":
		if len(arguments) != 1 || arguments[0].Kind() != ast.LiteralKind {
			return false
		}
		literal, ok := arguments[0].AsLiteral().(types.String)
		if !ok {
			return false
		}
		_, err := parseTimestampLiteral(string(literal))
		return err == nil
	default:
		return false
	}
}

// validateCheckedTypes restricts operators beyond CEL's broader standard overloads.
func validateCheckedTypes(checked *ast.AST, expression ast.Expr) bool {
	if expression.Kind() != ast.CallKind {
		return true
	}
	call := expression.AsCall()
	arguments := call.Args()
	switch call.FunctionName() {
	case operators.Equals, operators.NotEquals:
		argumentType := checked.GetType(arguments[0].ID())
		if !argumentType.IsExactType(types.StringType) && !argumentType.IsExactType(types.BoolType) && !argumentType.IsExactType(types.TimestampType) {
			return false
		}
		if !argumentType.IsExactType(checked.GetType(arguments[1].ID())) {
			return false
		}
	case operators.Less, operators.LessEquals, operators.Greater, operators.GreaterEquals:
		if !checked.GetType(arguments[0].ID()).IsExactType(types.TimestampType) || !checked.GetType(arguments[1].ID()).IsExactType(types.TimestampType) {
			return false
		}
	case operators.In:
		if !checked.GetType(arguments[0].ID()).IsExactType(types.StringType) {
			return false
		}
	}
	for _, argument := range arguments {
		if !validateCheckedTypes(checked, argument) {
			return false
		}
	}
	return true
}
