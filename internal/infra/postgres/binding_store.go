package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// BindingStore reads prepared bindings without granting runtime mutation privileges.
type BindingStore struct{ pool *pgxpool.Pool }

// NewBindingStore constructs a resource authorization snapshot reader.
func NewBindingStore(pool *pgxpool.Pool) *BindingStore { return &BindingStore{pool: pool} }

// LoadAuthorizationSnapshot reads membership, ancestry and bounded current grants together.
func (s *BindingStore) LoadAuthorizationSnapshot(ctx context.Context, principal identity.UserID, permission identity.Permission, target authz.ResourceTarget, maxBindings int) (authz.AuthorizationSnapshot, error) {
	if maxBindings <= 0 || maxBindings > authz.MaxResourceBindings {
		return authz.AuthorizationSnapshot{}, authz.ErrInvalidSnapshot
	}
	organization, err := stringToUUID(string(target.OrganizationID))
	if err != nil || !organization.Valid {
		return authz.AuthorizationSnapshot{}, authz.ErrPermissionDenied
	}
	user, err := stringToUUID(string(principal))
	if err != nil || !user.Valid {
		return authz.AuthorizationSnapshot{}, authz.ErrPermissionDenied
	}
	resourceID, err := stringToUUID(target.ID)
	if err != nil || !resourceID.Valid {
		return authz.AuthorizationSnapshot{}, authz.ErrPermissionDenied
	}
	transaction, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return authz.AuthorizationSnapshot{}, bindingReadError(ctx)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	queries := db.New(transaction)
	authority, err := queries.GetBindingAuthorizationContext(ctx, db.GetBindingAuthorizationContextParams{OrganizationID: organization, UserID: user})
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.AuthorizationSnapshot{}, authz.ErrPermissionDenied
	}
	if err != nil {
		return authz.AuthorizationSnapshot{}, bindingReadError(ctx)
	}
	if !authority.ActiveMember {
		return authz.AuthorizationSnapshot{}, authz.ErrPermissionDenied
	}
	if authority.Revision <= 0 {
		return authz.AuthorizationSnapshot{}, authz.ErrAuthorizationInactive
	}
	resources, err := queries.ResolveBindingResource(ctx, db.ResolveBindingResourceParams{ResourceType: string(target.Type), OrganizationID: organization, ResourceID: resourceID})
	if err != nil {
		return authz.AuthorizationSnapshot{}, bindingReadError(ctx)
	}
	if len(resources) == 0 {
		return authz.AuthorizationSnapshot{}, authz.ErrPermissionDenied
	}
	if len(resources) != 1 {
		return authz.AuthorizationSnapshot{}, authz.ErrInvalidSnapshot
	}
	resource := identity.ResourceReference{OrganizationID: target.OrganizationID, Type: target.Type, ID: uuidToString(resources[0].ResourceID), ConnectionID: uuidToString(resources[0].ConnectionID)}
	if resource.Validate() != nil {
		return authz.AuthorizationSnapshot{}, authz.ErrInvalidSnapshot
	}
	rows, err := queries.ListResourceBindingGrants(ctx, db.ListResourceBindingGrantsParams{OrganizationID: organization, UserID: user, PermissionKey: string(permission), ConnectionID: resources[0].ConnectionID, CandidateLimit: int32(maxBindings + 1)})
	if err != nil {
		return authz.AuthorizationSnapshot{}, bindingReadError(ctx)
	}
	if len(rows) > maxBindings {
		return authz.AuthorizationSnapshot{}, authz.ErrBindingLimitExceeded
	}
	snapshot := authz.AuthorizationSnapshot{ActiveMember: true, Resource: resource, EvaluatedAt: tsToTime(authority.EvaluatedAt), Revision: authority.Revision, Grants: make([]authz.BindingGrant, 0, len(rows))}
	for _, row := range rows {
		grant, decodeErr := decodeBindingGrant(row, permission)
		if decodeErr != nil {
			return authz.AuthorizationSnapshot{}, decodeErr
		}
		snapshot.Grants = append(snapshot.Grants, grant)
	}
	if err := transaction.Commit(ctx); err != nil {
		return authz.AuthorizationSnapshot{}, bindingReadError(ctx)
	}
	return snapshot, nil
}

// bindingReadError keeps driver details outside the authorization boundary.
func bindingReadError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return authz.ErrSnapshotUnavailable
}

// decodeBindingGrant preserves explicit condition absence and immutable version evidence.
func decodeBindingGrant(row db.ListResourceBindingGrantsRow, permission identity.Permission) (authz.BindingGrant, error) {
	binding := identity.RoleBinding{ID: identity.BindingID(uuidToString(row.BindingID)), OrganizationID: identity.OrganizationID(uuidToString(row.OrganizationID)), UserID: identity.UserID(uuidToString(row.UserID)), RoleID: identity.RoleID(uuidToString(row.RoleID)), Version: row.Version, Scope: identity.ResourceScope{Type: identity.ScopeType(row.ScopeType), ConnectionID: uuidToString(row.ConnectionID)}, Revoked: row.RevokedAt.Valid}
	if row.ConditionExpression != nil {
		if row.ConditionTitle == nil || row.ConditionLanguageVersion == nil || row.ConditionEnvironmentVersion == nil {
			return authz.BindingGrant{}, authz.ErrInvalidSnapshot
		}
		binding.HasCondition = true
		binding.Condition = identity.Condition{Title: *row.ConditionTitle, Expression: *row.ConditionExpression, LanguageVersion: *row.ConditionLanguageVersion, EnvironmentVersion: *row.ConditionEnvironmentVersion}
		if row.ConditionDescription != nil {
			binding.Condition.Description = *row.ConditionDescription
		}
	} else if row.ConditionTitle != nil || row.ConditionDescription != nil || row.ConditionLanguageVersion != nil || row.ConditionEnvironmentVersion != nil {
		return authz.BindingGrant{}, authz.ErrInvalidSnapshot
	}
	return authz.BindingGrant{Binding: binding, Permission: permission, RoleVersion: row.RoleVersion}, nil
}
