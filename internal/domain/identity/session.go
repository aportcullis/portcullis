package identity

import "time"

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
