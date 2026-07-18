package audit

import "errors"

// ErrEventNotFound means an event is absent from the caller's organization.
// Keeping the organization scope in the repository query makes this one
// sentinel safe for both nonexistent and cross-organization identifiers.
var ErrEventNotFound = errors.New("audit: event not found")
