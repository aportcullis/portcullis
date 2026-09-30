package authz_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

type fakeResolver struct {
	org     identity.OrganizationID
	orgErr  error
	perms   map[identity.UserID][]identity.Permission
	permErr error
	roles   map[identity.UserID]string
	roleErr error
	catalog []identity.Permission
	catErr  error
}

func (f *fakeResolver) DefaultOrganizationID(context.Context) (identity.OrganizationID, error) {
	if f.orgErr != nil {
		return "", f.orgErr
	}
	if f.org == "" {
		return "org-default", nil
	}
	return f.org, nil
}

func (f *fakeResolver) PermissionsForUser(_ context.Context, _ identity.OrganizationID, id identity.UserID) ([]identity.Permission, error) {
	if f.permErr != nil {
		return nil, f.permErr
	}
	return f.perms[id], nil
}

func (f *fakeResolver) RoleNameForUser(_ context.Context, _ identity.OrganizationID, id identity.UserID) (string, error) {
	if f.roleErr != nil {
		return "", f.roleErr
	}
	return f.roles[id], nil
}

func (f *fakeResolver) ListPermissions(context.Context) ([]identity.Permission, error) {
	if f.catErr != nil {
		return nil, f.catErr
	}
	return f.catalog, nil
}

var testCatalog = []identity.Permission{"audit.list", "audit.get", "connections.get"}

func newService(t *testing.T, r authz.PermissionResolver) *authz.Service {
	t.Helper()
	svc, err := authz.New(r, testCatalog)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc
}

func TestAuthorizeAllowsUserWithPermission(t *testing.T) {
	user := identity.User{ID: "u1"}
	svc := newService(t, &fakeResolver{
		perms: map[identity.UserID][]identity.Permission{
			user.ID: {"connections.get", "audit.list"},
		},
	})
	if err := svc.Authorize(context.Background(), user, "audit.list"); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeDeniesUserWithoutPermission(t *testing.T) {
	user := identity.User{ID: "u1"}
	svc := newService(t, &fakeResolver{
		perms: map[identity.UserID][]identity.Permission{
			user.ID: {"connections.get"},
		},
	})
	err := svc.Authorize(context.Background(), user, "audit.list")
	if !errors.Is(err, authz.ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied, got %v", err)
	}
}

func TestAuthorizeDeniesUserWithNoPermissions(t *testing.T) {
	user := identity.User{ID: "u1"}
	svc := newService(t, &fakeResolver{})
	err := svc.Authorize(context.Background(), user, "audit.list")
	if !errors.Is(err, authz.ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for permission-less user, got %v", err)
	}
}

func TestAuthorizeRejectsUnknownPermissionKey(t *testing.T) {
	user := identity.User{ID: "u1"}
	// The user is granted the typo'd key, proving the guard fires BEFORE resolution and does not depend on the user lacking it: an off-catalog key is a code bug, not a denial, even if a (corrupt) grant would otherwise match.
	svc := newService(t, &fakeResolver{
		perms: map[identity.UserID][]identity.Permission{
			user.ID: {"audit.lst"},
		},
	})
	err := svc.Authorize(context.Background(), user, "audit.lst")
	if !errors.Is(err, authz.ErrUnknownPermission) {
		t.Fatalf("expected ErrUnknownPermission, got %v", err)
	}
	if errors.Is(err, authz.ErrPermissionDenied) {
		t.Fatal("unknown key must not read as a permission denial")
	}
}

func TestAuthorizePropagatesResolverError(t *testing.T) {
	user := identity.User{ID: "u1"}
	sentinel := errors.New("db down")
	svc := newService(t, &fakeResolver{permErr: sentinel})
	err := svc.Authorize(context.Background(), user, "audit.list")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected resolver error propagated, got %v", err)
	}
	// A DB blip must never be reported as a denial.
	if errors.Is(err, authz.ErrPermissionDenied) {
		t.Fatal("resolver failure must not read as a permission denial")
	}
}

func TestAuthorizePropagatesOrgResolutionError(t *testing.T) {
	user := identity.User{ID: "u1"}
	sentinel := errors.New("no org")
	svc := newService(t, &fakeResolver{orgErr: sentinel})
	err := svc.Authorize(context.Background(), user, "audit.list")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected org resolution error propagated, got %v", err)
	}
	if errors.Is(err, authz.ErrPermissionDenied) {
		t.Fatal("org resolution failure must not read as a permission denial")
	}
}

func TestSessionInfoReturnsPermissionsAndRole(t *testing.T) {
	user := identity.User{ID: "u1"}
	svc := newService(t, &fakeResolver{
		perms: map[identity.UserID][]identity.Permission{user.ID: {"connections.get", "audit.list"}},
		roles: map[identity.UserID]string{user.ID: "admin"},
	})
	perms, role, err := svc.SessionInfo(context.Background(), user)
	if err != nil {
		t.Fatalf("SessionInfo: %v", err)
	}
	if role != "admin" {
		t.Errorf("role = %q, want admin", role)
	}

	if len(perms) != 2 || perms[0] != "audit.list" || perms[1] != "connections.get" {
		t.Errorf("perms = %v, want sorted [audit.list connections.get]", perms)
	}
}

func TestSessionInfoPropagatesRoleError(t *testing.T) {
	user := identity.User{ID: "u1"}
	sentinel := errors.New("role query down")
	svc := newService(t, &fakeResolver{
		perms:   map[identity.UserID][]identity.Permission{user.ID: {"audit.list"}},
		roleErr: sentinel,
	})
	if _, _, err := svc.SessionInfo(context.Background(), user); !errors.Is(err, sentinel) {
		t.Fatalf("expected role error propagated, got %v", err)
	}
}

func TestNewRejectsEmptyCatalog(t *testing.T) {
	if _, err := authz.New(&fakeResolver{}, nil); err == nil {
		t.Fatal("expected error for empty catalog")
	}
}

func TestNewRejectsNilResolver(t *testing.T) {
	if _, err := authz.New(nil, testCatalog); err == nil {
		t.Fatal("expected error for nil resolver")
	}
}

func TestLoadCatalogReturnsSeededKeys(t *testing.T) {
	got, err := authz.LoadCatalog(context.Background(), &fakeResolver{catalog: testCatalog})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if len(got) != len(testCatalog) {
		t.Fatalf("expected %d keys, got %d", len(testCatalog), len(got))
	}
}

func TestLoadCatalogRejectsEmpty(t *testing.T) {
	if _, err := authz.LoadCatalog(context.Background(), &fakeResolver{catalog: nil}); err == nil {
		t.Fatal("expected error for empty catalog (unseeded DB)")
	}
}

func TestLoadCatalogPropagatesSourceError(t *testing.T) {
	sentinel := errors.New("query failed")
	if _, err := authz.LoadCatalog(context.Background(), &fakeResolver{catErr: sentinel}); !errors.Is(err, sentinel) {
		t.Fatalf("expected source error propagated, got %v", err)
	}
}
