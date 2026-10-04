package access_test

import (
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/access"
)

func TestExecutionLeaseTimingsKeepLiveOwnersAndBoundRecovery(t *testing.T) {
	t.Parallel()
	if access.ExecutionHeartbeatInterval >= access.ExecutionLeaseDuration {
		t.Fatalf("heartbeat %s must renew before the %s lease expires", access.ExecutionHeartbeatInterval, access.ExecutionLeaseDuration)
	}
	if access.ExecutionLeaseDuration < 4*access.ExecutionHeartbeatInterval {
		t.Fatalf("lease %s must tolerate four missed %s heartbeats (PRD §4.4)", access.ExecutionLeaseDuration, access.ExecutionHeartbeatInterval)
	}
	if access.ExecutionReconcileTimeout >= access.ExecutionReconcileInterval {
		t.Fatalf("reconcile run %s must end before the next %s tick", access.ExecutionReconcileTimeout, access.ExecutionReconcileInterval)
	}
	if access.ExecutionReconcileBatchSize < 1 {
		t.Fatalf("reconcile batch size %d must be positive", access.ExecutionReconcileBatchSize)
	}
}

func TestExecutionCompletionAcceptsOnlyKnownEvidence(t *testing.T) {
	t.Parallel()
	expiry := time.Now()
	for _, scenario := range []struct {
		name       string
		completion access.ExecutionCompletion
		valid      bool
	}{
		{"stored result", access.ExecutionCompletion{State: access.StateSucceeded, ResultID: "result", ResultExpiresAt: &expiry}, true},
		{"store full success", access.ExecutionCompletion{State: access.StateSucceeded, ResultUnavailableReason: access.ResultUnavailableStoreFull}, true},
		{"persistence failed success", access.ExecutionCompletion{State: access.StateSucceeded, ResultUnavailableReason: access.ResultUnavailablePersistenceFailed}, true},
		{"failed without a reason", access.ExecutionCompletion{State: access.StateFailed}, true},
		{"forged free-text reason", access.ExecutionCompletion{State: access.StateSucceeded, ResultUnavailableReason: "password=hunter2"}, false},
		{"reason beside a stored result", access.ExecutionCompletion{State: access.StateSucceeded, ResultID: "result", ResultExpiresAt: &expiry, ResultUnavailableReason: access.ResultUnavailableStoreFull}, false},
		{"reason on a failed execution", access.ExecutionCompletion{State: access.StateFailed, ResultUnavailableReason: access.ResultUnavailablePersistenceFailed}, false},
		{"reason on an unknown outcome", access.ExecutionCompletion{State: access.StateOutcomeUnknown, ResultUnavailableReason: access.ResultUnavailableStoreFull}, false},
		{"shutdown-cancelled execution", access.ExecutionCompletion{State: access.StateCancelled, InterruptionCause: access.InterruptedByShutdown}, true},
		{"owner-cancelled commit left unknown", access.ExecutionCompletion{State: access.StateOutcomeUnknown, InterruptionCause: access.InterruptedByOwner}, true},
		{"lease loss before commit", access.ExecutionCompletion{State: access.StateCancelled, InterruptionCause: access.InterruptedByLeaseLoss}, true},
		{"forged interruption cause", access.ExecutionCompletion{State: access.StateCancelled, InterruptionCause: "operator said so"}, false},
		{"interruption cause on a success", access.ExecutionCompletion{State: access.StateSucceeded, InterruptionCause: access.InterruptedByShutdown}, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			if got := scenario.completion.Valid(); got != scenario.valid {
				t.Fatalf("Valid() = %v, want %v", got, scenario.valid)
			}
		})
	}
}
