package connectapi

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// authorizer is the consumer-defined permission port; production injects the resource-aware active application service.
type authorizer interface {
	Authorize(ctx context.Context, user identity.User, want identity.Permission) error
}

// requirePermission checks the inline key and preserves active policy refusal, cancellation and availability codes.
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
	case isResourceAuthorizationError(err) && !errors.Is(err, authz.ErrUnknownPermission):
		return activeAuthorizationError(err)
	default:
		return newServerFaultError(connect.CodeInternal, "authorization error", err)
	}
}

// hasPermission distinguishes denial from resolver failure when widening visibility; callers must propagate infrastructure errors.
func hasPermission(ctx context.Context, az authorizer, user identity.User, want identity.Permission) (bool, error) {
	switch err := az.Authorize(ctx, user, want); {
	case err == nil:
		return true, nil
	case errors.Is(err, authz.ErrPermissionDenied):
		return false, nil
	case isResourceAuthorizationError(err) && !errors.Is(err, authz.ErrUnknownPermission):
		return false, activeAuthorizationError(err)
	default:
		return false, newServerFaultError(connect.CodeInternal, "authorization error", err)
	}
}

// isResourceAuthorizationError identifies safe active-policy refusals and bounded operational failures.
func isResourceAuthorizationError(err error) bool {
	for _, target := range []error{authz.ErrPermissionDenied, authz.ErrSnapshotUnavailable, authz.ErrAuthorizationInactive, authz.ErrInvalidSnapshot, authz.ErrBindingLimitExceeded, authz.ErrUnknownPermission} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
