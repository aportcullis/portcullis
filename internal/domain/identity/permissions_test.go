package identity_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// seededCatalog mirrors a migrated catalog: every enforced key plus keys no code enforces yet.
func seededCatalog() []identity.Permission {
	return append(identity.EnforcedPermissions(), "users.list", "roles.update", "savedqueries.share")
}

func TestValidatePermissionCatalogAcceptsCatalogsCoveringEnforcedKeys(t *testing.T) {
	t.Parallel()
	reversed := slices.Clone(seededCatalog())
	slices.Reverse(reversed)
	cases := map[string][]identity.Permission{
		"exactly the enforced keys":  identity.EnforcedPermissions(),
		"seeded superset":            seededCatalog(),
		"reordered catalog":          reversed,
		"duplicated catalog entries": append(seededCatalog(), identity.PermissionRequestsExecute, identity.PermissionAuditGet),
	}
	for name, catalog := range cases {
		if err := identity.ValidatePermissionCatalog(catalog); err != nil {
			t.Errorf("%s: ValidatePermissionCatalog = %v, want nil", name, err)
		}
	}
}

func TestValidatePermissionCatalogRefusesMissingOrMisspelledKeys(t *testing.T) {
	t.Parallel()
	withoutExecute := slices.DeleteFunc(seededCatalog(), func(key identity.Permission) bool { return key == identity.PermissionRequestsExecute })
	cases := []struct {
		name        string
		catalog     []identity.Permission
		wantMissing []string
	}{
		{"execute key removed", withoutExecute, []string{"requests.execute"}},
		{"empty catalog", nil, []string{"audit.list", "requests.execute"}},
		{"case-changed key", append(slices.Clone(withoutExecute), "Requests.Execute"), []string{"requests.execute"}},
		{"padded key", append(slices.Clone(withoutExecute), "requests.execute "), []string{"requests.execute"}},
	}
	for _, tc := range cases {
		err := identity.ValidatePermissionCatalog(tc.catalog)
		if !errors.Is(err, identity.ErrPermissionCatalogIncomplete) {
			t.Errorf("%s: ValidatePermissionCatalog = %v, want ErrPermissionCatalogIncomplete", tc.name, err)
			continue
		}
		for _, key := range tc.wantMissing {
			if !strings.Contains(err.Error(), key) {
				t.Errorf("%s: error %q does not name %s", tc.name, err, key)
			}
		}
	}
}

func TestEnforcedPermissionsListsEveryDeclaredPermissionConstant(t *testing.T) {
	t.Parallel()
	file, err := parser.ParseFile(token.NewFileSet(), "permissions.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var declared []identity.Permission
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Values) != 1 {
				continue
			}
			if typeName, ok := value.Type.(*ast.Ident); !ok || typeName.Name != "Permission" {
				continue
			}
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok {
				t.Fatalf("permission constant %s is not a string literal", value.Names[0].Name)
			}
			key, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			declared = append(declared, identity.Permission(key))
		}
	}
	enforced := identity.EnforcedPermissions()
	slices.Sort(declared)
	slices.Sort(enforced)
	if !slices.Equal(declared, enforced) {
		t.Errorf("declared permission constants %v differ from EnforcedPermissions %v", declared, enforced)
	}
}
