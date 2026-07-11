package identity

import "time"

// LoginBackoff is the per-account progressive-backoff state for password login
// (ADR-0006): consecutive failures and the lockout they may have imposed. It is
// current state, not history — the repository keeps one row per user, rewritten
// in place. A locked account is refused with the SAME ErrInvalidCredentials as
// a wrong password, so the lockout is not observable (no oracle); there is
// deliberately no ErrAccountLocked sentinel.
type LoginBackoff struct {
	// FailureCount is the number of consecutive failed password attempts. It
	// resets on a successful login, once an expired lockout is observed, or once
	// the account has been failure-free for the staleness window.
	FailureCount int
	// LockedUntil is when the lockout ends; nil means not locked.
	LockedUntil *time.Time
	// Locked reports whether password login is refused. It is evaluated by the
	// REPOSITORY on the database clock at read time — deliberately a snapshot
	// field, not a method over LockedUntil, so the expiry decision and the lazy
	// counter reset (which also runs on the database clock, inside the failure
	// upsert) can never disagree under app/DB clock skew (ADR-0006).
	Locked bool
}

// FailureParams carries the backoff policy of one failed-attempt write
// (ADR-0006 Parameters): after Threshold consecutive failures the lockout
// starts at Base·JitterFactor, doubling per further failure up to Cap. A
// counter whose lockout has expired — or whose last failure is older than
// Staleness — restarts at 1. JitterFactor spreads the window (±20% in the
// service) so exact unlock times can't be probed. Values are supplied per call
// so the policy stays owned by configuration, not by the store.
type FailureParams struct {
	Threshold    int
	Base         time.Duration
	Cap          time.Duration
	Staleness    time.Duration
	JitterFactor float64
}
