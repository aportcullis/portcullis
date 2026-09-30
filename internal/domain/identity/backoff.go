package identity

import "time"

// LoginBackoff stores current per-account failure and lockout state. Locked accounts return the same ErrInvalidCredentials as wrong passwords (ADR-0006).
type LoginBackoff struct {
	// FailureCount is the number of consecutive failed password attempts. It resets on a successful login, once an expired lockout is observed, or once the account has been failure-free for the staleness window.
	FailureCount int
	// LockedUntil is when the lockout ends; nil means not locked.
	LockedUntil *time.Time
	// Locked is computed by the repository on the database clock so expiry and counter reset cannot disagree under clock skew.
	Locked bool
}

// FailureParams configures atomic backoff: exponential lockout capped at Cap, jittered expiry, and restart after expiry or staleness (ADR-0006).
type FailureParams struct {
	Threshold    int
	Base         time.Duration
	Cap          time.Duration
	Staleness    time.Duration
	JitterFactor float64
}
