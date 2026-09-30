package connectionpolicy

import (
	"context"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Repository mutations commit audit events atomically and observe expiry under the request lock before deciding (ADR-0009/0018).
type Repository interface {
	// DefaultOrganizationID resolves the single self-hosted organization all queries are scoped to (single-org MVP, ADR-0004).
	DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error)
	// GetCurrent returns the connection's current policy snapshot — archived connections included (the policy is part of the historical snapshot).
	GetCurrent(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (connection.Policy, error)
	// UpdatePolicy appends next as the new current version iff the connection is active and its current version still equals expectedVersion; failures map to connection.ErrNotFound / ErrArchived / ErrPolicyConflict.
	UpdatePolicy(ctx context.Context, next connection.Policy, expectedVersion int64, events ...audit.Event) (connection.Policy, error)
}
