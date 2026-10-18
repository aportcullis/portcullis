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

// historyGrantKey identifies grants reusable across trusted rows with the same ancestry.
type historyGrantKey struct {
	permission   identity.Permission
	connectionID string
}

// historyAuthorization evaluates row-specific predicates over transaction-local cached grants.
type historyAuthorization struct {
	scope     *policyReadScope
	revision  int64
	resource  identity.ResourceReference
	snapshots map[historyGrantKey]authz.AuthorizationSnapshot
	service   *authz.ResourceService
}

// newHistoryAuthorization pins membership and revision even when no row is visible.
func newHistoryAuthorization(ctx context.Context, scope *policyReadScope) (*historyAuthorization, error) {
	organization, err := policyOrganizationUUID(scope.organization)
	if err != nil {
		return nil, err
	}
	principal, valid := parseBindingPolicyUUID(string(scope.operation.Principal))
	if !valid {
		return nil, authz.ErrPermissionDenied
	}
	authority, err := scope.queries.GetBindingAuthorizationContext(ctx, db.GetBindingAuthorizationContextParams{OrganizationID: organization, UserID: principal})
	if err != nil {
		return nil, bindingReadError(ctx)
	}
	if !authority.ActiveMember {
		return nil, authz.ErrPermissionDenied
	}
	if authority.Revision <= 0 {
		return nil, authz.ErrAuthorizationInactive
	}
	history := &historyAuthorization{scope: scope, revision: authority.Revision, snapshots: make(map[historyGrantKey]authz.AuthorizationSnapshot)}
	history.service, err = authz.NewResourceService(history, scope.policy.conditions, scope.policy.catalog, scope.policy.settings)
	return history, err
}

// LoadAuthorizationSnapshot reuses bounded grants only for ancestry supplied by an organization-scoped row.
func (history *historyAuthorization) LoadAuthorizationSnapshot(ctx context.Context, actor identity.UserID, permission identity.Permission, target authz.ResourceTarget, limit int) (authz.AuthorizationSnapshot, error) {
	resource := history.resource
	if actor != history.scope.operation.Principal || target.OrganizationID != resource.OrganizationID || target.Type != resource.Type || target.ID != resource.ID || resource.Validate() != nil {
		return authz.AuthorizationSnapshot{}, authz.ErrInvalidSnapshot
	}
	key := historyGrantKey{permission: permission, connectionID: resource.ConnectionID}
	snapshot, exists := history.snapshots[key]
	if !exists {
		var err error
		snapshot, err = loadBindingSnapshot(ctx, history.scope.queries, actor, permission, target, limit)
		if err != nil {
			return authz.AuthorizationSnapshot{}, err
		}
		if snapshot.Resource != resource || snapshot.Revision != history.revision {
			return authz.AuthorizationSnapshot{}, authz.ErrInvalidSnapshot
		}
		if len(history.snapshots) >= history.scope.policy.readSettings.MaxCandidates {
			clear(history.snapshots)
		}
		history.snapshots[key] = snapshot
	}
	snapshot.Resource = resource
	snapshot.EvaluatedAt = history.scope.at
	return snapshot, nil
}

// authorize evaluates each resource separately, including resource-ID predicates, without repeated grant queries.
func (history *historyAuthorization) authorize(ctx context.Context, resource identity.ResourceReference, permission identity.Permission) (authz.ResourceDecision, error) {
	history.resource = resource
	return history.service.AuthorizeResource(ctx, history.scope.operation.Principal, permission, authz.ResourceTarget{OrganizationID: resource.OrganizationID, Type: resource.Type, ID: resource.ID})
}

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

// checkOutput resolves current ancestry in batches and reevaluates all count and page witnesses at fresh database time.
func (history *historyAuthorization) checkOutput(ctx context.Context, queries *db.Queries, at time.Time, decisions []readCheckpoint) error {
	scope := &policyReadScope{policy: history.scope.policy, queries: queries, organization: history.scope.organization, operation: history.scope.operation, at: at}
	current, err := newHistoryAuthorization(ctx, scope)
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
