package access

import (
	"github.com/aportcullis/portcullis/internal/domain/audit"
)

// System actor identifiers distinguish automatic request transitions.
const (
	ActorAutoApproval   = "system:auto-approval"
	ActorPolicyCascade  = "system:policy-change"
	ActorConfigCascade  = "system:connection-config-change"
	ActorArchiveCascade = "system:connection-archive"
	ActorTTLExpiry      = "system:ttl-expiry"
)

// ExpiredEvent builds a correlated system-expiry audit event.
func ExpiredEvent(r Request, from State, reason Reason, actorService string, correlate audit.Event) audit.Event {
	return cascadeEvent(audit.ActionAccessRequestExpired, r, from, StateExpired, reason, actorService, correlate)
}

// CancelledEvent builds an audit event for system cancellation.
func CancelledEvent(r Request, from State, reason Reason, actorService string, correlate audit.Event) audit.Event {
	return cascadeEvent(audit.ActionAccessRequestCancelled, r, from, StateCancelled, reason, actorService, correlate)
}

// cascadeEvent copies request evidence into a system transition event.
func cascadeEvent(action audit.Action, r Request, from, to State, reason Reason, actorService string, correlate audit.Event) audit.Event {
	meta := map[string]any{"reason": string(reason)}
	if r.RedactedSQL != "" {
		meta["redacted_sql"] = r.RedactedSQL
	}
	return audit.Event{
		OrganizationID:          r.OrganizationID,
		ActorType:               audit.ActorSystem,
		ActorService:            actorService,
		Action:                  action,
		TargetType:              audit.TargetTypeAccessRequest,
		TargetID:                string(r.ID),
		Outcome:                 audit.OutcomeSucceeded,
		PreviousState:           string(from),
		NextState:               string(to),
		RequestID:               correlate.RequestID,
		SourceIP:                correlate.SourceIP,
		ConnectionID:            string(r.ConnectionID),
		QueryType:               string(r.Class),
		PayloadDigest:           r.Digest,
		PayloadDigestKeyVersion: r.DigestKeyVersion,
		Metadata:                meta,
	}
}
