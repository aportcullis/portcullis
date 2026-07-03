package auth

import "time"

// Credential policy for bootstrap and password changes.
const (
	// minPasswordLength is the floor for a new password in Unicode code points.
	// NIST SP 800-63B-4 sets 15 as the minimum when a password is the sole
	// authenticator (as local login is here), above its 8-character floor.
	minPasswordLength = 15
	// maxPasswordLength bounds the Argon2 input so an oversized password can't be
	// used to amplify hashing cost.
	maxPasswordLength = 1024
	// maxEmailLength is the RFC 5321 maximum for a forward path (address).
	maxEmailLength = 254
)

// auditWriteTimeout bounds a best-effort audit write after it is detached from
// the request context (ADR-0009), so a stuck store can't leak goroutines.
const auditWriteTimeout = 5 * time.Second
