package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/jackc/pgx/v5/pgtype"
)

// historyCandidate records every permission contributing to visible rows or totals.
func (scope *policyReadScope) historyCandidate(ctx context.Context, resource identity.ResourceReference, permission identity.Permission) (bool, error) {
	decision, err := scope.history.authorize(ctx, resource, permission)
	if errors.Is(err, authz.ErrPermissionDenied) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	scope.decisions = append(scope.decisions, readCheckpoint{decision: decision, permission: permission})
	return true, nil
}

// checkHistoryOutput resolves current ancestry in batches and reevaluates all count and page witnesses at fresh database time.
func (history *resourceGrantCache) checkHistoryOutput(ctx context.Context, queries *db.Queries, at time.Time, decisions []readCheckpoint) error {
	scope := &policyReadScope{policy: history.scope.policy, queries: queries, organization: history.scope.organization, operation: history.scope.operation, at: at}
	current, err := newResourceGrantCache(ctx, scope)
	if err != nil {
		return err
	}
	if current.revision != history.revision {
		return authz.ErrPermissionDenied
	}
	organization, err := policyOrganizationUUID(scope.organization)
	if err != nil {
		return err
	}
	batchSize := scope.policy.readSettings.MaxCandidates
	for start := 0; start < len(decisions); start += batchSize {
		batch := decisions[start:min(start+batchSize, len(decisions))]
		identifiers := make([]pgtype.UUID, 0, len(batch))
		for _, previous := range batch {
			identifier, valid := parseBindingPolicyUUID(previous.decision.Resource.ID)
			if !valid {
				return authz.ErrInvalidSnapshot
			}
			identifiers = append(identifiers, identifier)
		}
		rows, err := queries.ResolveHistoryResources(ctx, db.ResolveHistoryResourcesParams{OrganizationID: organization, ResourceType: string(scope.operation.Type), ResourceIds: identifiers})
		if err != nil {
			return bindingReadError(ctx)
		}
		resources := make(map[string]identity.ResourceReference, len(rows))
		for _, row := range rows {
			resource := identity.ResourceReference{OrganizationID: scope.organization, Type: scope.operation.Type, ID: uuidToString(row.ResourceID), ConnectionID: uuidToString(row.ConnectionID)}
			resources[resource.ID] = resource
		}
		for _, previous := range batch {
			resource, exists := resources[previous.decision.Resource.ID]
			if !exists || resource != previous.decision.Resource {
				return authz.ErrPermissionDenied
			}
			decision, err := current.authorize(ctx, resource, previous.permission)
			if err != nil {
				return err
			}
			if decision.Revision != previous.decision.Revision {
				return authz.ErrPermissionDenied
			}
		}
	}
	return ctx.Err()
}
