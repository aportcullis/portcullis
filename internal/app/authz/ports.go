package authz

import (
	"context"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// PermissionResolver is the per-request authorization surface: it resolves a
// user's effective permissions, org-scoped. DefaultOrganizationID picks the single
// self-hosted organization (single-org MVP, ADR-0004); PermissionsForUser returns
// the union of that user's roles' permissions. It is consumer-defined (ISP) — only
// the two methods Authorize calls — so the postgres IdentityStore satisfies it with
// no new methods and tests substitute an in-memory fake.
type PermissionResolver interface {
	DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error)
	PermissionsForUser(ctx context.Context, org identity.OrganizationID, id identity.UserID) ([]identity.Permission, error)
}

// CatalogSource loads the seeded permission-key catalog. It is used ONCE, at
// startup (LoadCatalog), never per request: the catalog is the immutable universe
// of valid keys — changed only by a migration plus a redeploy — so it is
// snapshotted for the process lifetime. IdentityStore satisfies it via
// ListPermissions.
type CatalogSource interface {
	ListPermissions(ctx context.Context) ([]identity.Permission, error)
}
