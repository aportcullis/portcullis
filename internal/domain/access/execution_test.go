package access_test

import (
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/access"
)

func TestExecutionCompletionAcceptsOnlyKnownResultUnavailableReasons(t *testing.T) {
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
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			if got := scenario.completion.Valid(); got != scenario.valid {
				t.Fatalf("Valid() = %v, want %v", got, scenario.valid)
			}
		})
	}
}
