package identity

import (
	"errors"
	"time"
)

// PasswordSetupValidity is how long a one-time password setup link stays usable (PRD §8.3, ADR-0053).
const PasswordSetupValidity = 24 * time.Hour

// ErrPasswordSetupInvalid is the one refusal for an unknown, expired, revoked or already consumed setup token, or one whose user is disabled.
var ErrPasswordSetupInvalid = errors.New("identity: password setup link is invalid or expired")

// ErrPasswordAlreadySet means a setup link was requested for a user that already has a password.
var ErrPasswordAlreadySet = errors.New("identity: user already has a password")

// PasswordSetupIssue is a new setup link to persist: only the token digest is stored, and the validity is measured on the database clock.
type PasswordSetupIssue struct {
	TokenHash []byte
	Validity  time.Duration
}

// PasswordSetup is an open setup link resolved from its token digest.
type PasswordSetup struct {
	UserID         UserID
	OrganizationID OrganizationID
	ExpiresAt      time.Time
}
