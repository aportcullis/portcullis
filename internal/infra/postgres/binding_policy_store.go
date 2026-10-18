package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BindingPolicyStore serves current operation and capability decisions for the server-pinned organization.
type BindingPolicyStore struct {
	pool         *pgxpool.Pool
	policy       *BindingPolicyEvaluator
	organization identity.OrganizationID
}

// NewBindingPolicyStore requires an evaluator and explicit organization, never a legacy permission resolver.
func NewBindingPolicyStore(pool *pgxpool.Pool, policy *BindingPolicyEvaluator, organization identity.OrganizationID) (*BindingPolicyStore, error) {
	if pool == nil || policy == nil || organization == "" {
		return nil, authz.ErrAuthorizationInactive
	}
	return &BindingPolicyStore{pool: pool, policy: policy, organization: organization}, nil
}

// ValidateActivation refuses a partially switched schema before any route is served.
func (store *BindingPolicyStore) ValidateActivation(ctx context.Context) error {
	return store.policy.withPolicyRead(ctx, store.pool, store.organization, func(ctx context.Context, queries *db.Queries, _ time.Time) error {
		active, err := queries.IsBindingAuthorizationActivated(ctx)
		if err != nil {
			return authz.ErrSnapshotUnavailable
		}
		if active == nil || !*active {
			return authz.ErrAuthorizationInactive
		}
		return nil
	})
}

// AuthorizeOperation admits only a current bound resource or a bounded per-candidate scan.
func (store *BindingPolicyStore) AuthorizeOperation(ctx context.Context, actor identity.UserID, permission identity.Permission, operation authz.Operation) error {
	return store.policy.withPolicyRead(ctx, store.pool, store.organization, func(ctx context.Context, queries *db.Queries, at time.Time) error {
		if operation.Collection {
			organizationID, err := policyOrganizationUUID(store.organization)
			if err != nil {
				return err
			}
			userID, valid := parseBindingPolicyUUID(string(actor))
			if !valid {
				return authz.ErrPermissionDenied
			}
			candidate, err := queries.HasBindingPermissionCandidate(ctx, db.HasBindingPermissionCandidateParams{OrganizationID: organizationID, UserID: userID, PermissionKey: string(permission), AllowConnection: operation.Type != identity.ResourceAudit && operation.Type != identity.ResourceOrganization})
			if err != nil {
				return err
			}
			if !candidate {
				return authz.ErrPermissionDenied
			}
			return nil
		}
		_, err := store.policy.authorizeOperation(ctx, queries, store.organization, actor, permission, operation, at)
		return err
	})
}

// SessionCapabilities builds one bounded current advisory view; no predicate result is used as server authority.
func (store *BindingPolicyStore) SessionCapabilities(ctx context.Context, actor identity.UserID) (authz.SessionCapabilities, error) {
	var result authz.SessionCapabilities
	err := store.policy.withPolicyRead(ctx, store.pool, store.organization, func(ctx context.Context, queries *db.Queries, at time.Time) error {
		organizationID, err := policyOrganizationUUID(store.organization)
		if err != nil {
			return err
		}
		userID, valid := parseBindingPolicyUUID(string(actor))
		if !valid {
			return authz.ErrPermissionDenied
		}
		contextRow, err := queries.GetBindingAuthorizationContext(ctx, db.GetBindingAuthorizationContextParams{OrganizationID: organizationID, UserID: userID})
		if err != nil {
			return err
		}
		if !contextRow.ActiveMember || contextRow.Revision <= 0 {
			return authz.ErrPermissionDenied
		}
		result.Revision = contextRow.Revision
		keys, err := queries.PermissionsForUser(ctx, db.PermissionsForUserParams{OrganizationID: organizationID, UserID: userID})
		if err != nil {
			return err
		}
		result.Permissions = toPermissions(keys)
		result.Capabilities = append([]identity.Permission(nil), result.Permissions...)
		bindings, err := queries.ListSessionBindings(ctx, db.ListSessionBindingsParams{OrganizationID: organizationID, UserID: userID, CandidateLimit: store.policy.candidateQueryLimit()})
		if err != nil {
			return err
		}
		if len(bindings) > store.policy.readSettings.MaxCandidates {
			return authz.ErrBindingLimitExceeded
		}
		for _, binding := range bindings {
			result.Bindings = append(result.Bindings, authz.SessionBinding{ID: uuidToString(binding.ID), RoleID: uuidToString(binding.RoleID), RoleName: binding.RoleName, ScopeType: binding.ScopeType, ConnectionID: uuidToString(binding.ConnectionID), HasCondition: binding.HasCondition})
		}
		granted := map[identity.Permission]bool{}
		for _, permission := range result.Capabilities {
			granted[permission] = true
		}
		potential, err := queries.ListSessionPermissionCandidates(ctx, db.ListSessionPermissionCandidatesParams{OrganizationID: organizationID, UserID: userID})
		if err != nil {
			return err
		}
		var pending []identity.Permission
		var kinds []string
		seenKinds := map[identity.ResourceType]bool{}
		for _, key := range potential {
			permission := identity.Permission(key)
			if granted[permission] {
				continue
			}
			pending = append(pending, permission)
			for _, kind := range authz.ResourceTypesForPermission(permission) {
				if !seenKinds[kind] {
					seenKinds[kind] = true
					kinds = append(kinds, string(kind))
				}
			}
		}
		if len(pending) == 0 {
			return nil
		}
		candidates, err := queries.ListCapabilityTargets(ctx, db.ListCapabilityTargetsParams{OrganizationID: organizationID, CandidateLimit: store.policy.candidateQueryLimit(), ResourceTypes: kinds, EvaluatedAt: timeToTS(at)})
		if err != nil {
			return err
		}
		if len(candidates) > store.policy.readSettings.MaxCandidates {
			return authz.ErrBindingLimitExceeded
		}
		for _, permission := range pending {
			for _, candidate := range candidates {
				target := authz.ResourceTarget{OrganizationID: store.organization, Type: identity.ResourceType(candidate.Kind), ID: uuidToString(candidate.ID)}
				if _, err := store.policy.authorize(ctx, queries, actor, permission, target, at); err == nil {
					result.Capabilities = append(result.Capabilities, permission)
					break
				} else if !errors.Is(err, authz.ErrPermissionDenied) && !errors.Is(err, authz.ErrUnknownPermission) {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return authz.SessionCapabilities{}, err
	}
	return result, nil
}
