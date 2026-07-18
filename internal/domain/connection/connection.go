package connection

import (
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// DBType is the target database engine. A fixed domain enum the code branches
// on (dialect adapters, fingerprint input), so it lives in code; everything
// configurable comes from the database or config (see conventions/code.md).
type DBType string

// Supported engines — mirrors the connections.db_type check constraint. Only
// PostgreSQL has a dialect adapter in M1; MySQL/SQLite constants arrive with
// their M2 adapters so an unsupported type cannot pass construction.
const DBTypePostgreSQL DBType = "postgresql"

// maxDisplayNameLength bounds the connection display name in Unicode code
// points (matches the account display-name bound in app/auth).
const maxDisplayNameLength = 256

// Connection is a registered target database: the plaintext descriptor of one
// row (ADR-0014 storage split). Its credential travels separately as
// Credential/SealedCredential and is discarded on archive.
type Connection struct {
	ID             ConnectionID
	OrganizationID identity.OrganizationID
	DBType         DBType
	DisplayName    string
	Target         Target
	TLSMode        TLSMode
	// Fingerprint is the versioned target identity that survives archive as the
	// historical snapshot (PRD §4.3); derived, never set by callers.
	Fingerprint string
	CreatedBy   identity.UserID
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// Version is the database-enforced optimistic-lock token. It changes on
	// every mutation; UpdatedAt is display/audit metadata only.
	Version int64
	// ArchivedAt is the soft-delete marker; nil means active.
	ArchivedAt *time.Time
}

// New validates and assembles a connection, deriving the target fingerprint
// and stamping both timestamps from the caller's clock. The id is the
// app-generated UUID (see ConnectionID).
func New(
	id ConnectionID,
	org identity.OrganizationID,
	dbType DBType,
	displayName string,
	target Target,
	tlsMode TLSMode,
	createdBy identity.UserID,
	now time.Time,
) (Connection, error) {
	if id == "" || org == "" || createdBy == "" {
		return Connection{}, ErrInvalidConnection
	}
	if dbType != DBTypePostgreSQL {
		return Connection{}, ErrUnsupportedDBType
	}
	if err := ValidateDisplayName(displayName); err != nil {
		return Connection{}, err
	}
	return Connection{
		ID:             id,
		OrganizationID: org,
		DBType:         dbType,
		DisplayName:    displayName,
		Target:         target,
		TLSMode:        tlsMode,
		Fingerprint:    target.Fingerprint(dbType),
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
		Version:        1,
	}, nil
}

// ValidateDisplayName requires a non-blank name (it names the connection in
// lists and audit snapshots, and is unique per organization) within the length
// bound, and rejects control (Cc), format (Cf), and line/paragraph separator
// (Zl/Zp) characters — same classes as account display names: bidi overrides
// and zero-width joiners spoof rendered names, Zl/Zp inject real line breaks.
func ValidateDisplayName(name string) error {
	trimmed := false
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return ErrInvalidDisplayName
		}
		if !unicode.IsSpace(r) {
			trimmed = true
		}
	}
	if !trimmed || utf8.RuneCountInString(name) > maxDisplayNameLength {
		return ErrInvalidDisplayName
	}
	return nil
}

// IsArchived reports whether the connection was soft-deleted.
func (c Connection) IsArchived() bool { return c.ArchivedAt != nil }

// Archive marks the connection soft-deleted at now. Archiving twice fails
// with ErrAlreadyArchived and leaves the original timestamp untouched — the
// first archive time is audit evidence.
func (c *Connection) Archive(now time.Time) error {
	if c.IsArchived() {
		return ErrAlreadyArchived
	}
	c.ArchivedAt = &now
	return nil
}
