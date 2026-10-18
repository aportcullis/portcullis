package connectapi

import (
	"context"
	"errors"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// sessionView maps one coherent advisory snapshot; failures expose no stale capabilities.
func (a *AuthService) sessionView(ctx context.Context, user identity.User) *portcullisv1.LoginResponse {
	result := &portcullisv1.LoginResponse{User: toProtoUser(user)}
	provider, exists := a.session.(sessionCapabilityProvider)
	if !exists {
		var err error
		result.Permissions, result.RoleName, err = a.sessionInfo(ctx, user)
		if err != nil {
			return a.unavailableSessionView(ctx, user, err)
		}
		return result
	}
	snapshot, err := provider.Capabilities(ctx, user)
	if err != nil {
		return a.unavailableSessionView(ctx, user, err)
	}
	if snapshot.Revision <= 0 {
		return a.unavailableSessionView(ctx, user, authz.ErrInvalidSnapshot)
	}
	for _, key := range snapshot.Permissions {
		result.Permissions = append(result.Permissions, string(key))
	}
	for _, key := range snapshot.Capabilities {
		result.Capabilities = append(result.Capabilities, string(key))
	}
	for _, binding := range snapshot.Bindings {
		result.Bindings = append(result.Bindings, &portcullisv1.SessionBinding{Id: binding.ID, RoleId: binding.RoleID, RoleName: binding.RoleName, ScopeType: binding.ScopeType, ConnectionId: binding.ConnectionID, HasCondition: binding.HasCondition})
	}
	result.AuthorizationRevision = snapshot.Revision
	return result
}

// sessionInfo maps legacy advisory data without concealing resolution errors.
func (a *AuthService) sessionInfo(ctx context.Context, user identity.User) ([]string, string, error) {
	permissions, role, err := a.session.SessionInfo(ctx, user)
	if err != nil {
		return nil, "", err
	}
	keys := make([]string, len(permissions))
	for idx, permission := range permissions {
		keys[idx] = string(permission)
	}
	return keys, role, nil
}

// unavailableSessionView exposes authenticated identity without partial hints or private resolver diagnostics.
func (a *AuthService) unavailableSessionView(ctx context.Context, user identity.User, cause error) *portcullisv1.LoginResponse {
	reason := "unavailable"
	switch {
	case errors.Is(cause, context.Canceled):
		reason = "cancelled"
	case errors.Is(cause, context.DeadlineExceeded):
		reason = "deadline"
	case errors.Is(cause, authz.ErrBindingLimitExceeded):
		reason = "binding_limit"
	case errors.Is(cause, authz.ErrInvalidSnapshot), errors.Is(cause, authz.ErrUnknownPermission):
		reason = "invalid_snapshot"
	case errors.Is(cause, authz.ErrPermissionDenied):
		reason = "ineligible"
	}
	a.logger.WarnContext(ctx, "session authorization metadata unavailable", "reason", reason)
	return &portcullisv1.LoginResponse{User: toProtoUser(user), AuthorizationUnavailable: true}
}
