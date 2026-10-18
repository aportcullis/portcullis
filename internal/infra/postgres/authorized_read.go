package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/jackc/pgx/v5"
)

type readCheckpoint struct {
	decision   authz.ResourceDecision
	permission identity.Permission
}

type policyReadScope struct {
	policy       *BindingPolicyEvaluator
	queries      *db.Queries
	organization identity.OrganizationID
	operation    authz.Operation
	at           time.Time
	decisions    []readCheckpoint
	history      *resourceGrantCache
}

// candidate evaluates one current resource before it can enter a list, total or decrypted response.
func (scope *policyReadScope) candidate(ctx context.Context, kind identity.ResourceType, identifier string, permission identity.Permission) (bool, error) {
	decision, err := scope.policy.authorize(ctx, scope.queries, scope.operation.Principal, permission, authz.ResourceTarget{OrganizationID: scope.organization, Type: kind, ID: identifier}, scope.at)
	if errors.Is(err, authz.ErrPermissionDenied) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	scope.decisions = append(scope.decisions, readCheckpoint{decision: decision, permission: permission})
	return true, nil
}

// withAuthorizedRead resolves server-selected purpose and registers fresh checks for every disclosed resource.
func (store *ConnectionStore) withAuthorizedRead(ctx context.Context, organization identity.OrganizationID, read func(context.Context, *policyReadScope) error) error {
	return store.withAuthorizedReadIsolation(ctx, organization, pgx.ReadCommitted, read)
}

// withAuthorizedHistoryRead holds one snapshot across all history batches and page clamps.
func (store *ConnectionStore) withAuthorizedHistoryRead(ctx context.Context, organization identity.OrganizationID, read func(context.Context, *policyReadScope) error) error {
	return store.withAuthorizedReadIsolation(ctx, organization, pgx.RepeatableRead, read)
}

// withAuthorizedReadIsolation retains current policy fences for both snapshot and ordinary reads.
func (store *ConnectionStore) withAuthorizedReadIsolation(ctx context.Context, organization identity.OrganizationID, isolation pgx.TxIsoLevel, read func(context.Context, *policyReadScope) error) error {
	operation, exists := authz.OperationFromContext(ctx)
	if !exists || operation.Permission == "" || store.organization != organization {
		return authz.ErrPermissionDenied
	}
	var decisions []readCheckpoint
	var history *resourceGrantCache
	err := store.policy.withPolicyReadIsolation(ctx, store.pool, organization, isolation, func(ctx context.Context, queries *db.Queries, at time.Time) error {
		scope := &policyReadScope{policy: store.policy, queries: queries, organization: organization, operation: operation, at: at}
		if operation.Collection {
			organizationID, err := policyOrganizationUUID(organization)
			if err != nil {
				return err
			}
			principalID, valid := parseBindingPolicyUUID(string(operation.Principal))
			if !valid {
				return authz.ErrPermissionDenied
			}
			admitted, err := queries.HasBindingPermissionCandidate(ctx, db.HasBindingPermissionCandidateParams{OrganizationID: organizationID, UserID: principalID, PermissionKey: string(operation.Permission), AllowConnection: operation.Type != identity.ResourceAudit && operation.Type != identity.ResourceOrganization})
			if err != nil {
				return err
			}
			if !admitted {
				return authz.ErrPermissionDenied
			}
		}
		if !operation.Collection {
			decision, err := store.policy.authorizeOperation(ctx, queries, organization, operation.Principal, operation.Permission, operation, at)
			if err != nil {
				return err
			}
			scope.decisions = append(scope.decisions, readCheckpoint{decision: decision, permission: operation.Permission})
		}
		if isolation == pgx.RepeatableRead {
			var err error
			history, err = newResourceGrantCache(ctx, scope)
			if err != nil {
				return err
			}
			scope.history = history
		}
		if err := read(ctx, scope); err != nil {
			return err
		}
		decisions = scope.decisions
		return nil
	})
	if err != nil {
		return err
	}
	authz.RegisterOutputCheck(ctx, func(checkContext context.Context) error {
		return store.policy.withPolicyRead(checkContext, store.pool, organization, func(checkContext context.Context, queries *db.Queries, at time.Time) error {
			if history != nil {
				return history.checkHistoryOutput(checkContext, queries, at, decisions)
			}
			for _, previous := range decisions {
				current, err := store.policy.authorize(checkContext, queries, operation.Principal, previous.permission, authz.ResourceTarget{OrganizationID: organization, Type: previous.decision.Resource.Type, ID: previous.decision.Resource.ID}, at)
				if err != nil {
					return err
				}
				if current.Revision != previous.decision.Revision {
					return authz.ErrPermissionDenied
				}
			}
			return nil
		})
	})
	return nil
}
