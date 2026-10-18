package authz

import (
	"context"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

type operationContextKey struct{}

// Operation is server-selected RPC purpose; resource ancestry is still resolved from storage.
type Operation struct {
	Principal     identity.UserID
	Permission    identity.Permission
	Type          identity.ResourceType
	ID            string
	Collection    bool
	ResolveResult bool
}

// WithOperation pins authenticated identity and declared RPC purpose without granting authority.
func WithOperation(ctx context.Context, operation Operation) context.Context {
	return context.WithValue(ctx, operationContextKey{}, operation)
}

// OperationFromContext retrieves the server-established purpose for downstream policy checks.
func OperationFromContext(ctx context.Context) (Operation, bool) {
	operation, ok := ctx.Value(operationContextKey{}).(Operation)
	return operation, ok && operation.Principal != ""
}
