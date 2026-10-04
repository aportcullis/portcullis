package identity

import (
	"errors"
	"fmt"
	"slices"
)

// Permission is a resource.verb capability seeded in SQL and referenced at its enforcement site (ADR-0008).
type Permission string

// Permission keys enforced in code. The catalog itself is seeded in SQL and loaded at startup; these constants only name the keys enforcement sites check, and ValidatePermissionCatalog proves each exists before serving (ADR-0008).
const (
	// Audit list sees only collection summaries; get unlocks one event's correlation and detail data.
	PermissionAuditList Permission = "audit.list"
	PermissionAuditGet  Permission = "audit.get"

	// The catalog's connections delete verb gates Archive: no hard delete exists (PRD §4.3).
	PermissionConnectionsList   Permission = "connections.list"
	PermissionConnectionsGet    Permission = "connections.get"
	PermissionConnectionsCreate Permission = "connections.create"
	PermissionConnectionsUpdate Permission = "connections.update"
	PermissionConnectionsTest   Permission = "connections.test"
	PermissionConnectionsDelete Permission = "connections.delete"

	PermissionPoliciesGet    Permission = "policies.get"
	PermissionPoliciesUpdate Permission = "policies.update"

	PermissionRequestsList    Permission = "requests.list"
	PermissionRequestsGet     Permission = "requests.get"
	PermissionRequestsCreate  Permission = "requests.create"
	PermissionRequestsApprove Permission = "requests.approve"
	PermissionRequestsReject  Permission = "requests.reject"
	PermissionRequestsExecute Permission = "requests.execute"
)

// ErrPermissionCatalogIncomplete means a permission key enforced in code is missing from the loaded catalog.
var ErrPermissionCatalogIncomplete = errors.New("identity: permission catalog lacks enforced keys")

// EnforcedPermissions returns every permission key enforced in code, in declaration order.
func EnforcedPermissions() []Permission {
	return []Permission{
		PermissionAuditList, PermissionAuditGet,
		PermissionConnectionsList, PermissionConnectionsGet, PermissionConnectionsCreate, PermissionConnectionsUpdate, PermissionConnectionsTest, PermissionConnectionsDelete,
		PermissionPoliciesGet, PermissionPoliciesUpdate,
		PermissionRequestsList, PermissionRequestsGet, PermissionRequestsCreate, PermissionRequestsApprove, PermissionRequestsReject, PermissionRequestsExecute,
	}
}

// ValidatePermissionCatalog returns ErrPermissionCatalogIncomplete naming every enforced key the catalog lacks, so startup fails instead of every affected request failing as Internal.
func ValidatePermissionCatalog(catalog []Permission) error {
	var missing []Permission
	for _, enforced := range EnforcedPermissions() {
		if !slices.Contains(catalog, enforced) {
			missing = append(missing, enforced)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %v", ErrPermissionCatalogIncomplete, missing)
	}
	return nil
}
