package connection

import (
	"log/slog"
	"time"
)

// Service implements the connection-management use cases. Its constructor
// (New) and methods live in service.go; the type definition lives here with
// the package's other types (file-split convention). Collaborators are the
// consumer-defined ports (ports.go).
type Service struct {
	repo    Repository
	tester  Tester
	codec   CredentialCodec
	auditor AuditRecorder
	logger  *slog.Logger
	now     func() time.Time
	// newID mints the connection UUID before sealing — the envelope's AAD binds
	// the record id (ADR-0003/0014) — so it is injected for deterministic tests.
	newID func() string
}

// ConfigInput is the write model: the only shape credentials travel inbound.
// It is validated into domain value objects before anything is dialed or
// persisted, and it is never stored or logged as-is.
type ConfigInput struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
	// TLSMode is one of the accepted modes; empty selects the
	// certificate-verifying default (ADR-0014).
	TLSMode string
}

// CreateParams registers a new connection.
type CreateParams struct {
	DisplayName string
	Config      ConfigInput
}

// UpdateParams edits a connection. A nil Config is a rename-only update (no
// connection test); a non-nil Config replaces the full target + credential and
// re-runs the test (ADR-0014 — there is no partial credential edit). An empty
// DisplayName keeps the current name when Config is set.
type UpdateParams struct {
	DisplayName string
	Config      *ConfigInput
}
