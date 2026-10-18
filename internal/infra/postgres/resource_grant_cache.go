package postgres

import (
	"context"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// resourceGrantKey identifies grants reusable across trusted rows with the same ancestry.
type resourceGrantKey struct {
	permission   identity.Permission
	connectionID string
}

// resourceGrantCache evaluates row-specific predicates over transaction-local cached grants.
type resourceGrantCache struct {
	scope     *policyReadScope
	revision  int64
	resource  identity.ResourceReference
	snapshots map[resourceGrantKey]authz.AuthorizationSnapshot
	service   *authz.ResourceService
}

// newResourceGrantCache pins membership and revision even when no row is visible.
func newResourceGrantCache(ctx context.Context, scope *policyReadScope) (*resourceGrantCache, error) {
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
	cache := &resourceGrantCache{scope: scope, revision: authority.Revision, snapshots: make(map[resourceGrantKey]authz.AuthorizationSnapshot)}
	cache.service, err = authz.NewResourceService(cache, scope.policy.conditions, scope.policy.catalog, scope.policy.settings)
	return cache, err
}

// LoadAuthorizationSnapshot reuses bounded grants only for ancestry supplied by an organization-scoped row.
func (cache *resourceGrantCache) LoadAuthorizationSnapshot(ctx context.Context, actor identity.UserID, permission identity.Permission, target authz.ResourceTarget, limit int) (authz.AuthorizationSnapshot, error) {
	resource := cache.resource
	if actor != cache.scope.operation.Principal || target.OrganizationID != resource.OrganizationID || target.Type != resource.Type || target.ID != resource.ID || resource.Validate() != nil {
		return authz.AuthorizationSnapshot{}, authz.ErrInvalidSnapshot
	}
	key := resourceGrantKey{permission: permission, connectionID: resource.ConnectionID}
	snapshot, exists := cache.snapshots[key]
	if !exists {
		var err error
		snapshot, err = loadBindingSnapshot(ctx, cache.scope.queries, actor, permission, target, limit)
		if err != nil {
			return authz.AuthorizationSnapshot{}, err
		}
		if snapshot.Resource != resource || snapshot.Revision != cache.revision {
			return authz.AuthorizationSnapshot{}, authz.ErrInvalidSnapshot
		}
		if len(cache.snapshots) >= cache.scope.policy.readSettings.MaxCandidates {
			clear(cache.snapshots)
		}
		cache.snapshots[key] = snapshot
	}
	snapshot.Resource = resource
	snapshot.EvaluatedAt = cache.scope.at
	return snapshot, nil
}

// authorize evaluates each resource separately, including resource-ID predicates, without repeated grant queries.
func (cache *resourceGrantCache) authorize(ctx context.Context, resource identity.ResourceReference, permission identity.Permission) (authz.ResourceDecision, error) {
	cache.resource = resource
	return cache.service.AuthorizeResource(ctx, cache.scope.operation.Principal, permission, authz.ResourceTarget{OrganizationID: resource.OrganizationID, Type: resource.Type, ID: resource.ID})
}
