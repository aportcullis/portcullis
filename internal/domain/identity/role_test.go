package identity_test

import (
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

func TestRoleHas(t *testing.T) {
	t.Parallel()

	custom := identity.Role{
		Name:        "auditor",
		Permissions: []identity.Permission{"audit.list", "audit.get"},
	}
	if !custom.Has("audit.get") {
		t.Error("role should grant a permission it holds")
	}
	if custom.Has("users.disable") {
		t.Error("role should not grant a permission it lacks")
	}
}
