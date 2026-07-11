package connectapi

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// authorizer is the permission-check surface a protected handler needs: it answers
// whether an authenticated user holds a required permission (ADR-0008). It is a
// local, consumer-defined port so handler tests substitute a fake; the app-layer
// authz.Service is the production adapter.
type authorizer interface {
	Authorize(ctx context.Context, user identity.User, want identity.Permission) error
}

// requirePermission enforces has(permission) at an exact call site, inline
// (ADR-0008: a permission key is named only where it is enforced, never in a
// central procedure→permission map). The user is the one the auth interceptor
// injected into the context. Outcomes map to Connect codes:
//   - no authenticated user in context -> CodeUnauthenticated (a defensive
//     backstop; the interceptor already guarantees a user for non-public
//     procedures).
//   - authz.ErrPermissionDenied -> CodePermissionDenied, with a generic message
//     that does NOT name the permission (no capability-enumeration leak), matching
//     the interceptor's generic auth/CSRF errors.
//   - anything else (an off-catalog key defect, or a resolver/DB failure) ->
//     CodeInternal, so a bug or a database blip is never reported to the client as
//     a legitimate "forbidden".
//
// want is the general enforcement primitive: every future gated RPC names its own
// key here (ADR-0008). It has a single caller today (audit.list, the first gated
// RPC), which unparam reads as a constant argument — hence the suppression.
//
//nolint:unparam // want varies per enforcement site; only one exists pre-M1.
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
