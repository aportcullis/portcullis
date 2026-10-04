package administration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aportcullis/portcullis/internal/app/auditevent"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// refusalWriteTimeout bounds the detached best-effort write of a refusal event so a stuck store cannot leak goroutines.
const refusalWriteTimeout = 5 * time.Second

// administrationTarget names the audited action and entity of one administration attempt.
type administrationTarget struct {
	action     audit.Action
	targetType string
	targetID   string
}

// newSucceededEvent builds the event a mutation commits atomically with its change.
func newSucceededEvent(ctx context.Context, actor identity.UserID, org identity.OrganizationID, target administrationTarget) audit.Event {
	event := auditevent.NewUser(ctx, actor, target.action, target.targetType, audit.OutcomeSucceeded)
	event.OrganizationID = org
	event.TargetID = target.targetID
	return event
}

// refusalReason maps a privilege refusal to its audit reason; other errors are not privilege refusals.
func refusalReason(err error) (string, bool) {
	switch {
	case errors.Is(err, identity.ErrPrivilegeEscalation):
		return "privilege_escalation", true
	case errors.Is(err, identity.ErrSelfAdministration):
		return "self_administration", true
	case errors.Is(err, identity.ErrLastAdministrator):
		return "last_administrator", true
	case errors.Is(err, identity.ErrActorNotAuthorized):
		return "actor_not_authorized", true
	default:
		return "", false
	}
}

// recordPrivilegeRefusal appends a best-effort FAILED event for escalation, self-administration and last-administrator refusals, detached from request cancellation, and returns err unchanged.
func recordPrivilegeRefusal(ctx context.Context, auditor AuditRecorder, logger *slog.Logger, actor identity.UserID, org identity.OrganizationID, target administrationTarget, err error) error {
	reason, isRefusal := refusalReason(err)
	if !isRefusal {
		return err
	}
	event := auditevent.NewUser(ctx, actor, target.action, target.targetType, audit.OutcomeFailed)
	event.OrganizationID = org
	event.TargetID = target.targetID
	event.Metadata = map[string]any{"reason": reason}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refusalWriteTimeout)
	defer cancel()
	if recordErr := auditor.Record(writeCtx, event); recordErr != nil {
		logger.WarnContext(writeCtx, "audit event dropped", "action", target.action, "error_type", fmt.Sprintf("%T", recordErr))
	}
	return err
}

// permissionKeys converts permissions to plain strings for audit metadata.
func permissionKeys(permissions []identity.Permission) []string {
	if len(permissions) == 0 {
		return nil
	}
	keys := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		keys = append(keys, string(permission))
	}
	return keys
}
