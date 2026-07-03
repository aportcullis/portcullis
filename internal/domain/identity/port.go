package identity

import (
	"context"
	"time"
)

// UserRepository persists users, their password credential, memberships, and
// resolves a user's effective permissions. Lookups miss with ErrUserNotFound.
type UserRepository interface {
	CountUsers(ctx context.Context) (int64, error)
	DefaultOrganizationID(ctx context.Context) (OrganizationID, error)
	CreateUser(ctx context.Context, email, displayName string) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error)
	GetUserByID(ctx context.Context, id UserID) (User, error)
	SetPassword(ctx context.Context, id UserID, phc string) error
	GetPasswordHash(ctx context.Context, id UserID) (string, error)
	AddMembership(ctx context.Context, org OrganizationID, user UserID, role RoleID) error
	// PermissionsForUser returns the union of the user's roles' permissions within
	// one organization (org-scoped per ADR-0004).
	PermissionsForUser(ctx context.Context, org OrganizationID, id UserID) ([]Permission, error)
}

// RoleRepository resolves roles. Roles are referenced by id, never by a hardcoded
// name; BootstrapRoleID resolves the seeded default role via its DB flag. Custom
// role CRUD is added with the role-management feature.
type RoleRepository interface {
	BootstrapRoleID(ctx context.Context, org OrganizationID) (RoleID, error)
}

// PermissionCatalog loads the seeded permission keys (the catalog is in SQL).
type PermissionCatalog interface {
	ListPermissions(ctx context.Context) ([]Permission, error)
}

// OIDCRepository resolves and links external OIDC identities to local users.
type OIDCRepository interface {
	FindUserBySubject(ctx context.Context, issuer, subject string) (User, error)
	LinkIdentity(ctx context.Context, id OIDCIdentity) error
}

// SessionRepository persists server-side sessions keyed by token hash.
// Single-session revocation lives on the auth application port (auth.Repository)
// instead: it is always audited, and audit.Event already imports this package,
// so declaring it here would create a domain-level import cycle.
type SessionRepository interface {
	CreateSession(ctx context.Context, s Session, tokenHash []byte) (Session, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, error)
	// RevokeUserSessions invalidates all of a user's active sessions (a new login
	// or a privilege change rotates them out — ADR-0006).
	RevokeUserSessions(ctx context.Context, user UserID) error
	// ExtendSessionIdle slides the idle expiry forward on activity (capped at the
	// absolute expiry by the store); a no-op on revoked sessions.
	ExtendSessionIdle(ctx context.Context, id SessionID, idle time.Time) error
}
