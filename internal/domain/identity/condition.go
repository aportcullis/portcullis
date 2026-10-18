package identity

import (
	"errors"
	"strings"
)

// Versioned condition environments define the supported vocabulary and semantics.
const (
	ConditionLanguageCELV1 = "cel-v1"
	ConditionEnvironmentV1 = "portcullis-v1"
)

// ErrInvalidCondition means condition data is incomplete or has an unsupported contract version.
var ErrInvalidCondition = errors.New("identity: invalid condition")

// Condition is binding-owned source data; it contains no executable function or compiled SDK object.
type Condition struct {
	Title              string
	Description        string
	Expression         string
	LanguageVersion    string
	EnvironmentVersion string
}

// Validate checks the condition data boundary before separate expression preparation.
func (condition Condition) Validate() error {
	if strings.TrimSpace(condition.Title) == "" || hasUnsafeDisplayRune(condition.Title) || strings.TrimSpace(condition.Expression) == "" ||
		condition.LanguageVersion != ConditionLanguageCELV1 || condition.EnvironmentVersion != ConditionEnvironmentV1 {
		return ErrInvalidCondition
	}
	return nil
}
