package celcondition

import (
	"errors"
	"regexp"
	"strconv"
	"time"

	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/types"
)

// timestampShape accepts explicit RFC3339 offsets with at most nanosecond precision.
var timestampShape = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?(Z|[+-][0-9]{2}:[0-9]{2})$`)

// parseTimestampLiteral resolves constants against UTC without consulting the host's local zone.
func parseTimestampLiteral(literal string) (time.Time, error) {
	if !timestampShape.MatchString(literal) {
		return time.Time{}, errors.New("invalid timestamp literal")
	}
	if literal[len(literal)-1] != 'Z' {
		offset := literal[len(literal)-6:]
		hours, _ := strconv.Atoi(offset[1:3])
		minutes, _ := strconv.Atoi(offset[4:6])
		if hours > 23 || minutes > 59 {
			return time.Time{}, errors.New("invalid timestamp offset")
		}
	}
	instant, err := time.ParseInLocation(time.RFC3339Nano, literal, time.UTC)
	if err != nil {
		return time.Time{}, errors.New("invalid timestamp literal")
	}
	instant = instant.UTC()
	if instant.Year() < 1 || instant.Year() > 9999 {
		return time.Time{}, errors.New("timestamp outside supported range")
	}
	return instant, nil
}

// lowerTimestampLiterals replaces validated calls before the checked artifact becomes immutable.
func lowerTimestampLiterals(expression ast.Expr) {
	if expression.Kind() != ast.CallKind {
		return
	}
	call := expression.AsCall()
	if call.FunctionName() == "timestamp" {
		literal := call.Args()[0].AsLiteral().(types.String)
		instant, _ := parseTimestampLiteral(string(literal))
		expression.SetKindCase(ast.NewExprFactory().NewLiteral(expression.ID(), types.Timestamp{Time: instant}))
		return
	}
	for _, argument := range call.Args() {
		lowerTimestampLiterals(argument)
	}
}
