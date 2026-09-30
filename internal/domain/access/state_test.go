package access_test

import (
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/access"
)

func TestTransitionMatrix(t *testing.T) {
	t.Parallel()
	allowed := map[access.State][]access.State{
		access.StateDraft:     {access.StatePending, access.StateApproved, access.StateCancelled},
		access.StatePending:   {access.StateApproved, access.StateRejected, access.StateCancelled, access.StateExpired},
		access.StateApproved:  {access.StateExecuting, access.StateCancelled, access.StateExpired},
		access.StateExecuting: {access.StateSucceeded, access.StateFailed, access.StateCancelled, access.StateOutcomeUnknown},

		access.StateRejected:       {},
		access.StateExpired:        {},
		access.StateCancelled:      {},
		access.StateSucceeded:      {},
		access.StateFailed:         {},
		access.StateOutcomeUnknown: {},
	}
	states := access.States()
	if len(states) != 10 {
		t.Fatalf("States() = %d entries, want 10", len(states))
	}
	if len(allowed) != len(states) {
		t.Fatalf("matrix covers %d states, want %d", len(allowed), len(states))
	}
	for _, from := range states {
		targets, ok := allowed[from]
		if !ok {
			t.Fatalf("state %q missing from the test matrix", from)
		}
		for _, to := range states {
			want := false
			for _, a := range targets {
				if a == to {
					want = true
				}
			}
			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("CanTransitionTo(%q → %q) = %t, want %t", from, to, got, want)
			}
		}
	}
}

func TestTerminal(t *testing.T) {
	t.Parallel()
	terminal := map[access.State]bool{
		access.StateDraft:          false,
		access.StatePending:        false,
		access.StateApproved:       false,
		access.StateExecuting:      false,
		access.StateRejected:       true,
		access.StateExpired:        true,
		access.StateCancelled:      true,
		access.StateSucceeded:      true,
		access.StateFailed:         true,
		access.StateOutcomeUnknown: true,
	}
	for s, want := range terminal {
		if got := s.Terminal(); got != want {
			t.Errorf("Terminal(%q) = %t, want %t", s, got, want)
		}
	}
}

func TestReasonValidity(t *testing.T) {
	t.Parallel()
	for _, r := range []access.Reason{
		access.ReasonPolicyChanged, access.ReasonConnectionArchived,
		access.ReasonApprovalInvalidated, access.ReasonTTLExpired,
	} {
		if !r.Valid() {
			t.Errorf("Reason %q must be valid", r)
		}
	}
	if access.Reason("").Valid() || access.Reason("whimsy").Valid() {
		t.Error("empty/unknown reasons must be invalid")
	}
}
