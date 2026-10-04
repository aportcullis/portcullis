package identity_test

import (
	"errors"
	"slices"
	"strings"
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

func TestNewRoleDefinitionAcceptsCatalogPermissions(t *testing.T) {
	t.Parallel()
	catalog := identity.EnforcedPermissions()
	cases := []struct {
		name            string
		roleName        string
		permissions     []identity.Permission
		wantName        string
		wantPermissions []identity.Permission
	}{
		{"auditor", "auditor", []identity.Permission{identity.PermissionAuditList}, "auditor", []identity.Permission{identity.PermissionAuditList}},
		{"trimmed unicode name", "  감사 담당  ", []identity.Permission{identity.PermissionAuditGet}, "감사 담당", []identity.Permission{identity.PermissionAuditGet}},
		{"maximum length", strings.Repeat("r", identity.MaxRoleNameLength), nil, strings.Repeat("r", identity.MaxRoleNameLength), nil},
		{"sorted and de-duplicated", "reviewer", []identity.Permission{identity.PermissionRequestsReject, identity.PermissionRequestsApprove, identity.PermissionRequestsReject}, "reviewer", []identity.Permission{identity.PermissionRequestsApprove, identity.PermissionRequestsReject}},
	}
	for _, tc := range cases {
		definition, err := identity.NewRoleDefinition(tc.roleName, tc.permissions, catalog)
		if err != nil {
			t.Errorf("%s: NewRoleDefinition = %v, want nil", tc.name, err)
			continue
		}
		if definition.Name != tc.wantName || !slices.Equal(definition.Permissions, tc.wantPermissions) {
			t.Errorf("%s: definition = %+v, want %q %v", tc.name, definition, tc.wantName, tc.wantPermissions)
		}
	}
}

func TestNewRoleDefinitionRefusesInvalidNamesAndUnknownPermissions(t *testing.T) {
	t.Parallel()
	catalog := identity.EnforcedPermissions()
	cases := []struct {
		name        string
		roleName    string
		permissions []identity.Permission
		want        error
	}{
		{"empty name", "", nil, identity.ErrInvalidRoleName},
		{"whitespace name", " \t ", nil, identity.ErrInvalidRoleName},
		{"too long", strings.Repeat("r", identity.MaxRoleNameLength+1), nil, identity.ErrInvalidRoleName},
		{"control character", "audit\nor", nil, identity.ErrInvalidRoleName},
		{"format character", "admin" + string(rune(0x200b)), nil, identity.ErrInvalidRoleName},
		{"unknown key", "auditor", []identity.Permission{"audit.delete"}, identity.ErrPermissionNotInCatalog},
		{"case-changed key", "auditor", []identity.Permission{"Audit.List"}, identity.ErrPermissionNotInCatalog},
	}
	for _, tc := range cases {
		if _, err := identity.NewRoleDefinition(tc.roleName, tc.permissions, catalog); !errors.Is(err, tc.want) {
			t.Errorf("%s: NewRoleDefinition = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestDiffPermissionsReportsAddedAndRemovedKeys(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name               string
		previous, next     []identity.Permission
		added, removedKeys []identity.Permission
	}{
		{"grant only", nil, []identity.Permission{"audit.list"}, []identity.Permission{"audit.list"}, nil},
		{"revoke only", []identity.Permission{"audit.list"}, nil, nil, []identity.Permission{"audit.list"}},
		{"swap", []identity.Permission{"audit.list", "users.get"}, []identity.Permission{"users.get", "audit.get"}, []identity.Permission{"audit.get"}, []identity.Permission{"audit.list"}},
		{"unchanged", []identity.Permission{"users.get"}, []identity.Permission{"users.get"}, nil, nil},
	}
	for _, tc := range cases {
		added, removed := identity.DiffPermissions(tc.previous, tc.next)
		if !slices.Equal(added, tc.added) || !slices.Equal(removed, tc.removedKeys) {
			t.Errorf("%s: DiffPermissions = %v, %v; want %v, %v", tc.name, added, removed, tc.added, tc.removedKeys)
		}
	}
}
