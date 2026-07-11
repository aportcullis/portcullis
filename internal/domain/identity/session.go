package identity

import "time"

// Session is a server-side authenticated session. The opaque token handed to the
// client is never stored here; only its hash lives in the repository.
type Session struct {
	ID                SessionID
	UserID            UserID
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
	CreatedAt         time.Time
}

// NewSession builds a session with idle and absolute expiries measured from now.
func NewSession(id SessionID, user UserID, now time.Time, idle, absolute time.Duration) Session {
	return Session{
		ID:                id,
		UserID:            user,
		IdleExpiresAt:     now.Add(idle),
		AbsoluteExpiresAt: now.Add(absolute),
		CreatedAt:         now,
	}
}

// Valid reports whether the session is usable at now: not revoked, and within
// both the idle and absolute expiry windows.
func (s Session) Valid(now time.Time) bool {
	if s.RevokedAt != nil {
		return false
	}
	return now.Before(s.IdleExpiresAt) && now.Before(s.AbsoluteExpiresAt)
}
