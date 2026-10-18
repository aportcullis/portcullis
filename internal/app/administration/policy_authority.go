package administration

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// policyAuthorityReader is the admission-only slice used by condition validation and policy mutations.
type policyAuthorityReader interface {
	LoadPermissionCatalog(context.Context) ([]identity.Permission, error)
	LockMemberStatus(context.Context, identity.UserID) (identity.UserStatus, error)
	LoadUnconditionalPermissions(context.Context, identity.UserID) ([]identity.Permission, error)
}

// loadPolicyAuthority resolves current catalog and unrestricted authority under the organization lock.
func loadPolicyAuthority(ctx context.Context, transaction policyAuthorityReader, actor identity.UserID, gate identity.Permission) ([]identity.Permission, []identity.Permission, error) {
	catalog, err := transaction.LoadPermissionCatalog(ctx)
	if err != nil {
		return nil, nil, err
	}
	if identity.ValidatePermissionCatalog(catalog) != nil {
		return nil, nil, ErrBindingManagementInactive
	}
	for _, permission := range identity.BindingManagementPermissions() {
		if !slices.Contains(catalog, permission) {
			return nil, nil, ErrBindingManagementInactive
		}
	}
	status, err := transaction.LockMemberStatus(ctx, actor)
	if errors.Is(err, identity.ErrUserNotFound) {
		return nil, nil, identity.ErrActorNotAuthorized
	}
	if err != nil {
		return nil, nil, err
	}
	held, err := transaction.LoadUnconditionalPermissions(ctx, actor)
	if err != nil {
		return nil, nil, err
	}
	if err := (identity.Delegation{Actor: actor, Gate: gate}).Authorize(status == identity.StatusActive, held); err != nil {
		return nil, nil, err
	}
	return catalog, held, nil
}

// requirePolicyRecovery validates the complete current catalog after a tentative mutation.
func requirePolicyRecovery(ctx context.Context, transaction PolicyAuthorityTransaction, catalog []identity.Permission) error {
	recovery, err := transaction.HasRecoveryAdministrator(ctx, identity.SelectRecoveryPermissions(catalog))
	if err != nil {
		return err
	}
	if !recovery {
		return identity.ErrLastAdministrator
	}
	return nil
}

// recordPolicyRefusal strips storage details before applying the bounded refusal audit policy.
func recordPolicyRefusal(ctx context.Context, auditor AuditRecorder, actor identity.UserID, organization identity.OrganizationID, target administrationTarget, err error) error {
	safe := safeBindingManagementError(ctx, err)
	for _, known := range []error{identity.ErrEmailTaken, identity.ErrUserDisabled, identity.ErrPasswordAlreadySet, identity.ErrRoleConflict, identity.ErrSystemRoleImmutable, identity.ErrRoleInUse, identity.ErrRoleNameTaken, identity.ErrInvalidRoleName} {
		if errors.Is(err, known) {
			safe = known
			break
		}
	}
	if failure := bindingContextError(ctx); failure != nil {
		safe = failure
	}
	return recordPrivilegeRefusal(ctx, auditor, slog.Default(), actor, organization, target, safe)
}
