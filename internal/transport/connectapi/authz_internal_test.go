package connectapi

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

type fakeAuthorizer struct {
	err     error
	called  bool
	gotUser identity.User
	gotWant identity.Permission
}

func (f *fakeAuthorizer) Authorize(_ context.Context, user identity.User, want identity.Permission) error {
	f.called = true
	f.gotUser = user
	f.gotWant = want
	return f.err
}

func ctxWithUser(u identity.User) context.Context {
	return context.WithValue(context.Background(), ctxUser, u)
}

func TestRequirePermissionAllows(t *testing.T) {
	az := &fakeAuthorizer{}
	if err := requirePermission(ctxWithUser(identity.User{ID: "u1"}), az, "audit.list"); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
	if !az.called || az.gotWant != "audit.list" || az.gotUser.ID != "u1" {
		t.Fatalf("authorizer not called with expected args: %+v", az)
	}
}

func TestRequirePermissionMissingUserIsUnauthenticated(t *testing.T) {
	az := &fakeAuthorizer{}
	err := requirePermission(context.Background(), az, "audit.list")
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", connect.CodeOf(err))
	}
	if az.called {
		t.Fatal("authorizer must not be consulted without a user")
	}
}

func TestRequirePermissionDeniedIsPermissionDenied(t *testing.T) {
	az := &fakeAuthorizer{err: authz.ErrPermissionDenied}
	err := requirePermission(ctxWithUser(identity.User{ID: "u1"}), az, "audit.list")
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", connect.CodeOf(err))
	}
	// The client-facing message must not leak which permission was required.
	if strings.Contains(err.Error(), "audit.list") {
		t.Fatalf("error message leaks the permission key: %q", err.Error())
	}
}

func TestRequirePermissionOtherErrorIsInternal(t *testing.T) {
	// An off-catalog key (a code defect) must surface as Internal, never as a deny.
	az := &fakeAuthorizer{err: authz.ErrUnknownPermission}
	err := requirePermission(ctxWithUser(identity.User{ID: "u1"}), az, "audit.list")
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Fatalf("expected Internal, got %v", connect.CodeOf(err))
	}
}
