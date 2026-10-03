// Package audit is the audit-trail bounded context: structured, append-only records of security-relevant actions (authentication, requests, approvals, execution, administration). It holds the event value object and its vocabulary; persistence and emission live in the infra and application layers.
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

// Event is one structured audit record. A nil ActorUserID means the actor was not (or could not be) identified — e.g. a failed login for an unknown email.
type Event struct {
	// ID and OccurredAt are assigned by the store and populated only when an event is read back (List). On insert they are ignored — the database assigns the id (gen_random_uuid) and occurred_at (now() default).
	ID             string
	OccurredAt     time.Time
	OrganizationID identity.OrganizationID
	ActorType      ActorType
	ActorUserID    *identity.UserID
	// ActorService names a non-human actor (system/service) with a stable id; empty for a user actor.
	ActorService string
	Action       Action
	TargetType   string
	TargetID     string
	Outcome      Outcome
	// RequestID correlates the event with the request log; SourceIP is the resolved client IP. Both may be empty when unavailable.
	RequestID string
	SourceIP  string
	// State transition snapshot (§6.1): the states an ACCESS_REQUEST_* (and, later, execution) event moved between. Empty for non-transition events.
	PreviousState string
	NextState     string
	// Execution-path snapshot columns (§6.1, ADR-0018), populated from ACCESS_REQUEST_SUBMITTED onward: the target connection, the statement class, and the payload digest with its HMAC key version (the pair is all-or-nothing — the audit_digest_pairing CHECK). Zero values map to NULL.
	ConnectionID            string
	QueryType               string
	PayloadDigest           []byte
	PayloadDigestKeyVersion uint32
	// RowsAffected and DurationMilliseconds retain optional execution metrics.
	RowsAffected         *int64
	DurationMilliseconds *int64
	// Metadata carries non-sensitive supplemental fields (never secrets, tokens, or result rows) and is stored as JSONB.
	Metadata map[string]any
}

// ListParams describes a page whose rows, count, and clamp must share one database snapshot. Use int64 bounds to avoid offset overflow.
type ListParams struct {
	Page           int
	PageSize       int
	SortDescending bool
}

// EventPage is a page of audit events plus the totals needed for explicit page controls ("1–20 of 1,340", page jump — PRD §7.1). Page and PageSize echo the effective values the application service applied.
type EventPage struct {
	Events     []Event
	Page       int
	PageSize   int
	TotalCount int64
	TotalPages int
}
