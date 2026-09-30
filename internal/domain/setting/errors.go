package setting

import "errors"

// Sentinel errors of the settings vocabulary. Setting values are operational numbers, never secrets (ADR-0017), so error texts may echo them.
var (
	ErrUnknownKey = errors.New("unknown setting key")

	ErrInvalidValue = errors.New("invalid setting value")
)
