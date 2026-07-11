// Package identity is the access-control bounded context: users, roles, and the
// sessions that authenticate them. It holds entities, value objects, repository
// ports, and pure domain rules — no infrastructure. Each concept lives in its
// own file (user.go, session.go, role.go, email.go, oidc.go, backoff.go); this
// file keeps only the cross-concept vocabulary.
package identity

// Typed identifiers keep ids from being mixed up across entities.
type (
	UserID         string
	OrganizationID string
	SessionID      string
	RoleID         string
)
