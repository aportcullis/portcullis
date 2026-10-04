package administration

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// delegationGuard applies the escalation and self-administration rules and records privilege refusals for both services.
type delegationGuard struct {
	permissions organizationPermissions
	auditor     AuditRecorder
	logger      *slog.Logger
}

// authorizeDelegation reads the actor's current permissions and refuses, with an audited refusal, any affected set it does not fully hold.
func (g *delegationGuard) authorizeDelegation(ctx context.Context, actor identity.UserID, org identity.OrganizationID, target administrationTarget, affected ...[]identity.Permission) error {
	held, err := g.permissions.PermissionsForUser(ctx, org, actor)
	if err != nil {
		return fmt.Errorf("resolve actor permissions: %w", err)
	}
	if err := identity.ValidateDelegation(held, affected...); err != nil {
		return g.recordRefusal(ctx, actor, org, target, err)
	}
	return nil
}

// authorizeOtherAccount refuses, with an audited refusal, an actor administering its own account.
func (g *delegationGuard) authorizeOtherAccount(ctx context.Context, actor, user identity.UserID, org identity.OrganizationID, target administrationTarget) error {
	if err := identity.ValidateNotSelf(actor, user); err != nil {
		return g.recordRefusal(ctx, actor, org, target, err)
	}
	return nil
}

// recordRefusal records err when it is a privilege refusal and returns it unchanged.
func (g *delegationGuard) recordRefusal(ctx context.Context, actor identity.UserID, org identity.OrganizationID, target administrationTarget, err error) error {
	return recordPrivilegeRefusal(ctx, g.auditor, g.logger, actor, org, target, err)
}
