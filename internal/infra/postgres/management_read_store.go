package postgres

import (
	"context"
	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

// ManagementReadStore reads policy administration data coherently.
type ManagementReadStore struct {
	pool         *pgxpool.Pool
	policy       *BindingPolicyEvaluator
	organization identity.OrganizationID
}

// NewManagementReadStore constructs the historical snapshot reader; runtime uses NewAuthorizedManagementReadStore.
func NewManagementReadStore(pool *pgxpool.Pool) *ManagementReadStore {
	return &ManagementReadStore{pool: pool}
}

// NewAuthorizedManagementReadStore pins coordinated policy reads to the server organization.
func NewAuthorizedManagementReadStore(pool *pgxpool.Pool, policy *BindingPolicyEvaluator, organization identity.OrganizationID) (*ManagementReadStore, error) {
	if pool == nil || policy == nil || organization == "" {
		return nil, authz.ErrAuthorizationInactive
	}
	return &ManagementReadStore{pool: pool, policy: policy, organization: organization}, nil
}

// WithManagementRead binds authorization and rows to one metadata snapshot.
func (s *ManagementReadStore) WithManagementRead(ctx context.Context, org identity.OrganizationID, read func(context.Context, administration.ManagementReadSnapshot) error) error {
	if err := bindingPolicyContextError(ctx); err != nil {
		return err
	}
	organizationID, valid := parseBindingPolicyUUID(string(org))
	if !valid {
		return administration.ErrBindingManagementInactive
	}
	if s.policy != nil {
		if org != s.organization {
			return authz.ErrPermissionDenied
		}
		return s.withAuthorizedManagementRead(ctx, organizationID, read)
	}
	transaction, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return bindingReadError(ctx)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	snapshot := managementReadSnapshot{queries: db.New(transaction), organization: org, organizationID: organizationID}
	if err := read(ctx, &snapshot); err != nil {
		return err
	}
	if err := bindingPolicyContextError(ctx); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

// managementReadSnapshot exposes only read operations over independent policy data.
type managementReadSnapshot struct {
	queries        *db.Queries
	organization   identity.OrganizationID
	organizationID pgtype.UUID
	at             time.Time
	checkpoints    []managementReadCheckpoint
}

// LoadAuthorizationSnapshot keeps the read decision and all returned data in the same database snapshot.
func (snapshot *managementReadSnapshot) LoadAuthorizationSnapshot(ctx context.Context, actor identity.UserID, permission identity.Permission, target authz.ResourceTarget, maxBindings int) (authz.AuthorizationSnapshot, error) {
	if target.OrganizationID != snapshot.organization {
		return authz.AuthorizationSnapshot{}, authz.ErrPermissionDenied
	}
	result, err := loadBindingSnapshot(ctx, snapshot.queries, actor, permission, target, maxBindings)
	if err == nil && !snapshot.at.IsZero() {
		result.EvaluatedAt = snapshot.at
		snapshot.checkpoints = append(snapshot.checkpoints, managementReadCheckpoint{actor: actor, permission: permission, target: target, revision: result.Revision})
	}
	return result, err
}

// ListManagementIDs returns a bounded stable page and its authorized organization count.
func (snapshot *managementReadSnapshot) ListManagementIDs(ctx context.Context, kind identity.ResourceType, offset int64, limit int32) ([]string, int64, error) {
	if limit <= 0 || limit > 100 || offset < 0 {
		return nil, 0, administration.ErrInvalidManagementPage
	}
	rows, err := snapshot.queries.ListManagementIDs(ctx, db.ListManagementIDsParams{Kind: string(kind), OrganizationID: snapshot.organizationID, PageLimit: limit, PageOffset: offset})
	if err != nil {
		return nil, 0, err
	}
	total, err := snapshot.queries.CountManagementRows(ctx, db.CountManagementRowsParams{Kind: string(kind), OrganizationID: snapshot.organizationID})
	if err != nil {
		return nil, 0, err
	}
	identifiers := make([]string, 0, len(rows))
	for _, identifier := range rows {
		identifiers = append(identifiers, uuidToString(identifier))
	}
	return identifiers, total, nil
}

// GetPolicyAccount reads account details without requiring a legacy role assignment.
func (snapshot *managementReadSnapshot) GetPolicyAccount(ctx context.Context, user identity.UserID) (identity.PolicyAccount, error) {
	userID, valid := parseBindingPolicyUUID(string(user))
	if !valid {
		return identity.PolicyAccount{}, identity.ErrUserNotFound
	}
	row, err := snapshot.queries.GetManagementAccount(ctx, db.GetManagementAccountParams{OrganizationID: snapshot.organizationID, UserID: userID})
	if err != nil {
		return identity.PolicyAccount{}, notFound(err, identity.ErrUserNotFound)
	}
	return identity.PolicyAccount{User: identity.User{ID: identity.UserID(uuidToString(row.ID)), Email: row.Email, DisplayName: row.DisplayName, Status: identity.UserStatus(row.Status), CreatedAt: tsToTime(row.CreatedAt)}, HasPassword: row.HasPassword}, nil
}

// GetPolicyRole reads a live role and current binding usage without taking a write lock.
func (snapshot *managementReadSnapshot) GetPolicyRole(ctx context.Context, role identity.RoleID) (identity.Role, error) {
	roleID, valid := parseBindingPolicyUUID(string(role))
	if !valid {
		return identity.Role{}, identity.ErrRoleNotFound
	}
	row, err := snapshot.queries.GetBindingPolicyRole(ctx, db.GetBindingPolicyRoleParams{OrganizationID: snapshot.organizationID, RoleID: roleID})
	if err != nil {
		return identity.Role{}, notFound(err, identity.ErrRoleNotFound)
	}
	return identity.Role{ID: identity.RoleID(uuidToString(row.ID)), Name: row.Name, IsSystem: row.IsSystem, Version: row.Version, Permissions: toPermissions(row.Permissions), BindingCount: row.BindingCount}, nil
}

// GetPolicyBinding preserves the complete current condition source, including unsupported contracts, for authorized detail reads.
func (snapshot *managementReadSnapshot) GetPolicyBinding(ctx context.Context, binding identity.BindingID) (identity.RoleBinding, error) {
	bindingID, valid := parseBindingPolicyUUID(string(binding))
	if !valid {
		return identity.RoleBinding{}, identity.ErrBindingNotFound
	}
	row, err := snapshot.queries.GetManagementBinding(ctx, db.GetManagementBindingParams{OrganizationID: snapshot.organizationID, BindingID: bindingID})
	if err != nil {
		return identity.RoleBinding{}, notFound(err, identity.ErrBindingNotFound)
	}
	result := identity.RoleBinding{ID: identity.BindingID(uuidToString(row.BindingID)), OrganizationID: identity.OrganizationID(uuidToString(row.OrganizationID)), UserID: identity.UserID(uuidToString(row.UserID)), RoleID: identity.RoleID(uuidToString(row.RoleID)), Version: row.Version, Scope: identity.ResourceScope{Type: identity.ScopeType(row.ScopeType), ConnectionID: uuidToString(row.ConnectionID)}, Revoked: row.RevokedAt.Valid}
	result.Condition, result.HasCondition, err = decodeBindingCondition(row.ConditionTitle, row.ConditionDescription, row.ConditionExpression, row.ConditionLanguageVersion, row.ConditionEnvironmentVersion)
	return result, err
}

// LoadPermissionCatalog reads current vocabulary in the same snapshot as its grant decision.
func (snapshot *managementReadSnapshot) LoadPermissionCatalog(ctx context.Context) ([]identity.Permission, error) {
	keys, err := snapshot.queries.ListPermissionKeys(ctx)
	return toPermissions(keys), err
}
