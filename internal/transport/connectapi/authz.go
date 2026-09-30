package connectapi

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// authorizer is the permission-check surface a protected handler needs: it answers whether an authenticated user holds a required permission (ADR-0008). It is a local, consumer-defined port so handler tests substitute a fake; the app-layer authz.Service is the production adapter.
type authorizer interface {
	Authorize(ctx context.Context, user identity.User, want identity.Permission) error
}

// requirePermission checks the inline permission key (ADR-0008). Missing identity is Unauthenticated, denial is generic PermissionDenied, and resolver failures are Internal.
func requirePermission(ctx context.Context, az authorizer, want identity.Permission) error {
	user, ok := userFromContext(ctx)
	if !ok {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	switch err := az.Authorize(ctx, user, want); {
	case err == nil:
		return nil
	case errors.Is(err, authz.ErrPermissionDenied):
		return connect.NewError(connect.CodePermissionDenied, errors.New("permission denied"))
	default:
		return connect.NewError(connect.CodeInternal, errors.New("authorization error"))
	}
}

// hasPermission distinguishes denial from resolver failure when widening visibility; callers must propagate infrastructure errors.
func hasPermission(ctx context.Context, az authorizer, user identity.User, want identity.Permission) (bool, error) {
	switch err := az.Authorize(ctx, user, want); {
	case err == nil:
		return true, nil
	case errors.Is(err, authz.ErrPermissionDenied):
		return false, nil
	default:
		return false, connect.NewError(connect.CodeInternal, errors.New("authorization error"))
	}
}
