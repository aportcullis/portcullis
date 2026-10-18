package postgres

import (
	"context"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// LockPolicyAccount reads membership independently from any assigned legacy role.
func (transaction *bindingManagementTransaction) LockPolicyAccount(ctx context.Context, user identity.UserID) (identity.PolicyAccount, error) {
	userID, valid := parseBindingPolicyUUID(string(user))
	if !valid {
		return identity.PolicyAccount{}, identity.ErrUserNotFound
	}
	row, err := transaction.queries.LockPolicyAccount(ctx, db.LockPolicyAccountParams{OrganizationID: transaction.organizationID, UserID: userID})
	if err != nil {
		return identity.PolicyAccount{}, notFound(err, identity.ErrUserNotFound)
	}
	return identity.PolicyAccount{User: identity.User{ID: identity.UserID(uuidToString(row.ID)), Email: row.Email, DisplayName: row.DisplayName, Status: identity.UserStatus(row.Status), CreatedAt: tsToTime(row.CreatedAt)}, HasPassword: row.HasPassword}, nil
}

// LoadAccountBindingPermissions includes every non-revoked grant without evaluating or flattening delegation authority.
func (transaction *bindingManagementTransaction) LoadAccountBindingPermissions(ctx context.Context, user identity.UserID) ([]identity.Permission, error) {
	userID, valid := parseBindingPolicyUUID(string(user))
	if !valid {
		return nil, identity.ErrUserNotFound
	}
	keys, err := transaction.queries.GetAccountBindingPermissions(ctx, db.GetAccountBindingPermissionsParams{OrganizationID: transaction.organizationID, UserID: userID})
	return toPermissions(keys), err
}

// InsertPolicyAccount creates an independent membership with no implicit grants.
func (transaction *bindingManagementTransaction) InsertPolicyAccount(ctx context.Context, definition identity.NewPolicyAccount) (identity.PolicyAccount, error) {
	if transaction.at.IsZero() {
		return identity.PolicyAccount{}, administration.ErrBindingManagementUnavailable
	}
	userID, err := transaction.queries.InsertPolicyUser(ctx, db.InsertPolicyUserParams{Email: definition.Email, DisplayName: definition.DisplayName, At: timeToTS(transaction.at)})
	if err != nil {
		return identity.PolicyAccount{}, onUniqueViolation(err, usersEmailLowerIndex, identity.ErrEmailTaken)
	}
	if err := transaction.queries.InsertIndependentMembership(ctx, db.InsertIndependentMembershipParams{OrganizationID: transaction.organizationID, UserID: userID, At: timeToTS(transaction.at)}); err != nil {
		return identity.PolicyAccount{}, err
	}
	return transaction.LockPolicyAccount(ctx, identity.UserID(uuidToString(userID)))
}

// SetPolicyAccountStatus changes status while retaining all binding and credential history.
func (transaction *bindingManagementTransaction) SetPolicyAccountStatus(ctx context.Context, user identity.UserID, status identity.UserStatus) error {
	userID, valid := parseBindingPolicyUUID(string(user))
	if !valid {
		return identity.ErrUserNotFound
	}
	if transaction.at.IsZero() {
		return administration.ErrBindingManagementUnavailable
	}
	if err := transaction.queries.SetUserStatus(ctx, db.SetUserStatusParams{UserID: userID, Status: string(status)}); err != nil {
		return err
	}
	if status == identity.StatusDisabled {
		return transaction.queries.RevokeOpenPasswordSetups(ctx, db.RevokeOpenPasswordSetupsParams{At: timeToTS(transaction.at), UserID: userID})
	}
	return nil
}

// ReplacePolicyPasswordSetup retains only a digest and measures expiry from the post-lock instant.
func (transaction *bindingManagementTransaction) ReplacePolicyPasswordSetup(ctx context.Context, user identity.UserID, issue identity.PasswordSetupIssue) (identity.PasswordSetup, error) {
	userID, valid := parseBindingPolicyUUID(string(user))
	if !valid {
		return identity.PasswordSetup{}, identity.ErrUserNotFound
	}
	if transaction.at.IsZero() {
		return identity.PasswordSetup{}, administration.ErrBindingManagementUnavailable
	}
	if err := transaction.queries.RevokeOpenPasswordSetups(ctx, db.RevokeOpenPasswordSetupsParams{At: timeToTS(transaction.at), UserID: userID}); err != nil {
		return identity.PasswordSetup{}, err
	}
	expiry, err := transaction.queries.InsertPasswordSetupToken(ctx, db.InsertPasswordSetupTokenParams{OrganizationID: transaction.organizationID, UserID: userID, TokenHash: issue.TokenHash, At: timeToTS(transaction.at), ValiditySeconds: issue.Validity.Seconds()})
	return identity.PasswordSetup{UserID: user, OrganizationID: transaction.organization, ExpiresAt: tsToTime(expiry)}, err
}
