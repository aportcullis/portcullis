package administration

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// BindingService administers structured grants without using legacy membership authority.
type BindingService struct {
	repository BindingRepository
	conditions BindingConditionPreparer
	auditor    AuditRecorder
	settings   BindingConfig
}

// BindingMutation records the committed grant version and organization policy revision.
type BindingMutation struct {
	Binding    identity.RoleBinding
	Revision   int64
	OccurredAt time.Time
}

// NewBindingService checks the transactional, condition and refusal audit dependencies.
func NewBindingService(repository BindingRepository, conditions BindingConditionPreparer, auditor AuditRecorder, settings BindingConfig) (*BindingService, error) {
	if repository == nil || conditions == nil || auditor == nil {
		return nil, errors.New("administration: binding dependencies required")
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	return &BindingService{repository: repository, conditions: conditions, auditor: auditor, settings: settings}, nil
}

// Create grants a role to another existing member at explicit scope.
func (s *BindingService) Create(ctx context.Context, organization identity.OrganizationID, actor identity.UserID, definition identity.BindingDefinition) (BindingMutation, error) {
	return s.mutateBinding(ctx, organization, actor, bindingCommand{action: audit.ActionBindingCreated, gate: identity.PermissionBindingsCreate, definition: definition})
}

// Update appends a checked version while preserving the subject and omitted condition.
func (s *BindingService) Update(ctx context.Context, organization identity.OrganizationID, actor identity.UserID, binding identity.BindingID, expectedVersion int64, update identity.BindingUpdate) (BindingMutation, error) {
	if update.Scope != nil {
		scope := *update.Scope
		update.Scope = &scope
	}
	return s.mutateBinding(ctx, organization, actor, bindingCommand{action: audit.ActionBindingUpdated, gate: identity.PermissionBindingsUpdate, bindingID: binding, expectedVersion: expectedVersion, update: update})
}

// Revoke appends a terminal version and retains the previous scope and condition evidence.
func (s *BindingService) Revoke(ctx context.Context, organization identity.OrganizationID, actor identity.UserID, binding identity.BindingID, expectedVersion int64) (BindingMutation, error) {
	return s.mutateBinding(ctx, organization, actor, bindingCommand{action: audit.ActionBindingRevoked, gate: identity.PermissionBindingsDelete, bindingID: binding, expectedVersion: expectedVersion})
}

// bindingCommand keeps caller data separate from the server-selected management gate.
type bindingCommand struct {
	action          audit.Action
	gate            identity.Permission
	definition      identity.BindingDefinition
	bindingID       identity.BindingID
	expectedVersion int64
	update          identity.BindingUpdate
}

// mutateBinding commits only after the locked policy, recovery and evidence checks succeed.
func (s *BindingService) mutateBinding(ctx context.Context, organization identity.OrganizationID, actor identity.UserID, command bindingCommand) (BindingMutation, error) {
	bounded, cancel := context.WithTimeout(ctx, s.settings.Timeout)
	defer cancel()
	if err := bindingContextError(bounded); err != nil {
		return BindingMutation{}, err
	}
	if organization == "" || actor == "" {
		return BindingMutation{}, identity.ErrActorNotAuthorized
	}
	var mutation BindingMutation
	err := s.repository.WithBindingTransaction(bounded, organization, func(transaction BindingTransaction) error {
		var changeErr error
		mutation, changeErr = s.applyBindingCommand(bounded, transaction, organization, actor, command)
		return changeErr
	})
	if err != nil {
		err = safeBindingManagementError(bounded, err)
		target := administrationTarget{action: command.action, targetType: audit.TargetTypeBinding, targetID: string(command.bindingID)}
		return BindingMutation{}, recordPrivilegeRefusal(bounded, s.auditor, slog.Default(), actor, organization, target, err)
	}
	return mutation, nil
}

// applyBindingCommand checks authoritative policy data and appends one immutable grant version.
func (s *BindingService) applyBindingCommand(ctx context.Context, transaction BindingTransaction, organization identity.OrganizationID, actor identity.UserID, command bindingCommand) (BindingMutation, error) {
	catalog, err := transaction.LoadPermissionCatalog(ctx)
	if err != nil {
		return BindingMutation{}, err
	}
	if identity.ValidatePermissionCatalog(catalog) != nil {
		return BindingMutation{}, ErrBindingManagementInactive
	}
	for _, permission := range identity.BindingManagementPermissions() {
		if !slices.Contains(catalog, permission) {
			return BindingMutation{}, ErrBindingManagementInactive
		}
	}
	status, err := transaction.LockMemberStatus(ctx, actor)
	if errors.Is(err, identity.ErrUserNotFound) {
		return BindingMutation{}, identity.ErrActorNotAuthorized
	}
	if err != nil {
		return BindingMutation{}, err
	}
	held, err := transaction.LoadUnconditionalPermissions(ctx, actor)
	if err != nil {
		return BindingMutation{}, err
	}
	if err := (identity.Delegation{Actor: actor, Gate: command.gate}).Authorize(status == identity.StatusActive, held); err != nil {
		return BindingMutation{}, err
	}
	previous, definition, err := loadBindingChange(ctx, transaction, organization, command)
	if err != nil {
		return BindingMutation{}, err
	}
	if err := identity.ValidateNotSelf(actor, definition.UserID); err != nil {
		return BindingMutation{}, err
	}
	if _, err := transaction.LockMemberStatus(ctx, definition.UserID); err != nil {
		return BindingMutation{}, err
	}
	role, err := transaction.LoadRole(ctx, definition.RoleID)
	if err != nil {
		return BindingMutation{}, err
	}
	if err := validateBindingRole(role, definition.RoleID, catalog); err != nil {
		return BindingMutation{}, err
	}
	var previousRole identity.Role
	if previous.ID != "" {
		previousRole, err = transaction.LoadRole(ctx, previous.RoleID)
		if err != nil {
			return BindingMutation{}, err
		}
		if err := validateBindingRole(previousRole, previous.RoleID, catalog); err != nil {
			return BindingMutation{}, err
		}
	}
	if err := identity.ValidateDelegation(held, previousRole.Permissions, role.Permissions); err != nil {
		return BindingMutation{}, err
	}
	if err := transaction.LockBindingScope(ctx, definition.Scope); err != nil {
		return BindingMutation{}, err
	}
	if command.action != audit.ActionBindingRevoked && definition.HasCondition {
		if _, err := s.conditions.Prepare(ctx, definition.Condition); err != nil {
			if failure := bindingContextError(ctx); failure != nil {
				return BindingMutation{}, failure
			}
			return BindingMutation{}, identity.ErrInvalidCondition
		}
	}
	at, err := transaction.ObserveBindingMutationTime(ctx, definition.UserID)
	if err != nil {
		return BindingMutation{}, err
	}
	if at.IsZero() {
		return BindingMutation{}, ErrBindingManagementUnavailable
	}
	var next identity.RoleBinding
	if previous.ID == "" {
		next, err = transaction.InsertBinding(ctx, definition)
		if err != nil {
			return BindingMutation{}, err
		}
		if next.Validate() != nil || next.OrganizationID != organization || next.Definition() != definition || next.Version != 1 || next.Revoked {
			return BindingMutation{}, ErrBindingManagementUnavailable
		}
	} else {
		next = previous
		next.Version++
		next.RoleID = definition.RoleID
		next.Scope = definition.Scope
		next.Condition = definition.Condition
		next.HasCondition = definition.HasCondition
		next.Revoked = command.action == audit.ActionBindingRevoked
		if err := transaction.AppendBindingVersion(ctx, next, command.expectedVersion); err != nil {
			return BindingMutation{}, err
		}
	}
	recovery, err := transaction.HasRecoveryAdministrator(ctx, identity.SelectRecoveryPermissions(catalog))
	if err != nil {
		return BindingMutation{}, err
	}
	if !recovery {
		return BindingMutation{}, identity.ErrLastAdministrator
	}
	event := newSucceededEvent(ctx, actor, organization, administrationTarget{action: command.action, targetType: audit.TargetTypeBinding, targetID: string(next.ID)})
	event.OccurredAt = at
	event.Metadata = map[string]any{"after": bindingAuditState(next, role.Version)}
	if previous.ID != "" {
		event.Metadata["before"] = bindingAuditState(previous, previousRole.Version)
	}
	revision, err := transaction.FinalizeBindingChange(ctx, next.UserID, event)
	if err != nil {
		return BindingMutation{}, err
	}
	if revision <= 0 {
		return BindingMutation{}, ErrBindingManagementUnavailable
	}
	if err := bindingContextError(ctx); err != nil {
		return BindingMutation{}, err
	}
	return BindingMutation{Binding: next, Revision: revision, OccurredAt: at}, nil
}

// loadBindingChange enforces expected versions and explicit condition edits before any write.
func loadBindingChange(ctx context.Context, transaction BindingTransaction, organization identity.OrganizationID, command bindingCommand) (identity.RoleBinding, identity.BindingDefinition, error) {
	if command.action == audit.ActionBindingCreated {
		return identity.RoleBinding{}, command.definition, command.definition.Validate()
	}
	previous, err := transaction.LockBindingVersion(ctx, command.bindingID)
	if err != nil {
		return identity.RoleBinding{}, identity.BindingDefinition{}, err
	}
	if previous.OrganizationID != organization || previous.ID != command.bindingID {
		return identity.RoleBinding{}, identity.BindingDefinition{}, identity.ErrBindingNotFound
	}
	if command.expectedVersion <= 0 || previous.Version != command.expectedVersion || previous.Version == math.MaxInt64 {
		return identity.RoleBinding{}, identity.BindingDefinition{}, identity.ErrBindingConflict
	}
	if previous.Revoked {
		return identity.RoleBinding{}, identity.BindingDefinition{}, identity.ErrBindingRevoked
	}
	if err := previous.Validate(); err != nil {
		return identity.RoleBinding{}, identity.BindingDefinition{}, err
	}
	definition := previous.Definition()
	if command.action == audit.ActionBindingUpdated {
		definition, err = command.update.Apply(previous)
	}
	return previous, definition, err
}

// validateBindingRole refuses missing versions and permissions outside the transaction catalog.
func validateBindingRole(role identity.Role, expected identity.RoleID, catalog []identity.Permission) error {
	if role.ID != expected || role.Version <= 0 {
		return identity.ErrInvalidRoleBinding
	}
	for _, permission := range role.Permissions {
		if !slices.Contains(catalog, permission) {
			return identity.ErrPermissionNotInCatalog
		}
	}
	return nil
}

// bindingAuditState retains before/after grant identities without copying raw condition source.
func bindingAuditState(binding identity.RoleBinding, roleVersion int64) map[string]any {
	return map[string]any{
		"binding_id": string(binding.ID), "binding_version": binding.Version, "subject_user_id": string(binding.UserID), "role_id": string(binding.RoleID), "role_version": roleVersion, "scope_type": string(binding.Scope.Type), "connection_id": binding.Scope.ConnectionID, "has_condition": binding.HasCondition, "condition_language_version": binding.Condition.LanguageVersion, "condition_environment_version": binding.Condition.EnvironmentVersion, "revoked": binding.Revoked,
	}
}

// bindingContextError checks logical deadlines even while cancellation delivery is delayed.
func bindingContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, exists := ctx.Deadline(); exists && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

// safeBindingManagementError strips driver details while preserving semantic refusals.
func safeBindingManagementError(ctx context.Context, err error) error {
	if failure := bindingContextError(ctx); failure != nil {
		return failure
	}
	for _, safe := range []error{ErrBindingManagementInactive, ErrBindingManagementUnavailable, identity.ErrActorNotAuthorized, identity.ErrPrivilegeEscalation, identity.ErrSelfAdministration, identity.ErrLastAdministrator, identity.ErrBindingNotFound, identity.ErrBindingConflict, identity.ErrBindingRevoked, identity.ErrUserNotFound, identity.ErrRoleNotFound, identity.ErrInvalidRoleBinding, identity.ErrInvalidResourceScope, identity.ErrInvalidCondition, identity.ErrPermissionNotInCatalog} {
		if errors.Is(err, safe) {
			return safe
		}
	}
	return ErrBindingManagementUnavailable
}
