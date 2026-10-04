// Package administration implements user and custom-role administration: escalation, self-administration and last-administrator guards, one-time password setup links and audited mutations (ADR-0053).
package administration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// NewUserService builds the user administration service.
func NewUserService(repo UserRepository, auditor AuditRecorder) (*UserService, error) {
	if repo == nil || auditor == nil {
		return nil, errors.New("administration: user repository and auditor are required")
	}
	return &UserService{repo: repo, guard: &delegationGuard{permissions: repo, auditor: auditor, logger: slog.Default()}}, nil
}

// WithLogger overrides the logger that records dropped refusal events.
func (s *UserService) WithLogger(logger *slog.Logger) *UserService {
	if logger != nil {
		s.guard.logger = logger
	}
	return s
}

// List returns the organization's members.
func (s *UserService) List(ctx context.Context) ([]identity.Member, error) {
	org, err := s.resolveOrganization(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.ListMembers(ctx, org)
}

// Get returns one member of the organization.
func (s *UserService) Get(ctx context.Context, user identity.UserID) (identity.Member, error) {
	org, err := s.resolveOrganization(ctx)
	if err != nil {
		return identity.Member{}, err
	}
	return s.repo.GetMember(ctx, org, user)
}

// Create adds an account with an initial role the actor fully holds and returns its one-time setup link.
func (s *UserService) Create(ctx context.Context, actor identity.UserID, params CreateUserParams) (CreatedUser, error) {
	email := identity.NormalizeEmail(params.Email)
	if err := identity.ValidateEmail(email); err != nil {
		return CreatedUser{}, err
	}
	if err := identity.ValidateDisplayName(params.DisplayName); err != nil {
		return CreatedUser{}, err
	}
	org, err := s.resolveOrganization(ctx)
	if err != nil {
		return CreatedUser{}, err
	}
	role, err := s.repo.GetRole(ctx, org, params.RoleID)
	if err != nil {
		return CreatedUser{}, err
	}
	target := administrationTarget{action: audit.ActionUserCreated, targetType: audit.TargetTypeUser}
	if err := s.guard.authorizeDelegation(ctx, actor, org, target, role.Permissions); err != nil {
		return CreatedUser{}, err
	}
	token, issue, err := newPasswordSetupIssue()
	if err != nil {
		return CreatedUser{}, err
	}
	event := newSucceededEvent(ctx, actor, org, target)
	event.Metadata = map[string]any{"role_id": string(role.ID)}
	member, setup, err := s.repo.CreateMember(ctx, org, identity.NewMember{Email: email, DisplayName: params.DisplayName, RoleID: role.ID}, issue, event)
	if err != nil {
		return CreatedUser{}, err
	}
	return CreatedUser{Member: member, SetupLink: SetupLink{Token: token, ExpiresAt: setup.ExpiresAt}}, nil
}

// IssueSetupLink replaces the open setup link of an active user without a password.
func (s *UserService) IssueSetupLink(ctx context.Context, actor, user identity.UserID) (SetupLink, error) {
	target := administrationTarget{action: audit.ActionUserSetupLinkIssued, targetType: audit.TargetTypeUser, targetID: string(user)}
	org, err := s.resolveOrganization(ctx)
	if err != nil {
		return SetupLink{}, err
	}
	if _, err := s.loadDominatedMember(ctx, actor, org, user, target); err != nil {
		return SetupLink{}, err
	}
	token, issue, err := newPasswordSetupIssue()
	if err != nil {
		return SetupLink{}, err
	}
	setup, err := s.repo.IssuePasswordSetup(ctx, org, user, issue, newSucceededEvent(ctx, actor, org, target))
	if err != nil {
		return SetupLink{}, err
	}
	return SetupLink{Token: token, ExpiresAt: setup.ExpiresAt}, nil
}

// Disable blocks another member whose role the actor fully holds and revokes its sessions and setup link.
func (s *UserService) Disable(ctx context.Context, actor, user identity.UserID) (identity.Member, error) {
	return s.changeStatus(ctx, actor, user, identity.StatusDisabled, audit.ActionUserDisabled)
}

// Enable restores another disabled member whose role the actor fully holds.
func (s *UserService) Enable(ctx context.Context, actor, user identity.UserID) (identity.Member, error) {
	return s.changeStatus(ctx, actor, user, identity.StatusActive, audit.ActionUserEnabled)
}

// AssignRole replaces another member's role when the actor fully holds both the current and the new role.
func (s *UserService) AssignRole(ctx context.Context, actor, user identity.UserID, role identity.RoleID) (identity.Member, error) {
	target := administrationTarget{action: audit.ActionUserRoleAssigned, targetType: audit.TargetTypeUser, targetID: string(user)}
	org, err := s.resolveOrganization(ctx)
	if err != nil {
		return identity.Member{}, err
	}
	if err := s.guard.authorizeOtherAccount(ctx, actor, user, org, target); err != nil {
		return identity.Member{}, err
	}
	next, err := s.repo.GetRole(ctx, org, role)
	if err != nil {
		return identity.Member{}, err
	}
	member, err := s.loadDominatedMember(ctx, actor, org, user, target, next.Permissions)
	if err != nil {
		return identity.Member{}, err
	}
	if member.RoleID == next.ID {
		return member, nil
	}
	event := newSucceededEvent(ctx, actor, org, target)
	event.Metadata = map[string]any{"previous_role_id": string(member.RoleID), "role_id": string(next.ID)}
	updated, err := s.repo.AssignMemberRole(ctx, org, user, next.ID, event)
	if err != nil {
		return identity.Member{}, s.guard.recordRefusal(ctx, actor, org, target, err)
	}
	return updated, nil
}

// changeStatus applies a disable or enable to another member the actor dominates; an unchanged status commits nothing.
func (s *UserService) changeStatus(ctx context.Context, actor, user identity.UserID, status identity.UserStatus, action audit.Action) (identity.Member, error) {
	target := administrationTarget{action: action, targetType: audit.TargetTypeUser, targetID: string(user)}
	org, err := s.resolveOrganization(ctx)
	if err != nil {
		return identity.Member{}, err
	}
	if err := s.guard.authorizeOtherAccount(ctx, actor, user, org, target); err != nil {
		return identity.Member{}, err
	}
	member, err := s.loadDominatedMember(ctx, actor, org, user, target)
	if err != nil {
		return identity.Member{}, err
	}
	if member.User.Status == status {
		return member, nil
	}
	updated, err := s.repo.SetMemberStatus(ctx, org, user, status, newSucceededEvent(ctx, actor, org, target))
	if err != nil {
		return identity.Member{}, s.guard.recordRefusal(ctx, actor, org, target, err)
	}
	return updated, nil
}

// loadDominatedMember returns the member after proving the actor holds every permission of the member's current role and of any additional affected sets.
func (s *UserService) loadDominatedMember(ctx context.Context, actor identity.UserID, org identity.OrganizationID, user identity.UserID, target administrationTarget, additional ...[]identity.Permission) (identity.Member, error) {
	member, err := s.repo.GetMember(ctx, org, user)
	if err != nil {
		return identity.Member{}, err
	}
	current, err := s.repo.GetRole(ctx, org, member.RoleID)
	if err != nil {
		return identity.Member{}, err
	}
	if err := s.guard.authorizeDelegation(ctx, actor, org, target, append([][]identity.Permission{current.Permissions}, additional...)...); err != nil {
		return identity.Member{}, err
	}
	return member, nil
}

// resolveOrganization returns the single self-hosted organization every administration query is scoped to.
func (s *UserService) resolveOrganization(ctx context.Context) (identity.OrganizationID, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve organization: %w", err)
	}
	return org, nil
}
