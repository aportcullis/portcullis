package administration

import (
	"context"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// organizationPermissions resolves the single organization and an actor's current permissions within it, which every administration decision needs.
type organizationPermissions interface {
	DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error)
	PermissionsForUser(ctx context.Context, org identity.OrganizationID, user identity.UserID) ([]identity.Permission, error)
	// GetRole returns one live role of the organization with its permissions; a role of another organization misses with ErrRoleNotFound.
	GetRole(ctx context.Context, org identity.OrganizationID, role identity.RoleID) (identity.Role, error)
}

// UserRepository persists organization members. Every mutation commits its audit event atomically; status and role changes revoke the member's sessions and, under a per-organization lock, roll back with ErrLastAdministrator when no active administrator would remain (ADR-0053).
type UserRepository interface {
	organizationPermissions
	ListMembers(ctx context.Context, org identity.OrganizationID) ([]identity.Member, error)
	// GetMember misses with ErrUserNotFound when the user has no membership in org.
	GetMember(ctx context.Context, org identity.OrganizationID, user identity.UserID) (identity.Member, error)
	// CreateMember inserts the user, membership and open setup link; a taken email fails with ErrEmailTaken.
	CreateMember(ctx context.Context, org identity.OrganizationID, member identity.NewMember, setup identity.PasswordSetupIssue, event audit.Event) (identity.Member, identity.PasswordSetup, error)
	// IssuePasswordSetup revokes the user's open link and inserts a new one, refusing a disabled user (ErrUserDisabled) or one with a password (ErrPasswordAlreadySet).
	IssuePasswordSetup(ctx context.Context, org identity.OrganizationID, user identity.UserID, setup identity.PasswordSetupIssue, event audit.Event) (identity.PasswordSetup, error)
	// SetMemberStatus changes the account status; disabling also revokes the open setup link.
	SetMemberStatus(ctx context.Context, org identity.OrganizationID, user identity.UserID, status identity.UserStatus, event audit.Event) (identity.Member, error)
	// AssignMemberRole replaces the membership's single role.
	AssignMemberRole(ctx context.Context, org identity.OrganizationID, user identity.UserID, role identity.RoleID, event audit.Event) (identity.Member, error)
}

// RoleRepository persists organization roles. Every mutation commits its audit event atomically, refuses system roles and stale versions, and an update revokes every member's sessions and is checked for a remaining administrator under the organization lock (ADR-0053).
type RoleRepository interface {
	organizationPermissions
	ListRoles(ctx context.Context, org identity.OrganizationID) ([]identity.Role, error)
	// CreateRole fails with ErrRoleNameTaken when a live role of the organization already has the name.
	CreateRole(ctx context.Context, org identity.OrganizationID, definition identity.RoleDefinition, event audit.Event) (identity.Role, error)
	UpdateRole(ctx context.Context, org identity.OrganizationID, role identity.RoleID, expectedVersion int64, definition identity.RoleDefinition, event audit.Event) (identity.Role, error)
	// DeleteRole soft-deletes the role and fails with ErrRoleInUse while a membership references it.
	DeleteRole(ctx context.Context, org identity.OrganizationID, role identity.RoleID, expectedVersion int64, event audit.Event) error
}

// AuditRecorder appends a best-effort audit event for a refused administration attempt.
type AuditRecorder interface {
	Record(ctx context.Context, event audit.Event) error
}
