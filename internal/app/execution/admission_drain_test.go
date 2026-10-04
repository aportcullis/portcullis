package execution_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/execution"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

// preflightGateFixture holds an admitted execution at lease acquisition so a test can change the server's drain state mid-preflight.
type preflightGateFixture struct {
	*executionFixture
	entered chan struct{}
	proceed chan struct{}
}

func (f *preflightGateFixture) AcquireExecution(ctx context.Context, org identity.OrganizationID, id access.RequestID, requester identity.UserID, owner, attempt string, digest []byte, event audit.Event) (access.ExecutionLease, error) {
	close(f.entered)
	<-f.proceed
	return f.executionFixture.AcquireExecution(ctx, org, id, requester, owner, attempt, digest, event)
}

// startExecutionHeldInPreflight admits one execution and returns once it waits at lease acquisition.
func startExecutionHeldInPreflight(t *testing.T) (*execution.Service, *preflightGateFixture, <-chan access.ExecutionCompletion) {
	t.Helper()
	gate := &preflightGateFixture{executionFixture: newInterruptionFixture(), entered: make(chan struct{}), proceed: make(chan struct{})}
	// The target honours an already-cancelled context, so an interruption registered during preflight is observable.
	gate.executeTarget = func(ctx context.Context, _ query.Execution) (query.ResultStream, error) {
		if err := ctx.Err(); err != nil {
			return &failingStream{err: fmt.Errorf("%w: %w", query.ErrInterruptedBeforeCommit, err)}, nil
		}
		return &resultStream{}, nil
	}
	service, err := execution.New(gate, gate, gate, gate, credentialCodec{}, gate, gate, "server", 1)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan access.ExecutionCompletion, 1)
	go func() {
		completion, err := service.Execute(context.Background(), "requester", "request")
		if err != nil {
			t.Errorf("Execute: %v", err)
		}
		finished <- completion
	}()
	select {
	case <-gate.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("execution never reached lease acquisition")
	}
	return service, gate, finished
}

func TestStoppingAdmissionLetsAdmittedExecutionsFinish(t *testing.T) {
	t.Run("an execution in preflight when admission stops completes normally", func(t *testing.T) {
		service, gate, finished := startExecutionHeldInPreflight(t)
		service.StopAdmission()
		close(gate.proceed)
		completion := awaitCompletion(t, finished)
		if completion.State != access.StateSucceeded || completion.InterruptionCause != "" {
			t.Fatalf("admitted execution after StopAdmission = %+v, want succeeded without an interruption cause", completion)
		}
	})
	t.Run("an execution running when admission stops completes normally", func(t *testing.T) {
		fixture := newInterruptionFixture()
		service, finished, release := startBlockedExecution(t, fixture, nil)
		service.StopAdmission()
		close(release)
		if completion := awaitCompletion(t, finished); completion.State != access.StateSucceeded || completion.InterruptionCause != "" {
			t.Fatalf("running execution after StopAdmission = %+v, want succeeded", completion)
		}
	})
	t.Run("stopping admission twice still lets the admitted execution finish", func(t *testing.T) {
		service, gate, finished := startExecutionHeldInPreflight(t)
		service.StopAdmission()
		service.StopAdmission()
		close(gate.proceed)
		if completion := awaitCompletion(t, finished); completion.State != access.StateSucceeded {
			t.Fatalf("admitted execution = %+v, want succeeded", completion)
		}
	})
	t.Run("a new execution after admission stops is refused without reaching the target", func(t *testing.T) {
		fixture := newInterruptionFixture()
		service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
		if err != nil {
			t.Fatal(err)
		}
		service.StopAdmission()
		if _, err := service.Execute(context.Background(), "requester", "request"); !errors.Is(err, query.ErrResultBusy) || fixture.executions != 0 {
			t.Fatalf("execution after StopAdmission = %v with %d target calls", err, fixture.executions)
		}
	})
	t.Run("interrupting after admission stopped cancels an execution that registers later", func(t *testing.T) {
		service, gate, finished := startExecutionHeldInPreflight(t)
		service.StopAdmission()
		interrupted := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			interrupted <- service.InterruptActive(ctx)
		}()
		// InterruptActive holds the lock while marking interruption; give it the chance to run before the preflight resumes.
		waitUntilInterrupting(t, service)
		close(gate.proceed)
		completion := awaitCompletion(t, finished)
		if completion.State != access.StateCancelled || completion.InterruptionCause != access.InterruptedByShutdown {
			t.Fatalf("execution registering after InterruptActive = %+v, want cancelled by server_shutdown", completion)
		}
		if err := <-interrupted; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("interrupting refuses new executions as well", func(t *testing.T) {
		fixture := newInterruptionFixture()
		service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.InterruptActive(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Execute(context.Background(), "requester", "request"); !errors.Is(err, query.ErrResultBusy) || fixture.executions != 0 {
			t.Fatalf("execution after InterruptActive = %v with %d target calls", err, fixture.executions)
		}
	})
}

// waitUntilInterrupting polls the exported drain state until InterruptActive has marked the service.
func waitUntilInterrupting(t *testing.T, service *execution.Service) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !service.Interrupting() {
		if time.Now().After(deadline) {
			t.Fatal("InterruptActive never marked the service")
		}
		time.Sleep(time.Millisecond)
	}
}
