package query

import (
	"fmt"
	"strconv"
	"time"
)

// ParamType is the declared type of a named parameter value (PRD §4.2).
// Values always travel as text; the type governs validation here and bind
// encoding in the dialect adapter.
type ParamType string

const (
	ParamString    ParamType = "string"
	ParamInteger   ParamType = "integer"
	ParamDecimal   ParamType = "decimal"
	ParamBoolean   ParamType = "boolean"
	ParamDate      ParamType = "date"
	ParamTimestamp ParamType = "timestamp"
	ParamUUID      ParamType = "uuid"
	ParamNull      ParamType = "null"
)

// TypedValue is one parameter value in its textual form.
type TypedValue struct {
	Type ParamType
	Text string
}

// Validate checks that Text is a well-formed value of Type. Error messages
// never echo Text: parameter values are sensitive before encryption
// (PRD §8.1).
func (v TypedValue) Validate() error {
	ok := false
	switch v.Type {
	case ParamString:
		ok = true
	case ParamInteger:
		_, err := strconv.ParseInt(v.Text, 10, 64)
		ok = err == nil
	case ParamDecimal:
		ok = validDecimal(v.Text)
	case ParamBoolean:
		ok = v.Text == "true" || v.Text == "false"
	case ParamDate:
		_, err := time.Parse(time.DateOnly, v.Text)
		ok = err == nil
	case ParamTimestamp:
		_, err := time.Parse(time.RFC3339, v.Text)
		ok = err == nil
	case ParamUUID:
		ok = validUUID(v.Text)
	case ParamNull:
		ok = v.Text == ""
	default:
		return fmt.Errorf("%w: unknown parameter type %q", ErrInvalidParamValue, v.Type)
	}
	if !ok {
		return fmt.Errorf("%w: not a valid %s", ErrInvalidParamValue, v.Type)
	}
	return nil
}

// Parameter is a named, typed parameter value supplied for binding.
type Parameter struct {
	Name  string
	Value TypedValue
}

// validDecimal accepts plain decimal notation with an optional exponent:
// [+-] digits [. digits] [eE [+-] digits], or a leading-dot form. It rejects
// NaN/Inf, separators, and anything strconv would accept beyond that.
func validDecimal(s string) bool {
	i, n := 0, len(s)
	if i < n && (s[i] == '+' || s[i] == '-') {
		i++
	}
	intDigits := 0
	for i < n && s[i] >= '0' && s[i] <= '9' {
		i++
		intDigits++
	}
	fracDigits := 0
	if i < n && s[i] == '.' {
		i++
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
			fracDigits++
		}
	}
	if intDigits+fracDigits == 0 {
		return false
	}
	if i < n && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < n && (s[i] == '+' || s[i] == '-') {
			i++
		}
		expDigits := 0
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
			expDigits++
		}
		if expDigits == 0 {
			return false
		}
	}
	return i == n
}

// validUUID accepts only the canonical hyphenated 8-4-4-4-12 form, hex in
// either case.
func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := range 36 {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			c := s[i]
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}
