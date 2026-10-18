package administration

import (
	"context"
	"errors"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// AccountPolicyService administers accounts through binding authority.
type AccountPolicyService struct {
	repository AccountPolicyRepository
	auditor    AuditRecorder
	settings   BindingConfig
}

// CreatedPolicyAccount returns independent membership and a one-time setup link.
type CreatedPolicyAccount struct {
	Account   identity.PolicyAccount
	SetupLink SetupLink
}

// NewAccountPolicyService constructs the Binding-authorized account boundary.
func NewAccountPolicyService(repository AccountPolicyRepository, auditor AuditRecorder, settings BindingConfig) (*AccountPolicyService, error) {
	if repository == nil || auditor == nil {
		return nil, errors.New("administration: account policy dependencies required")
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	return &AccountPolicyService{repository: repository, auditor: auditor, settings: settings}, nil
}

// Create establishes membership without assigning authority.
func (s *AccountPolicyService) Create(ctx context.Context, organization identity.OrganizationID, actor identity.UserID, definition identity.NewPolicyAccount) (CreatedPolicyAccount, error) {
	definition.Email = identity.NormalizeEmail(definition.Email)
	if err := identity.ValidateEmail(definition.Email); err != nil {
		return CreatedPolicyAccount{}, err
	}
	if err := identity.ValidateDisplayName(definition.DisplayName); err != nil {
		return CreatedPolicyAccount{}, err
	}
	token, issue, err := newPasswordSetupIssue()
	if err != nil {
		return CreatedPolicyAccount{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, s.settings.Timeout)
	defer cancel()
	target := administrationTarget{action: audit.ActionUserCreated, targetType: audit.TargetTypeUser}
	var result CreatedPolicyAccount
	err = s.repository.WithAccountPolicyTransaction(bounded, organization, func(transaction AccountPolicyTransaction) error {
		catalog, _, err := loadPolicyAuthority(bounded, transaction, actor, identity.PermissionUsersCreate)
		if err != nil {
			return err
		}
		at, err := transaction.ObservePolicyMutationTime(bounded, nil)
		if err != nil {
			return err
		}
		account, err := transaction.InsertPolicyAccount(bounded, definition)
		if err != nil {
			return err
		}
		setup, err := transaction.ReplacePolicyPasswordSetup(bounded, account.User.ID, issue)
		if err != nil {
			return err
		}
		if err := requirePolicyRecovery(bounded, transaction, catalog); err != nil {
			return err
		}
		event := newSucceededEvent(bounded, actor, organization, target)
		event.TargetID = string(account.User.ID)
		event.OccurredAt = at
		event.Metadata = map[string]any{"initial_binding_count": 0}
		if _, err := transaction.FinalizePolicyChange(bounded, []identity.UserID{account.User.ID}, event); err != nil {
			return err
		}
		result = CreatedPolicyAccount{Account: account, SetupLink: SetupLink{Token: token, ExpiresAt: setup.ExpiresAt}}
		return bindingContextError(bounded)
	})
	if err != nil {
		return CreatedPolicyAccount{}, recordPolicyRefusal(bounded, s.auditor, actor, organization, target, err)
	}
	return result, nil
}

// Disable blocks another account without changing its grants.
func (s *AccountPolicyService) Disable(ctx context.Context, organization identity.OrganizationID, actor, user identity.UserID) (identity.PolicyAccount, error) {
	account, _, err := s.mutateAccount(ctx, organization, actor, user, identity.StatusDisabled, false)
	return account, err
}

// Enable restores another account under complete binding delegation checks.
func (s *AccountPolicyService) Enable(ctx context.Context, organization identity.OrganizationID, actor, user identity.UserID) (identity.PolicyAccount, error) {
	account, _, err := s.mutateAccount(ctx, organization, actor, user, identity.StatusActive, false)
	return account, err
}

// IssueSetupLink replaces a passwordless member's open setup link.
func (s *AccountPolicyService) IssueSetupLink(ctx context.Context, organization identity.OrganizationID, actor, user identity.UserID) (SetupLink, error) {
	_, link, err := s.mutateAccount(ctx, organization, actor, user, "", true)
	return link, err
}

// mutateAccount checks all live grants before changing account state or setup authority.
func (s *AccountPolicyService) mutateAccount(ctx context.Context, organization identity.OrganizationID, actor, user identity.UserID, status identity.UserStatus, issueSetup bool) (identity.PolicyAccount, SetupLink, error) {
	action, gate := audit.ActionUserEnabled, identity.PermissionUsersDisable
	if status == identity.StatusDisabled {
		action = audit.ActionUserDisabled
	}
	if issueSetup {
		action, gate = audit.ActionUserSetupLinkIssued, identity.PermissionUsersUpdate
	}
	target := administrationTarget{action: action, targetType: audit.TargetTypeUser, targetID: string(user)}
	bounded, cancel := context.WithTimeout(ctx, s.settings.Timeout)
	defer cancel()
	var account identity.PolicyAccount
	var link SetupLink
	err := s.repository.WithAccountPolicyTransaction(bounded, organization, func(transaction AccountPolicyTransaction) error {
		catalog, held, err := loadPolicyAuthority(bounded, transaction, actor, gate)
		if err != nil {
			return err
		}
		if err := identity.ValidateNotSelf(actor, user); err != nil {
			return err
		}
		account, err = transaction.LockPolicyAccount(bounded, user)
		if err != nil {
			return err
		}
		affected, err := transaction.LoadAccountBindingPermissions(bounded, user)
		if err != nil {
			return err
		}
		if err := identity.ValidateDelegation(held, affected); err != nil {
			return err
		}
		if !issueSetup && account.User.Status == status {
			return requirePolicyRecovery(bounded, transaction, catalog)
		}
		if issueSetup {
			if account.User.Status != identity.StatusActive {
				return identity.ErrUserDisabled
			}
			if account.HasPassword {
				return identity.ErrPasswordAlreadySet
			}
		}
		at, err := transaction.ObservePolicyMutationTime(bounded, []identity.UserID{user})
		if err != nil {
			return err
		}
		previousStatus := account.User.Status
		if issueSetup {
			token, issue, err := newPasswordSetupIssue()
			if err != nil {
				return err
			}
			setup, err := transaction.ReplacePolicyPasswordSetup(bounded, user, issue)
			if err != nil {
				return err
			}
			link = SetupLink{Token: token, ExpiresAt: setup.ExpiresAt}
		} else {
			if err := transaction.SetPolicyAccountStatus(bounded, user, status); err != nil {
				return err
			}
			account.User.Status = status
		}
		if err := requirePolicyRecovery(bounded, transaction, catalog); err != nil {
			return err
		}
		event := newSucceededEvent(bounded, actor, organization, target)
		event.OccurredAt = at
		event.Metadata = map[string]any{"previous_status": string(previousStatus), "status": string(account.User.Status)}
		if _, err := transaction.FinalizePolicyChange(bounded, []identity.UserID{user}, event); err != nil {
			return err
		}
		return bindingContextError(bounded)
	})
	if err != nil {
		return identity.PolicyAccount{}, SetupLink{}, recordPolicyRefusal(bounded, s.auditor, actor, organization, target, err)
	}
	return account, link, nil
}
