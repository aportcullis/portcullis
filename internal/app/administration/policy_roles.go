package administration

import (
	"context"
	"errors"
	"math"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// RolePolicyService administers shared definitions through unrestricted binding authority.
type RolePolicyService struct {
	repository RolePolicyRepository
	auditor    AuditRecorder
	settings   RolePolicyConfig
}

// NewRolePolicyService constructs the inactive binding-based role boundary.
func NewRolePolicyService(repository RolePolicyRepository, auditor AuditRecorder, settings RolePolicyConfig) (*RolePolicyService, error) {
	if repository == nil || auditor == nil {
		return nil, errors.New("administration: role policy dependencies required")
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	return &RolePolicyService{repository: repository, auditor: auditor, settings: settings}, nil
}

// Create adds a reusable permission bundle.
func (s *RolePolicyService) Create(ctx context.Context, organization identity.OrganizationID, actor identity.UserID, params RoleParams) (identity.Role, error) {
	return s.mutateRole(ctx, organization, actor, "", 0, params, audit.ActionRoleCreated, identity.PermissionRolesCreate)
}

// Update replaces a custom role at the expected version.
func (s *RolePolicyService) Update(ctx context.Context, organization identity.OrganizationID, actor identity.UserID, role identity.RoleID, expectedVersion int64, params RoleParams) (identity.Role, error) {
	return s.mutateRole(ctx, organization, actor, role, expectedVersion, params, audit.ActionRoleUpdated, identity.PermissionRolesUpdate)
}

// Delete soft-deletes a role only after its last live binding is revoked.
func (s *RolePolicyService) Delete(ctx context.Context, organization identity.OrganizationID, actor identity.UserID, role identity.RoleID, expectedVersion int64) error {
	_, err := s.mutateRole(ctx, organization, actor, role, expectedVersion, RoleParams{}, audit.ActionRoleDeleted, identity.PermissionRolesDelete)
	return err
}

// mutateRole checks both permission sets and revokes every live binding subject's sessions atomically.
func (s *RolePolicyService) mutateRole(ctx context.Context, organization identity.OrganizationID, actor identity.UserID, role identity.RoleID, expectedVersion int64, params RoleParams, action audit.Action, gate identity.Permission) (identity.Role, error) {
	bounded, cancel := context.WithTimeout(ctx, s.settings.Timeout)
	defer cancel()
	target := administrationTarget{action: action, targetType: audit.TargetTypeRole, targetID: string(role)}
	var result identity.Role
	err := s.repository.WithRolePolicyTransaction(bounded, organization, func(transaction RolePolicyTransaction) error {
		catalog, held, err := loadPolicyAuthority(bounded, transaction, actor, gate)
		if err != nil {
			return err
		}
		var previous identity.Role
		var subjects []identity.UserID
		if action != audit.ActionRoleCreated {
			previous, err = transaction.LoadRole(bounded, role)
			if err != nil {
				return err
			}
			if err := validateBindingRole(previous, role, catalog); err != nil {
				return err
			}
			if previous.IsSystem {
				return identity.ErrSystemRoleImmutable
			}
			if expectedVersion <= 0 || previous.Version != expectedVersion || expectedVersion == math.MaxInt64 {
				return identity.ErrRoleConflict
			}
			subjects, err = transaction.LoadRoleBindingSubjects(bounded, role, s.settings.MaxSubjects+1)
			if err != nil {
				return err
			}
		}
		if len(subjects) > s.settings.MaxSubjects {
			return ErrBindingManagementUnavailable
		}
		var definition identity.RoleDefinition
		if action != audit.ActionRoleDeleted {
			definition, err = identity.NewRoleDefinition(params.Name, params.Permissions, catalog)
			if err != nil {
				return err
			}
		}
		if err := identity.ValidateDelegation(held, previous.Permissions, definition.Permissions); err != nil {
			return err
		}
		if action == audit.ActionRoleDeleted && len(subjects) > 0 {
			return identity.ErrRoleInUse
		}
		at, err := transaction.ObservePolicyMutationTime(bounded, subjects)
		if err != nil {
			return err
		}
		switch action {
		case audit.ActionRoleCreated:
			result, err = transaction.InsertPolicyRole(bounded, definition)
		case audit.ActionRoleUpdated:
			result, err = transaction.UpdatePolicyRole(bounded, role, expectedVersion, definition)
		case audit.ActionRoleDeleted:
			err = transaction.DeletePolicyRole(bounded, role, expectedVersion)
			result = previous
		}
		if err != nil {
			return err
		}
		if err := requirePolicyRecovery(bounded, transaction, catalog); err != nil {
			return err
		}
		event := newSucceededEvent(bounded, actor, organization, target)
		event.TargetID = string(result.ID)
		event.OccurredAt = at
		added, removed := identity.DiffPermissions(previous.Permissions, definition.Permissions)
		event.Metadata = map[string]any{"previous_role_version": previous.Version, "role_version": result.Version, "added_permissions": permissionKeys(added), "removed_permissions": permissionKeys(removed), "affected_subject_count": len(subjects)}
		if action == audit.ActionRoleDeleted {
			event.Metadata["role_version"] = previous.Version + 1
		}
		if _, err := transaction.FinalizePolicyChange(bounded, subjects, event); err != nil {
			return err
		}
		return bindingContextError(bounded)
	})
	if err != nil {
		return identity.Role{}, recordPolicyRefusal(bounded, s.auditor, actor, organization, target, err)
	}
	return result, nil
}
