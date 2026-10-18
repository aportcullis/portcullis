package authz

import (
	"context"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// ActiveAuthorizationSource supplies current resource admission and advisory capability snapshots.
type ActiveAuthorizationSource interface {
	AuthorizeOperation(context.Context, identity.UserID, identity.Permission, Operation) error
	SessionCapabilities(context.Context, identity.UserID) (SessionCapabilities, error)
}

// SessionBinding describes one caller-owned grant without exposing its protected source expression.
type SessionBinding struct {
	ID, RoleID, RoleName, ScopeType, ConnectionID string
	HasCondition                                  bool
}

// SessionCapabilities separates unrestricted compatibility hints from evaluated navigation capabilities.
type SessionCapabilities struct {
	Permissions, Capabilities []identity.Permission
	Bindings                  []SessionBinding
	Revision                  int64
}

// ActiveService is the sole runtime authorizer after atomic activation; it has no legacy resolver.
type ActiveService struct{ source ActiveAuthorizationSource }

// NewActive validates the resource-aware source before serving.
func NewActive(source ActiveAuthorizationSource) (*ActiveService, error) {
	if source == nil {
		return nil, ErrAuthorizationInactive
	}
	return &ActiveService{source: source}, nil
}

// Authorize checks the server-established resource purpose on each operation.
func (service *ActiveService) Authorize(ctx context.Context, user identity.User, want identity.Permission) error {
	operation, ok := OperationFromContext(ctx)
	if !ok || operation.Principal != user.ID {
		return ErrPermissionDenied
	}
	return service.source.AuthorizeOperation(ctx, user.ID, want, operation)
}

// SessionInfo retains safe compatibility fields; richer capability data is obtained separately.
func (service *ActiveService) SessionInfo(ctx context.Context, user identity.User) ([]identity.Permission, string, error) {
	result, err := service.source.SessionCapabilities(ctx, user.ID)
	return result.Permissions, "", err
}

// Capabilities exposes scope-aware navigation data without granting protected operations.
func (service *ActiveService) Capabilities(ctx context.Context, user identity.User) (SessionCapabilities, error) {
	return service.source.SessionCapabilities(ctx, user.ID)
}
