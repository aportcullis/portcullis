// Package auditevent attaches request correlation and client IP to application audit events.
package auditevent

import (
	"context"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/platform/logging"
	"github.com/aportcullis/portcullis/internal/platform/reqmeta"
)

// New assembles a user-typed event WITHOUT a resolved actor or target, carrying the ctx's request id and source IP (ADR-0009 §6.1) — the shape the auth service needs before a user is resolved (a failed login for an unknown email must not carry a spurious actor/target). Callers fill the rest.
func New(ctx context.Context, action audit.Action, outcome audit.Outcome) audit.Event {
	return audit.Event{
		ActorType: audit.ActorUser,
		Action:    action,
		Outcome:   outcome,
		RequestID: logging.RequestID(ctx),
		SourceIP:  reqmeta.ClientIP(ctx),
	}
}

// NewUser assembles an event attributed to a known actor acting on a known target type. Callers fill OrganizationID, TargetID, and Metadata for their entity.
func NewUser(ctx context.Context, actor identity.UserID, action audit.Action, targetType string, outcome audit.Outcome) audit.Event {
	e := New(ctx, action, outcome)
	e.ActorUserID = &actor
	e.TargetType = targetType
	return e
}
