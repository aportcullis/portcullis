package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BindingPolicyEvaluator shares one resource evaluator across coherent reads and locked workflow admissions.
type BindingPolicyEvaluator struct {
	conditions   authz.ResourceConditions
	catalog      []identity.Permission
	settings     authz.ResourceConfig
	readSettings authz.ReadConfig
}

// NewBindingPolicyEvaluator validates the complete active catalog and injected restricted evaluator.
func NewBindingPolicyEvaluator(conditions authz.ResourceConditions, catalog []identity.Permission, settings authz.ResourceConfig, readSettings authz.ReadConfig) (*BindingPolicyEvaluator, error) {
	if conditions == nil || settings.Validate() != nil || readSettings.Validate() != nil || identity.ValidatePermissionCatalog(catalog) != nil {
		return nil, authz.ErrAuthorizationInactive
	}
	return &BindingPolicyEvaluator{conditions: conditions, catalog: append([]identity.Permission(nil), catalog...), settings: settings, readSettings: readSettings}, nil
}

type transactionResourceSnapshot struct {
	queries *db.Queries
	at      time.Time
}

// candidateQueryLimit includes the overflow witness within the constructor-validated scan ceiling.
func (policy *BindingPolicyEvaluator) candidateQueryLimit() int32 {
	return int32(policy.readSettings.MaxCandidates + 1) //nolint:gosec // validated ceiling is 65536
}

func (snapshot transactionResourceSnapshot) LoadAuthorizationSnapshot(ctx context.Context, actor identity.UserID, permission identity.Permission, target authz.ResourceTarget, limit int) (authz.AuthorizationSnapshot, error) {
	result, err := loadBindingSnapshot(ctx, snapshot.queries, actor, permission, target, limit)
	if err == nil && !snapshot.at.IsZero() {
		result.EvaluatedAt = snapshot.at
	}
	return result, err
}

// authorize evaluates current grants inside the caller's transaction and explicit database instant.
func (policy *BindingPolicyEvaluator) authorize(ctx context.Context, queries *db.Queries, actor identity.UserID, permission identity.Permission, target authz.ResourceTarget, at time.Time) (authz.ResourceDecision, error) {
	if policy == nil {
		return authz.ResourceDecision{}, authz.ErrAuthorizationInactive
	}
	service, err := authz.NewResourceService(transactionResourceSnapshot{queries: queries, at: at}, policy.conditions, policy.catalog, policy.settings)
	if err != nil {
		return authz.ResourceDecision{}, err
	}
	return service.AuthorizeResource(ctx, actor, permission, target)
}

// withPolicyRead owns bounded coherent metadata reads under the shared organization policy lock.
func (policy *BindingPolicyEvaluator) withPolicyRead(ctx context.Context, pool *pgxpool.Pool, organization identity.OrganizationID, read func(context.Context, *db.Queries, time.Time) error) (readError error) {
	return policy.withPolicyReadIsolation(ctx, pool, organization, pgx.ReadCommitted, read)
}

// withPolicyReadIsolation keeps paged history batches on one metadata snapshot.
func (policy *BindingPolicyEvaluator) withPolicyReadIsolation(ctx context.Context, pool *pgxpool.Pool, organization identity.OrganizationID, isolation pgx.TxIsoLevel, read func(context.Context, *db.Queries, time.Time) error) (readError error) {
	if policy == nil {
		return authz.ErrAuthorizationInactive
	}
	bounded, cancel := context.WithTimeout(ctx, policy.readSettings.Timeout)
	defer cancel()
	defer func() {
		if bounded.Err() != nil {
			readError = bounded.Err()
		}
		if errors.Is(readError, context.Canceled) || errors.Is(readError, context.DeadlineExceeded) {
			readError = errors.Join(authz.ErrSnapshotUnavailable, readError)
		}
	}()
	transaction, err := pool.BeginTx(bounded, pgx.TxOptions{IsoLevel: isolation})
	if err != nil {
		return authz.ErrSnapshotUnavailable
	}
	defer func() {
		cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), policy.readSettings.Timeout)
		defer cancelCleanup()
		_ = transaction.Rollback(cleanup)
	}()
	queries := db.New(transaction)
	if err := lockPolicyOrganization(bounded, queries, organization, true); err != nil {
		return err
	}
	at, err := observeInstant(bounded, queries)
	if err != nil {
		return err
	}
	if err := read(bounded, queries, at); err != nil {
		return err
	}
	if err := bounded.Err(); err != nil {
		return err
	}
	return transaction.Commit(bounded)
}

// lockPolicyOrganization precedes connection, request, execution and session locks.
func lockPolicyOrganization(ctx context.Context, queries *db.Queries, organization identity.OrganizationID, shared bool) error {
	if shared {
		return queries.LockSharedPolicyOrganization(ctx, db.LockSharedPolicyOrganizationParams{LockClass: lockClassAdministration, LockObject: organizationLockObject(organization)})
	}
	return queries.LockBindingOrganization(ctx, db.LockBindingOrganizationParams{LockClass: lockClassAdministration, LockObject: organizationLockObject(organization)})
}

// authorizeOperation resolves result ancestry server-side before checking an RPC's current purpose.
func (policy *BindingPolicyEvaluator) authorizeOperation(ctx context.Context, queries *db.Queries, organization identity.OrganizationID, actor identity.UserID, permission identity.Permission, operation authz.Operation, at time.Time) (authz.ResourceDecision, error) {
	identifier := operation.ID
	if operation.ResolveResult {
		requestID, valid := parseBindingPolicyUUID(identifier)
		organizationID, validOrganization := parseBindingPolicyUUID(string(organization))
		if !valid || !validOrganization {
			return authz.ResourceDecision{}, authz.ErrPermissionDenied
		}
		result, err := queries.ResolveExecutionResultIdentity(ctx, db.ResolveExecutionResultIdentityParams{OrganizationID: organizationID, RequestID: requestID})
		if errors.Is(err, pgx.ErrNoRows) {
			return authz.ResourceDecision{}, authz.ErrPermissionDenied
		}
		if err != nil {
			return authz.ResourceDecision{}, err
		}
		identifier = uuidToString(result)
	}
	if identifier == "" && !operation.Collection {
		identifier = string(organization)
	}
	return policy.authorize(ctx, queries, actor, permission, authz.ResourceTarget{OrganizationID: organization, Type: operation.Type, ID: identifier}, at)
}

// policyOrganizationUUID refuses incomplete identities before a database query.
func policyOrganizationUUID(organization identity.OrganizationID) (pgtype.UUID, error) {
	identifier, valid := parseBindingPolicyUUID(string(organization))
	if !valid {
		return pgtype.UUID{}, authz.ErrPermissionDenied
	}
	return identifier, nil
}
