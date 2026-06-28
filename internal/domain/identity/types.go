// Package identity is the access-control bounded context: users, roles, and the
// server-side sessions that authenticate them. It holds entities, value objects,
// repository ports, and pure domain rules — no infrastructure.
package identity

import "time"

// Typed identifiers keep ids from being mixed up across entities.
type (
	UserID         string
	OrganizationID string
	SessionID      string
	RoleID         string
)

// UserStatus is the lifecycle state of an account.
type UserStatus string

// User is an account.
type User struct {
	ID          UserID
	Email       string
	DisplayName string
	Status      UserStatus
	CreatedAt   time.Time
}

// Active reports whether the user may authenticate and act.
func (u User) Active() bool { return u.Status == StatusActive }

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
