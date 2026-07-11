package auth

import (
	"log/slog"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Service implements the auth use cases. Its constructor (New) and methods live
// in service.go; the type definition lives here with the package's other types
// (file-split convention). Collaborators are the consumer-defined ports (ports.go).
type Service struct {
	repo      Repository
	hasher    PasswordHasher
	csrf      CSRFProtector
	auditor   AuditRecorder
	oidc      OIDCProvider // nil unless Google login is configured (ADR-0007)
	logger    *slog.Logger
	now       func() time.Time
	idle      time.Duration
	absolute  time.Duration
	idleRenew time.Duration
	dummyHash string // verified for unknown accounts to equalize login timing
}

// Config tunes session lifetimes. The password-hash profile lives in the
// injected PasswordHasher, not here.
type Config struct {
	Idle     time.Duration
	Absolute time.Duration
	// IdleRenewInterval throttles idle-window writes: the session is only
	// re-persisted once activity has advanced the window by at least this much,
	// so high-traffic sessions don't write on every request.
	IdleRenewInterval time.Duration
}

// Session is the result of a successful login: the persisted session plus the
// raw token (for the session cookie) and the CSRF token (for the readable cookie).
type Session struct {
	User    identity.User
	Session identity.Session
	Token   string
	CSRF    string
}

// OIDCPending is the state minted at the start of a Google login and carried
// across the provider redirect (in an AEAD-sealed cookie the transport owns —
// ADR-0007). ExpiresAt is enforced by the service against its own clock, so the
// client-controlled cookie Max-Age is never the only expiry check.
type OIDCPending struct {
	State     string
	Nonce     string
	Verifier  string
	ExpiresAt time.Time
}
