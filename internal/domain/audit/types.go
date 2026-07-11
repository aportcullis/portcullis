// Package audit is the audit-trail bounded context: structured, append-only
// records of security-relevant actions (authentication, requests, approvals,
// execution, administration). It holds the event value object and its vocabulary;
// persistence and emission live in the infra and application layers.
package audit

import (
	"time"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// ActorType is who caused an event (audit_events.actor_type: user|system|service).
type ActorType string

// Action is the audited operation (audit_events.action).
type Action string

// Outcome is the terminal result of an action (audit_events.outcome).
type Outcome string

// Event is one structured audit record. A nil ActorUserID means the actor was
// not (or could not be) identified — e.g. a failed login for an unknown email.
type Event struct {
	// ID and OccurredAt are assigned by the store and populated only when an event
	// is read back (List). On insert they are ignored — the database assigns the id
	// (gen_random_uuid) and occurred_at (now() default).
	ID             string
	OccurredAt     time.Time
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

// ListParams is the store-level query for one page of audit events: return Limit
// rows, skipping Offset, ordered by occurred_at in the requested direction with an
// id tie-breaker (OFFSET pagination, PRD §7.1). The application service produces
// these from a validated page request, so Limit is already capped and Offset is
// non-negative. Limit/Offset are int64 to match the SQL bigint bounds, so a large
// page offset can never wrap negative (a 32-bit cast could — see review).
type ListParams struct {
	Limit          int64
	Offset         int64
	SortDescending bool
}

// EventPage is a page of audit events plus the totals needed for explicit page
// controls ("1–20 of 1,340", page jump — PRD §7.1). Page and PageSize echo the
// effective values the application service applied.
type EventPage struct {
	Events     []Event
	Page       int
	PageSize   int
	TotalCount int64
	TotalPages int
}
