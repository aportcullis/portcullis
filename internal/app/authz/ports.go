package authz

import (
	"context"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// PermissionResolver reads organization-scoped effective grants on each request; DefaultOrganizationID selects the single MVP organization.
type PermissionResolver interface {
	DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error)
	PermissionsForUser(ctx context.Context, org identity.OrganizationID, id identity.UserID) ([]identity.Permission, error)
	// RoleNameForUser returns the user's role display name (UI label only — authorization never consults it, ADR-0008). Resolved alongside the permissions for the session views so the org is looked up once.
	RoleNameForUser(ctx context.Context, org identity.OrganizationID, id identity.UserID) (string, error)
}

// CatalogSource loads the seeded permission-key catalog. It is used ONCE, at startup (LoadCatalog), never per request: the catalog is the immutable universe of valid keys — changed only by a migration plus a redeploy — so it is snapshotted for the process lifetime. IdentityStore satisfies it via ListPermissions.
type CatalogSource interface {
	ListPermissions(ctx context.Context) ([]identity.Permission, error)
}
