package administration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// NewRoleService builds the role administration service over the permission catalog loaded at startup.
func NewRoleService(repo RoleRepository, auditor AuditRecorder, catalog []identity.Permission) (*RoleService, error) {
	if repo == nil || auditor == nil || len(catalog) == 0 {
		return nil, errors.New("administration: role repository, auditor and permission catalog are required")
	}
	sorted := slices.Clone(catalog)
	slices.Sort(sorted)
	return &RoleService{
		repo:    repo,
		guard:   &delegationGuard{permissions: repo, auditor: auditor, logger: slog.Default()},
		catalog: slices.Compact(sorted),
	}, nil
}

// WithLogger overrides the logger that records dropped refusal events.
func (s *RoleService) WithLogger(logger *slog.Logger) *RoleService {
	if logger != nil {
		s.guard.logger = logger
	}
	return s
}

// ListPermissions returns the sorted permission catalog a custom role may draw from.
func (s *RoleService) ListPermissions() []identity.Permission {
	return slices.Clone(s.catalog)
}

// List returns the organization's live roles.
func (s *RoleService) List(ctx context.Context) ([]identity.Role, error) {
	org, err := s.resolveOrganization(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.ListRoles(ctx, org)
}

// Get returns one live role of the organization.
func (s *RoleService) Get(ctx context.Context, role identity.RoleID) (identity.Role, error) {
	org, err := s.resolveOrganization(ctx)
	if err != nil {
		return identity.Role{}, err
	}
	return s.repo.GetRole(ctx, org, role)
}

// Create adds a custom role whose permissions the actor fully holds.
func (s *RoleService) Create(ctx context.Context, actor identity.UserID, params RoleParams) (identity.Role, error) {
	definition, err := identity.NewRoleDefinition(params.Name, params.Permissions, s.catalog)
	if err != nil {
		return identity.Role{}, err
	}
	org, err := s.resolveOrganization(ctx)
	if err != nil {
		return identity.Role{}, err
	}
	target := administrationTarget{action: audit.ActionRoleCreated, targetType: audit.TargetTypeRole}
	if err := s.guard.authorizeDelegation(ctx, actor, org, target, definition.Permissions); err != nil {
		return identity.Role{}, err
	}
	event := newSucceededEvent(ctx, actor, org, target)
	event.Metadata = map[string]any{"permissions": permissionKeys(definition.Permissions)}
	return s.repo.CreateRole(ctx, org, definition, event)
}

// Update replaces a custom role's name and permissions when the actor holds both the old and the new set.
func (s *RoleService) Update(ctx context.Context, actor identity.UserID, role identity.RoleID, expectedVersion int64, params RoleParams) (identity.Role, error) {
	definition, err := identity.NewRoleDefinition(params.Name, params.Permissions, s.catalog)
	if err != nil {
		return identity.Role{}, err
	}
	org, err := s.resolveOrganization(ctx)
	if err != nil {
		return identity.Role{}, err
	}
	current, err := s.loadCustomRole(ctx, org, role, expectedVersion)
	if err != nil {
		return identity.Role{}, err
	}
	target := administrationTarget{action: audit.ActionRoleUpdated, targetType: audit.TargetTypeRole, targetID: string(role)}
	if err := s.guard.authorizeDelegation(ctx, actor, org, target, current.Permissions, definition.Permissions); err != nil {
		return identity.Role{}, err
	}
	added, removed := identity.DiffPermissions(current.Permissions, definition.Permissions)
	event := newSucceededEvent(ctx, actor, org, target)
	event.Metadata = map[string]any{
		"role_version":        expectedVersion + 1,
		"renamed":             current.Name != definition.Name,
		"added_permissions":   permissionKeys(added),
		"removed_permissions": permissionKeys(removed),
	}
	updated, err := s.repo.UpdateRole(ctx, org, role, expectedVersion, definition, event)
	if err != nil {
		return identity.Role{}, s.guard.recordRefusal(ctx, actor, org, target, err)
	}
	return updated, nil
}

// Delete soft-deletes an unassigned custom role.
func (s *RoleService) Delete(ctx context.Context, actor identity.UserID, role identity.RoleID, expectedVersion int64) error {
	org, err := s.resolveOrganization(ctx)
	if err != nil {
		return err
	}
	if _, err := s.loadCustomRole(ctx, org, role, expectedVersion); err != nil {
		return err
	}
	target := administrationTarget{action: audit.ActionRoleDeleted, targetType: audit.TargetTypeRole, targetID: string(role)}
	return s.repo.DeleteRole(ctx, org, role, expectedVersion, newSucceededEvent(ctx, actor, org, target))
}

// loadCustomRole returns a live custom role at the expected version; system roles and stale versions are refused before any mutation.
func (s *RoleService) loadCustomRole(ctx context.Context, org identity.OrganizationID, role identity.RoleID, expectedVersion int64) (identity.Role, error) {
	current, err := s.repo.GetRole(ctx, org, role)
	if err != nil {
		return identity.Role{}, err
	}
	if current.IsSystem {
		return identity.Role{}, identity.ErrSystemRoleImmutable
	}
	if current.Version != expectedVersion {
		return identity.Role{}, identity.ErrRoleConflict
	}
	return current, nil
}

// resolveOrganization returns the single self-hosted organization every administration query is scoped to.
func (s *RoleService) resolveOrganization(ctx context.Context) (identity.OrganizationID, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve organization: %w", err)
	}
	return org, nil
}
