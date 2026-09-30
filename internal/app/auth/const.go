package auth

import "time"

// Credential policy for bootstrap and password changes.
const (
	// minPasswordLength is the floor for a new password in Unicode code points. NIST SP 800-63B-4 sets 15 as the minimum when a password is the sole authenticator (as local login is here), above its 8-character floor.
	minPasswordLength = 15
	// maxPasswordLength bounds the Argon2 input so an oversized password can't be used to amplify hashing cost.
	maxPasswordLength = 1024
	// maxDisplayNameLength bounds the stored display name (Unicode code points) so an unbounded value can't bloat rows, logs, or UI. Generous for any real name.
	maxDisplayNameLength = 256
)

// detachedWriteTimeout bounds the best-effort writes that detach from the request context — audit events (ADR-0009) and the backoff failure counter (ADR-0006, so an attacker can't skip the counter by disconnecting mid-attempt). Detached writes need their own bound or a stuck store would leak goroutines.
const detachedWriteTimeout = 5 * time.Second

// Backoff starts at five failures, doubles from one minute to fifteen minutes, and applies ±20% jitter (ADR-0006). Configuration may override timing and threshold.
const (
	defaultBackoffThreshold = 5
	defaultBackoffBase      = time.Minute
	defaultBackoffCap       = 15 * time.Minute
	backoffJitterFraction   = 0.2
)

// oidcPendingTTL bounds the window between /auth/google/start and the callback. ADR-0007 pins it at exactly 10 minutes; the service enforces it inside the sealed payload in addition to the cookie's Max-Age.
const oidcPendingTTL = 10 * time.Minute

// oidcMethodMetadata tags login audit events (success and failure) that came through Google, so one AUTH_LOGIN action covers every sign-in method and the method stays queryable (ADR-0009: metadata carries supplemental dimensions).
const oidcMethodMetadata = "google"
