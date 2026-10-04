package connectapi_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
)

// permissionCheckFunctions are the enforcement helpers whose last argument names the required permission.
var permissionCheckFunctions = []string{"requirePermission", "hasPermission"}

// enforcementSite is one permission check found in the package source.
type enforcementSite struct {
	position string
	argument ast.Expr
}

// collectEnforcementSites parses the package's production files and returns every permission check call.
func collectEnforcementSites(t *testing.T) []enforcementSite {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	var sites []enforcementSite
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			if name, ok := call.Fun.(*ast.Ident); ok && slices.Contains(permissionCheckFunctions, name.Name) {
				sites = append(sites, enforcementSite{position: fileSet.Position(call.Pos()).String(), argument: call.Args[len(call.Args)-1]})
			}
			return true
		})
	}
	return sites
}

func TestMigratedCatalogCoversEveryEnforcedPermission(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	catalog, err := authz.LoadCatalog(ctx, postgres.NewIdentityStore(pool))
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if err := identity.ValidatePermissionCatalog(catalog); err != nil {
		t.Errorf("migrated catalog: %v", err)
	}
}

func TestEveryEnforcementSiteNamesADomainPermissionConstant(t *testing.T) {
	sites := collectEnforcementSites(t)
	if len(sites) < 10 {
		t.Fatalf("found %d permission checks, want the package's enforcement sites", len(sites))
	}
	enforcedNames := map[string]bool{}
	for _, name := range []string{
		"PermissionAuditList", "PermissionAuditGet",
		"PermissionConnectionsList", "PermissionConnectionsGet", "PermissionConnectionsCreate", "PermissionConnectionsUpdate", "PermissionConnectionsTest", "PermissionConnectionsDelete",
		"PermissionPoliciesGet", "PermissionPoliciesUpdate",
		"PermissionRequestsList", "PermissionRequestsGet", "PermissionRequestsCreate", "PermissionRequestsApprove", "PermissionRequestsReject", "PermissionRequestsExecute",
		"PermissionUsersList", "PermissionUsersGet", "PermissionUsersCreate", "PermissionUsersUpdate", "PermissionUsersDisable",
		"PermissionRolesList", "PermissionRolesGet", "PermissionRolesCreate", "PermissionRolesUpdate", "PermissionRolesDelete",
	} {
		enforcedNames[name] = true
	}
	if len(enforcedNames) != len(identity.EnforcedPermissions()) {
		t.Fatalf("test name list has %d keys, EnforcedPermissions has %d", len(enforcedNames), len(identity.EnforcedPermissions()))
	}
	for _, site := range sites {
		// Refusal: string literals, local constants and computed keys bypass the startup catalog check.
		selector, ok := site.argument.(*ast.SelectorExpr)
		if !ok {
			t.Errorf("%s: permission argument is %T, want identity.Permission* constant", site.position, site.argument)
			continue
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "identity" || !enforcedNames[selector.Sel.Name] {
			t.Errorf("%s: permission argument %s is not an enforced identity constant", site.position, selector.Sel.Name)
		}
	}
}
