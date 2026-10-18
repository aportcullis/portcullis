package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// InsertBinding creates a deferred head and its first complete immutable version together.
func (transaction *bindingManagementTransaction) InsertBinding(ctx context.Context, definition identity.BindingDefinition) (identity.RoleBinding, error) {
	if transaction.at.IsZero() {
		return identity.RoleBinding{}, administration.ErrBindingManagementUnavailable
	}
	if err := definition.Validate(); err != nil {
		return identity.RoleBinding{}, err
	}
	userID, valid := parseBindingPolicyUUID(string(definition.UserID))
	if !valid {
		return identity.RoleBinding{}, identity.ErrUserNotFound
	}
	bindingID, err := transaction.queries.InsertManagedBindingHead(ctx, db.InsertManagedBindingHeadParams{OrganizationID: transaction.organizationID, UserID: userID, At: timeToTS(transaction.at)})
	if err != nil {
		return identity.RoleBinding{}, err
	}
	binding := identity.RoleBinding{ID: identity.BindingID(uuidToString(bindingID)), OrganizationID: transaction.organization, UserID: definition.UserID, RoleID: definition.RoleID, Version: 1, Scope: definition.Scope, Condition: definition.Condition, HasCondition: definition.HasCondition}
	if err := transaction.insertBindingVersion(ctx, binding); err != nil {
		return identity.RoleBinding{}, err
	}
	return binding, nil
}

// AppendBindingVersion inserts immutable evidence and advances only the expected head version.
func (transaction *bindingManagementTransaction) AppendBindingVersion(ctx context.Context, binding identity.RoleBinding, expectedVersion int64) error {
	if transaction.at.IsZero() {
		return administration.ErrBindingManagementUnavailable
	}
	if binding.OrganizationID != transaction.organization || expectedVersion <= 0 || binding.Version <= expectedVersion || binding.Version-expectedVersion != 1 {
		return identity.ErrBindingConflict
	}
	if err := transaction.insertBindingVersion(ctx, binding); err != nil {
		return onUniqueViolation(err, "", identity.ErrBindingConflict)
	}
	bindingID, _ := parseBindingPolicyUUID(string(binding.ID))
	userID, _ := parseBindingPolicyUUID(string(binding.UserID))
	changed, err := transaction.queries.AdvanceManagedBindingHead(ctx, db.AdvanceManagedBindingHeadParams{OrganizationID: transaction.organizationID, BindingID: bindingID, UserID: userID, ExpectedVersion: expectedVersion, NextVersion: binding.Version})
	if err != nil {
		return err
	}
	if changed != 1 {
		return identity.ErrBindingConflict
	}
	return nil
}

// insertBindingVersion encodes condition absence explicitly and uses the post-lock observation instant.
func (transaction *bindingManagementTransaction) insertBindingVersion(ctx context.Context, binding identity.RoleBinding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	bindingID, valid := parseBindingPolicyUUID(string(binding.ID))
	if !valid {
		return identity.ErrBindingNotFound
	}
	userID, valid := parseBindingPolicyUUID(string(binding.UserID))
	if !valid {
		return identity.ErrUserNotFound
	}
	roleID, valid := parseBindingPolicyUUID(string(binding.RoleID))
	if !valid {
		return identity.ErrRoleNotFound
	}
	var connectionID pgtype.UUID
	if binding.Scope.Type == identity.ScopeConnection {
		connectionID, valid = parseBindingPolicyUUID(binding.Scope.ConnectionID)
		if !valid {
			return identity.ErrInvalidResourceScope
		}
	}
	params := db.InsertManagedBindingVersionParams{BindingID: bindingID, OrganizationID: transaction.organizationID, UserID: userID, Version: binding.Version, RoleID: roleID, ScopeType: string(binding.Scope.Type), ConnectionID: connectionID, At: timeToTS(transaction.at)}
	if binding.HasCondition {
		source := binding.Condition
		params.ConditionTitle = &source.Title
		params.ConditionExpression = &source.Expression
		params.ConditionLanguageVersion = &source.LanguageVersion
		params.ConditionEnvironmentVersion = &source.EnvironmentVersion
		if source.Description != "" {
			params.ConditionDescription = &source.Description
		}
	}
	if binding.Revoked {
		params.RevokedAt = timeToTS(transaction.at)
	}
	return transaction.queries.InsertManagedBindingVersion(ctx, params)
}
