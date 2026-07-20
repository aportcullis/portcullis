package setting

import "errors"

// Sentinel errors of the settings vocabulary. Setting values are operational
// numbers, never secrets (ADR-0017), so error texts may echo them.
var (
	// ErrUnknownKey reports a key outside the Tier-C catalog.
	ErrUnknownKey = errors.New("unknown setting key")
	// ErrInvalidValue reports a value that fails its descriptor's validation.
	ErrInvalidValue = errors.New("invalid setting value")
)
