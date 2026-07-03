// Package audit is the audit-trail bounded context: structured, append-only
// records of security-relevant actions (authentication, requests, approvals,
// execution, administration). It holds the event value object and its vocabulary;
// persistence and emission live in the infra and application layers.
package audit

import "github.com/aportcullis/portcullis/internal/domain/identity"

// ActorType is who caused an event (audit_events.actor_type: user|system|service).
type ActorType string

// Action is the audited operation (audit_events.action).
type Action string

// Outcome is the terminal result of an action (audit_events.outcome).
type Outcome string

// Event is one structured audit record. occurred_at is assigned by the store
// (DB default), so it is not carried here. A nil ActorUserID means the actor was
// not (or could not be) identified — e.g. a failed login for an unknown email.
type Event struct {
	OrganizationID identity.OrganizationID
	ActorType      ActorType
	ActorUserID    *identity.UserID
	// ActorService names a non-human actor (system/service) with a stable id; empty
	// for a user actor.
	ActorService string
	Action       Action
	TargetType   string
	TargetID     string
	Outcome      Outcome
	// RequestID correlates the event with the request log; SourceIP is the resolved
	// client IP. Both may be empty when unavailable.
	RequestID string
	SourceIP  string
	// Metadata carries non-sensitive supplemental fields (never secrets, tokens, or
	// result rows) and is stored as JSONB.
	Metadata map[string]any
}
