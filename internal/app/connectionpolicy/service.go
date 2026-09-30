package connectionpolicy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aportcullis/portcullis/internal/app/auditevent"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// New builds the service on its repository port.
func New(repo Repository) (*Service, error) {
	if repo == nil {
		return nil, errors.New("connectionpolicy: repository required")
	}
	return &Service{repo: repo, now: time.Now}, nil
}

// WithClock overrides the clock for deterministic tests.
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

// Get returns the connection's current policy. Archived connections keep answering — the policy is part of the historical snapshot (ADR-0015).
func (s *Service) Get(ctx context.Context, id connection.ConnectionID) (connection.Policy, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return connection.Policy{}, fmt.Errorf("resolve organization: %w", err)
	}
	return s.repo.GetCurrent(ctx, org, id)
}

// Update replaces the connection's policy with a new immutable version: validate → diff against the version the caller read → append version expected+1 with CONNECTION_POLICY_UPDATED (and one CONNECTION_POLICY_CLASS_ENABLED per newly enabled write/ddl class — §4.3's admin-audit requirement) committing in the same transaction (ADR-0009).
func (s *Service) Update(ctx context.Context, actor identity.UserID, id connection.ConnectionID, p UpdateParams) (connection.Policy, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return connection.Policy{}, fmt.Errorf("resolve organization: %w", err)
	}
	next, err := connection.NewPolicy(
		id, org, p.ExpectedVersion+1,
		connection.ClassRule(p.Read), connection.ClassRule(p.Write), connection.ClassRule(p.DDL),
		connection.Limits{
			QueryTimeoutSeconds: p.QueryTimeoutSeconds,
			MaxRows:             p.MaxRows,
			MaxResultBytes:      p.MaxResultBytes,
		},
		actor, s.now(),
	)
	if err != nil {
		return connection.Policy{}, err
	}

	// The diff base is the CURRENT version. A racing update between this read and the pointer bump is caught by the bump's optimistic predicate, so the diff can never be recorded against a version the update didn't replace.
	current, err := s.repo.GetCurrent(ctx, org, id)
	if err != nil {
		return connection.Policy{}, err
	}
	if current.Version != p.ExpectedVersion {
		return connection.Policy{}, connection.ErrPolicyConflict
	}

	events := []audit.Event{s.updatedEvent(ctx, actor, org, current, next)}
	for _, class := range []connection.StatementClass{connection.ClassWrite, connection.ClassDDL} {
		if !current.Rule(class).Allowed && next.Rule(class).Allowed {
			events = append(events, s.classEnabledEvent(ctx, actor, org, next, class))
		}
	}
	return s.repo.UpdatePolicy(ctx, next, p.ExpectedVersion, events...)
}

// updatedEvent is the always-present CONNECTION_POLICY_UPDATED: the full new snapshot plus diffs against the replaced version, so the trail answers "who allowed what, when" without replaying history.
func (s *Service) updatedEvent(ctx context.Context, actor identity.UserID, org identity.OrganizationID, current, next connection.Policy) audit.Event {
	var enabled, disabled, autoApprove []string
	for _, class := range connection.Classes() {
		was, is := current.Rule(class), next.Rule(class)
		switch {
		case !was.Allowed && is.Allowed:
			enabled = append(enabled, string(class))
		case was.Allowed && !is.Allowed:
			disabled = append(disabled, string(class))
		}
		if is.Allowed && is.RequiredApprovals == 0 {
			autoApprove = append(autoApprove, string(class))
		}
	}

	evt := s.newEvent(ctx, actor, audit.ActionConnectionPolicyUpdated)
	evt.OrganizationID = org
	evt.TargetID = string(next.ConnectionID)
	evt.Metadata = map[string]any{
		"policy_version":        next.Version,
		"read":                  ruleMetadata(next.Read),
		"write":                 ruleMetadata(next.Write),
		"ddl":                   ruleMetadata(next.DDL),
		"query_timeout_seconds": next.Limits.QueryTimeoutSeconds,
		"max_rows":              next.Limits.MaxRows,
		"max_result_bytes":      next.Limits.MaxResultBytes,
		"enabled_classes":       enabled,
		"disabled_classes":      disabled,
		"changed_fields":        changedFields(current, next),
		"auto_approve_classes":  autoApprove,
	}
	return evt
}

func (s *Service) classEnabledEvent(ctx context.Context, actor identity.UserID, org identity.OrganizationID, next connection.Policy, class connection.StatementClass) audit.Event {
	evt := s.newEvent(ctx, actor, audit.ActionConnectionPolicyClassEnabled)
	evt.OrganizationID = org
	evt.TargetID = string(next.ConnectionID)
	evt.Metadata = map[string]any{
		"class":              string(class),
		"required_approvals": next.Rule(class).RequiredApprovals,
		"policy_version":     next.Version,
	}
	return evt
}

func (s *Service) newEvent(ctx context.Context, actor identity.UserID, action audit.Action) audit.Event {
	return auditevent.NewUser(ctx, actor, action, audit.TargetTypeConnection, audit.OutcomeSucceeded)
}

func ruleMetadata(r connection.ClassRule) map[string]any {
	return map[string]any{"allowed": r.Allowed, "required_approvals": r.RequiredApprovals}
}

// changedFields records the policy fields that actually changed.
func changedFields(current, next connection.Policy) []string {
	var fields []string
	for _, class := range connection.Classes() {
		was, is := current.Rule(class), next.Rule(class)
		if was.Allowed != is.Allowed {
			fields = append(fields, string(class)+".allowed")
		}
		if was.RequiredApprovals != is.RequiredApprovals {
			fields = append(fields, string(class)+".required_approvals")
		}
	}
	if current.Limits.QueryTimeoutSeconds != next.Limits.QueryTimeoutSeconds {
		fields = append(fields, "query_timeout_seconds")
	}
	if current.Limits.MaxRows != next.Limits.MaxRows {
		fields = append(fields, "max_rows")
	}
	if current.Limits.MaxResultBytes != next.Limits.MaxResultBytes {
		fields = append(fields, "max_result_bytes")
	}
	return fields
}
