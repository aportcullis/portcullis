package connectapi

import (
	"testing"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// TestActiveOperationRegistryUsesEnforcedResourcePurposes inspects the private RPC registry, which has no black-box enumeration surface.
func TestActiveOperationRegistryUsesEnforcedResourcePurposes(t *testing.T) {
	known := map[identity.Permission]bool{}
	for _, permission := range identity.EnforcedPermissions() {
		known[permission] = true
	}
	for procedure, declaration := range activeOperationDeclarations {
		if !known[declaration.permission] {
			t.Errorf("%s uses an unregistered permission", procedure)
		}
		valid := false
		for _, kind := range authz.ResourceTypesForPermission(declaration.permission) {
			if kind == declaration.kind {
				valid = true
			}
		}
		if !valid {
			t.Errorf("%s has an unsupported resource purpose", procedure)
		}
		if declaration.resolveResult && (declaration.kind != identity.ResourceResult || declaration.idField != "request_id") {
			t.Errorf("%s does not resolve result ancestry from its request", procedure)
		}
	}
}
