package audit

import "errors"

var ErrEventNotFound = errors.New("audit: event not found")

// ErrOrganizationRequired refuses an audit read or write without an explicit organization scope (ADR-0004).
var ErrOrganizationRequired = errors.New("audit: organization is required")

// ErrOrganizationMismatch refuses an event attributed to a different organization than the mutation it records.
var ErrOrganizationMismatch = errors.New("audit: event organization does not match the mutation")
