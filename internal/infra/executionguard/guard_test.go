package executionguard_test

import (
	"errors"
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
		finish(false)
	}
	if _, err := guard.Allow("broken-target"); !errors.Is(err, access.ErrTargetUnavailable) {
		t.Fatalf("open circuit admitted: %v", err)
	}
	finish, err := guard.Allow("healthy-target")
	if err != nil {
		t.Fatal("one target poisoned another target")
	}
	finish(true)
}
