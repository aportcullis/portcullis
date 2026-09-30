package connection

import (
	"unicode"
	"unicode/utf8"
)

// Environment labels what a connection points at so the UI can make production unmistakable (badge, stronger warnings). A fixed domain enum like TLSMode — mirrors the connections.environment check constraint.
type Environment string

// Supported environments.
const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentProduction  Environment = "production"
)

// ParseEnvironment maps the wire string to the enum. Empty means the caller omitted it and gets the safe default (development); anything else must match exactly, like ParseTLSMode.
func ParseEnvironment(s string) (Environment, error) {
	switch Environment(s) {
	case "":
		return EnvironmentDevelopment, nil
	case EnvironmentDevelopment:
		return EnvironmentDevelopment, nil
	case EnvironmentProduction:
		return EnvironmentProduction, nil
	}
	return "", ErrInvalidEnvironment
}

// maxDescriptionLength bounds the free-text description in Unicode code points (matches the connections.description check constraint).
const maxDescriptionLength = 500

// ValidateDescription accepts empty or multi-line free text within the length bound. Newlines are legitimate here (unlike display names), but the same spoofing classes are rejected: other control (Cc), format (Cf), and line/paragraph separator (Zl/Zp) characters.
func ValidateDescription(s string) error {
	if utf8.RuneCountInString(s) > maxDescriptionLength {
		return ErrInvalidDescription
	}
	for _, r := range s {
		if r == '\n' {
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return ErrInvalidDescription
		}
	}
	return nil
}
