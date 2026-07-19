package connectionpolicy

import (
	"context"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Repository is the narrow storage surface the policy use cases need (ISP).
// The postgres ConnectionPolicyStore satisfies it; tests use an in-memory
// fake. Mutations carry the audit events that commit in the same transaction
// as the change (ADR-0009).
type Repository interface {
	// DefaultOrganizationID resolves the single self-hosted organization all
	// queries are scoped to (single-org MVP, ADR-0004).
	DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error)
	// GetCurrent returns the connection's current policy snapshot — archived
	// connections included (the policy is part of the historical snapshot).
	GetCurrent(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (connection.Policy, error)
	// UpdatePolicy appends next as the new current version iff the connection
	// is active and its current version still equals expectedVersion; failures
	// map to connection.ErrNotFound / ErrArchived / ErrPolicyConflict.
	UpdatePolicy(ctx context.Context, next connection.Policy, expectedVersion int64, events ...audit.Event) (connection.Policy, error)
}
