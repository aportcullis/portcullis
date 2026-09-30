// Package access defines request states, immutable approval payloads, and approval evidence (ADR-0018).
package access

// State identifies a request lifecycle state.
type State string

const (
	StateDraft          State = "draft"
	StatePending        State = "pending"
	StateApproved       State = "approved"
	StateRejected       State = "rejected"
	StateExpired        State = "expired"
	StateCancelled      State = "cancelled"
	StateExecuting      State = "executing"
	StateSucceeded      State = "succeeded"
	StateFailed         State = "failed"
	StateOutcomeUnknown State = "outcome_unknown"
)

// States returns the state vocabulary in its canonical order.
func States() []State {
	return []State{
		StateDraft, StatePending, StateApproved, StateRejected, StateExpired,
		StateCancelled, StateExecuting, StateSucceeded, StateFailed, StateOutcomeUnknown,
	}
}

// transitions defines allowed request-state changes.
var transitions = map[State][]State{
	StateDraft:     {StatePending, StateApproved, StateCancelled},
	StatePending:   {StateApproved, StateRejected, StateCancelled, StateExpired},
	StateApproved:  {StateExecuting, StateCancelled, StateExpired},
	StateExecuting: {StateSucceeded, StateFailed, StateCancelled, StateOutcomeUnknown},
}

// CanTransitionTo reports whether the edge s → t exists in the state machine.
func (s State) CanTransitionTo(t State) bool {
	for _, next := range transitions[s] {
		if next == t {
			return true
		}
	}
	return false
}

// Terminal reports whether the state has no outgoing transition.
func (s State) Terminal() bool {
	switch s {
	case StateRejected, StateExpired, StateCancelled, StateSucceeded, StateFailed, StateOutcomeUnknown:
		return true
	}
	return false
}

// Reason identifies a system-caused expiry or cancellation.
type Reason string

const (
	ReasonPolicyChanged Reason = "policy_changed"
	// ReasonConnectionChanged: the target's configuration was replaced. The id stayed, but host, port, database, TLS mode or credential did not — so what the approvers looked at is gone and the approval goes with it (ADR-0018).
	ReasonConnectionChanged   Reason = "connection_changed"
	ReasonConnectionArchived  Reason = "connection_archived"
	ReasonApprovalInvalidated Reason = "approval_invalidated"
	ReasonTTLExpired          Reason = "ttl_expired"
)

// Valid reports whether r is one of the system-caused reasons.
func (r Reason) Valid() bool {
	switch r {
	case ReasonPolicyChanged, ReasonConnectionChanged, ReasonConnectionArchived,
		ReasonApprovalInvalidated, ReasonTTLExpired:
		return true
	}
	return false
}
