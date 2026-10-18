package postgres

import (
	"context"
	"time"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// LoadPermissionCatalog reads current policy vocabulary without deriving authority from membership roles.
func (transaction *bindingManagementTransaction) LoadPermissionCatalog(ctx context.Context) ([]identity.Permission, error) {
	keys, err := transaction.queries.ListPermissionKeys(ctx)
	return toPermissions(keys), err
}

// LockMemberStatus locks existing membership references before observing mutation time.
func (transaction *bindingManagementTransaction) LockMemberStatus(ctx context.Context, user identity.UserID) (identity.UserStatus, error) {
	userID, valid := parseBindingPolicyUUID(string(user))
	if !valid {
		return "", identity.ErrUserNotFound
	}
	status, err := transaction.queries.LockBindingMember(ctx, db.LockBindingMemberParams{OrganizationID: transaction.organizationID, UserID: userID})
	return identity.UserStatus(status), notFound(err, identity.ErrUserNotFound)
}

// LoadUnconditionalPermissions returns only current unrestricted organization grants.
func (transaction *bindingManagementTransaction) LoadUnconditionalPermissions(ctx context.Context, user identity.UserID) ([]identity.Permission, error) {
	userID, valid := parseBindingPolicyUUID(string(user))
	if !valid {
		return nil, identity.ErrActorNotAuthorized
	}
	permissions, err := transaction.queries.GetUnconditionalBindingPermissions(ctx, db.GetUnconditionalBindingPermissionsParams{OrganizationID: transaction.organizationID, UserID: userID})
	return toPermissions(permissions), err
}

// LoadRole locks a live definition before loading its current permission bundle.
func (transaction *bindingManagementTransaction) LoadRole(ctx context.Context, role identity.RoleID) (identity.Role, error) {
	roleID, valid := parseBindingPolicyUUID(string(role))
	if !valid {
		return identity.Role{}, identity.ErrRoleNotFound
	}
	if _, err := transaction.queries.LockBindingRole(ctx, db.LockBindingRoleParams{OrganizationID: transaction.organizationID, RoleID: roleID}); err != nil {
		return identity.Role{}, notFound(err, identity.ErrRoleNotFound)
	}
	definition, err := transaction.queries.GetBindingPolicyRole(ctx, db.GetBindingPolicyRoleParams{OrganizationID: transaction.organizationID, RoleID: roleID})
	if err != nil {
		return identity.Role{}, notFound(err, identity.ErrRoleNotFound)
	}
	return identity.Role{ID: identity.RoleID(uuidToString(definition.ID)), Name: definition.Name, IsSystem: definition.IsSystem, Version: definition.Version, Permissions: toPermissions(definition.Permissions), BindingCount: definition.BindingCount}, nil
}

// LockBindingVersion locks the head and loads its immutable current version without hiding revocation.
func (transaction *bindingManagementTransaction) LockBindingVersion(ctx context.Context, binding identity.BindingID) (identity.RoleBinding, error) {
	bindingID, valid := parseBindingPolicyUUID(string(binding))
	if !valid {
		return identity.RoleBinding{}, identity.ErrBindingNotFound
	}
	row, err := transaction.queries.GetManagedBinding(ctx, db.GetManagedBindingParams{OrganizationID: transaction.organizationID, BindingID: bindingID})
	if err != nil {
		return identity.RoleBinding{}, notFound(err, identity.ErrBindingNotFound)
	}
	result := identity.RoleBinding{ID: identity.BindingID(uuidToString(row.BindingID)), OrganizationID: identity.OrganizationID(uuidToString(row.OrganizationID)), UserID: identity.UserID(uuidToString(row.UserID)), RoleID: identity.RoleID(uuidToString(row.RoleID)), Version: row.Version, Scope: identity.ResourceScope{Type: identity.ScopeType(row.ScopeType), ConnectionID: uuidToString(row.ConnectionID)}, Revoked: row.RevokedAt.Valid}
	result.Condition, result.HasCondition, err = decodeBindingCondition(row.ConditionTitle, row.ConditionDescription, row.ConditionExpression, row.ConditionLanguageVersion, row.ConditionEnvironmentVersion)
	return result, err
}

// LockBindingScope locks the exact connection reference without making archive state an authorization bypass.
func (transaction *bindingManagementTransaction) LockBindingScope(ctx context.Context, scope identity.ResourceScope) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if scope.Type == identity.ScopeOrganization {
		return nil
	}
	connectionID, valid := parseBindingPolicyUUID(scope.ConnectionID)
	if !valid {
		return identity.ErrInvalidResourceScope
	}
	_, err := transaction.queries.LockBindingScopeConnection(ctx, db.LockBindingScopeConnectionParams{OrganizationID: transaction.organizationID, ConnectionID: connectionID})
	return notFound(err, identity.ErrInvalidResourceScope)
}

// ObserveBindingMutationTime serializes session issuance and locks affected rows before observing wall time.
func (transaction *bindingManagementTransaction) ObserveBindingMutationTime(ctx context.Context, user identity.UserID) (time.Time, error) {
	return transaction.ObservePolicyMutationTime(ctx, []identity.UserID{user})
}

// HasRecoveryAdministrator tests all catalog administration keys across the bindings of each active member.
func (transaction *bindingManagementTransaction) HasRecoveryAdministrator(ctx context.Context, required []identity.Permission) (bool, error) {
	if len(required) == 0 {
		return false, administration.ErrBindingManagementInactive
	}
	return transaction.queries.HasBindingRecoveryAdministrator(ctx, db.HasBindingRecoveryAdministratorParams{OrganizationID: transaction.organizationID, RequiredPermissions: permissionStrings(required)})
}

// FinalizeBindingChange revokes sessions and commits revision and audit with the grant mutation.
func (transaction *bindingManagementTransaction) FinalizeBindingChange(ctx context.Context, user identity.UserID, event audit.Event) (int64, error) {
	return transaction.FinalizePolicyChange(ctx, []identity.UserID{user}, event)
}
