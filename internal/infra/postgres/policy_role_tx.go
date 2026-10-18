package postgres

import (
	"context"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// LoadRoleBindingSubjects locks distinct subjects of all live grants, including conditional and scoped bindings.
func (transaction *bindingManagementTransaction) LoadRoleBindingSubjects(ctx context.Context, role identity.RoleID, limit int) ([]identity.UserID, error) {
	roleID, valid := parseBindingPolicyUUID(string(role))
	if !valid {
		return nil, identity.ErrRoleNotFound
	}
	identifiers, err := transaction.queries.GetRoleBindingSubjects(ctx, db.GetRoleBindingSubjectsParams{OrganizationID: transaction.organizationID, RoleID: roleID, SubjectLimit: int32(limit)})
	if err != nil {
		return nil, err
	}
	subjects := make([]identity.UserID, 0, len(identifiers))
	for _, identifier := range identifiers {
		subjects = append(subjects, identity.UserID(uuidToString(identifier)))
	}
	return subjects, nil
}

// InsertPolicyRole persists a reusable definition without assigning it to a member.
func (transaction *bindingManagementTransaction) InsertPolicyRole(ctx context.Context, definition identity.RoleDefinition) (identity.Role, error) {
	if transaction.at.IsZero() {
		return identity.Role{}, administration.ErrBindingManagementUnavailable
	}
	roleID, err := transaction.queries.InsertRole(ctx, db.InsertRoleParams{OrganizationID: transaction.organizationID, Name: definition.Name, At: timeToTS(transaction.at)})
	if err != nil {
		return identity.Role{}, onUniqueViolation(err, rolesOrgNameIndex, identity.ErrRoleNameTaken)
	}
	if err := transaction.queries.GrantRolePermissions(ctx, db.GrantRolePermissionsParams{RoleID: roleID, PermissionKeys: permissionStrings(definition.Permissions)}); err != nil {
		return identity.Role{}, err
	}
	return transaction.LoadRole(ctx, identity.RoleID(uuidToString(roleID)))
}

// UpdatePolicyRole replaces permissions with optimistic version enforcement and retained soft-delete evidence.
func (transaction *bindingManagementTransaction) UpdatePolicyRole(ctx context.Context, role identity.RoleID, expectedVersion int64, definition identity.RoleDefinition) (identity.Role, error) {
	roleID, valid := parseBindingPolicyUUID(string(role))
	if !valid {
		return identity.Role{}, identity.ErrRoleNotFound
	}
	if transaction.at.IsZero() {
		return identity.Role{}, administration.ErrBindingManagementUnavailable
	}
	changed, err := transaction.queries.UpdateCustomRole(ctx, db.UpdateCustomRoleParams{OrganizationID: transaction.organizationID, RoleID: roleID, ExpectedVersion: expectedVersion, Name: definition.Name})
	if err != nil {
		return identity.Role{}, onUniqueViolation(err, rolesOrgNameIndex, identity.ErrRoleNameTaken)
	}
	if changed != 1 {
		return identity.Role{}, identity.ErrRoleConflict
	}
	keys := permissionStrings(definition.Permissions)
	if err := transaction.queries.SoftDeleteRemovedRolePermissions(ctx, db.SoftDeleteRemovedRolePermissionsParams{RoleID: roleID, PermissionKeys: keys, At: timeToTS(transaction.at)}); err != nil {
		return identity.Role{}, err
	}
	if err := transaction.queries.GrantRolePermissions(ctx, db.GrantRolePermissionsParams{RoleID: roleID, PermissionKeys: keys}); err != nil {
		return identity.Role{}, err
	}
	return transaction.LoadRole(ctx, role)
}

// DeletePolicyRole refuses any current binding reference without consulting legacy assignment columns.
func (transaction *bindingManagementTransaction) DeletePolicyRole(ctx context.Context, role identity.RoleID, expectedVersion int64) error {
	roleID, valid := parseBindingPolicyUUID(string(role))
	if !valid {
		return identity.ErrRoleNotFound
	}
	if transaction.at.IsZero() {
		return administration.ErrBindingManagementUnavailable
	}
	changed, err := transaction.queries.SoftDeletePolicyRole(ctx, db.SoftDeletePolicyRoleParams{OrganizationID: transaction.organizationID, RoleID: roleID, ExpectedVersion: expectedVersion, At: timeToTS(transaction.at)})
	if err != nil {
		return err
	}
	if changed != 1 {
		return identity.ErrRoleConflict
	}
	return nil
}
