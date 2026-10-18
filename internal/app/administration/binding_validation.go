package administration

import (
	"context"
	"errors"

	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// ValidateCondition checks source under current unconditional binding-management authority without creating a grant.
func (s *BindingService) ValidateCondition(ctx context.Context, org identity.OrganizationID, actor identity.UserID, source identity.Condition) error {
	bounded, cancel := context.WithTimeout(ctx, s.settings.Timeout)
	defer cancel()
	err := s.repository.WithBindingTransaction(bounded, org, func(transaction BindingTransaction) error {
		_, _, err := loadPolicyAuthority(bounded, transaction, actor, identity.PermissionBindingsCreate)
		if errors.Is(err, identity.ErrActorNotAuthorized) {
			_, _, err = loadPolicyAuthority(bounded, transaction, actor, identity.PermissionBindingsUpdate)
		}
		if err != nil {
			return err
		}
		if _, err := s.conditions.Prepare(bounded, source); err != nil {
			return bindingPreparationError(bounded, err)
		}
		return bindingContextError(bounded)
	})
	if err != nil {
		return safeBindingManagementError(bounded, err)
	}
	return nil
}

// bindingPreparationError separates operational refusal from invalid source without exposing diagnostics.
func bindingPreparationError(ctx context.Context, err error) error {
	if failure := bindingContextError(ctx); failure != nil {
		return failure
	}
	var preparation condition.PreparationError
	if errors.As(err, &preparation) && (preparation.Reason == condition.ReasonCancelled || preparation.Reason == condition.ReasonDeadlineExceeded) {
		return ErrBindingManagementUnavailable
	}
	return identity.ErrInvalidCondition
}
