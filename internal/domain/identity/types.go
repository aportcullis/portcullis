// Package identity defines users, roles, sessions, and access-control rules.
package identity

// Typed identifiers keep ids from being mixed up across entities.
type (
	UserID         string
	OrganizationID string
	SessionID      string
	RoleID         string
)
