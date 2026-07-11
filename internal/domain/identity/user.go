package identity

import "time"

// UserStatus is the lifecycle state of an account. These are fixed domain
// enums the code branches on (see Active), so they live in code; everything
// configurable — the permission catalog, roles, the OIDC provider — comes from
// the database or config, not constants.
type UserStatus string

// Account lifecycle states — mirrors the users.status check constraint.
const (
	StatusActive   UserStatus = "active"
	StatusDisabled UserStatus = "disabled"
)

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
