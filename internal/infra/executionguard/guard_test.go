package executionguard_test

import (
	"errors"
	"github.com/aportcullis/portcullis/internal/app/execution"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/infra/executionguard"
)

func TestCircuitAdmissionIsPerConnectionAndTripsAfterSixFailures(t *testing.T) {
	guard := executionguard.New()
	for range 6 {
		finish, err := guard.Allow("broken-target")
		if err != nil {
			t.Fatal(err)
		}
		finish(execution.TargetUnhealthy)
	}
	if _, err := guard.Allow("broken-target"); !errors.Is(err, access.ErrTargetUnavailable) {
		t.Fatalf("open circuit admitted: %v", err)
	}
	finish, err := guard.Allow("healthy-target")
	if err != nil {
		t.Fatal("one target poisoned another target")
	}
	finish(execution.TargetHealthy)
}

func TestUnattemptedLeaseDoesNotResetTargetFailures(t *testing.T) {
	guard := executionguard.New()
	for range 5 {
		done, err := guard.Allow("target")
		if err != nil {
			t.Fatal(err)
		}
		done(execution.TargetUnhealthy)
	}
	done, err := guard.Allow("target")
	if err != nil {
		t.Fatal(err)
	}
	done(execution.TargetNotAttempted)
	done, err = guard.Allow("target")
	if err != nil {
		t.Fatal(err)
	}
	done(execution.TargetUnhealthy)
	if _, err := guard.Allow("target"); !errors.Is(err, access.ErrTargetUnavailable) {
		t.Fatalf("abandoned lease reset the failure streak: %v", err)
	}
}
