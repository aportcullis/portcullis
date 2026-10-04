package execution_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/execution"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

func TestShutdownInterruptsActiveExecutionsWithAuditedCause(t *testing.T) {
	t.Run("running statement records server_shutdown before interruption returns", func(t *testing.T) {
		fixture := newInterruptionFixture()
		service, finished, _ := startBlockedExecution(t, fixture, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := service.InterruptActive(ctx); err != nil {
			t.Fatal(err)
		}
		if fixture.completion.State != access.StateCancelled || fixture.completion.InterruptionCause != access.InterruptedByShutdown {
			t.Fatalf("recorded before interruption returned = %+v, want cancelled by server_shutdown", fixture.completion)
		}
		if completion := awaitCompletion(t, finished); completion.InterruptionCause != access.InterruptedByShutdown {
			t.Fatalf("returned completion = %+v", completion)
		}
	})
	t.Run("owner cancellation records owner_cancel", func(t *testing.T) {
		fixture := newInterruptionFixture()
		service, finished, _ := startBlockedExecution(t, fixture, nil)
		if err := service.Cancel(context.Background(), "requester", "request"); err != nil {
			t.Fatal(err)
		}
		awaitCompletion(t, finished)
		if fixture.completion.State != access.StateCancelled || fixture.completion.InterruptionCause != access.InterruptedByOwner {
			t.Fatalf("owner cancellation recorded %+v", fixture.completion)
		}
	})
	t.Run("idle service interrupts immediately and refuses new executions", func(t *testing.T) {
		fixture := newInterruptionFixture()
		service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.InterruptActive(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Execute(context.Background(), "requester", "request"); !errors.Is(err, query.ErrResultBusy) || fixture.executions != 0 {
			t.Fatalf("execution after shutdown = %v with %d target calls", err, fixture.executions)
		}
	})
	t.Run("every active execution is interrupted", func(t *testing.T) {
		blocking := &blockingExecutionFixture{
			executionFixture: newInterruptionFixture(),
			leases:           make(map[access.RequestID]bool), started: make(chan struct{}, 2), release: make(chan struct{}),
		}
		service, err := execution.New(blocking, blocking, blocking, blocking, credentialCodec{}, blocking, blocking, "server", 2)
		if err != nil {
			t.Fatal(err)
		}
		results := make(chan access.ExecutionCompletion, 2)
		for _, id := range []access.RequestID{"first", "second"} {
			go func() {
				completion, err := service.Execute(context.Background(), "requester", id)
				if err != nil {
					t.Error(err)
				}
				results <- completion
			}()
		}
		for range 2 {
			<-blocking.started
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := service.InterruptActive(ctx); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if completion := awaitCompletion(t, results); completion.State != access.StateCancelled || completion.InterruptionCause != access.InterruptedByShutdown {
				t.Fatalf("interrupted execution = %+v", completion)
			}
		}
	})
}

func TestShutdownInterruptionStaysBoundedAndKeepsCommittedOutcomes(t *testing.T) {
	t.Run("statement ignoring cancellation cannot hold shutdown past its bound", func(t *testing.T) {
		fixture := newInterruptionFixture()
		release := make(chan struct{})
		started := make(chan struct{})
		fixture.executeTarget = func(context.Context, query.Execution) (query.ResultStream, error) {
			close(started)
			<-release
			return &failingStream{err: fmt.Errorf("execution interrupted: %w", context.Canceled)}, nil
		}
		service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
		if err != nil {
			t.Fatal(err)
		}
		finished := make(chan access.ExecutionCompletion, 1)
		go func() {
			completion, _ := service.Execute(context.Background(), "requester", "request")
			finished <- completion
		}()
		<-started
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		begun := time.Now()
		if err := service.InterruptActive(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unresponsive interruption = %v, want DeadlineExceeded", err)
		}
		if waited := time.Since(begun); waited > 2*time.Second {
			t.Fatalf("interruption waited %s past its bound", waited)
		}
		close(release)
		if completion := awaitCompletion(t, finished); completion.State != access.StateOutcomeUnknown || completion.InterruptionCause != access.InterruptedByShutdown {
			t.Fatalf("commit-phase interruption = %+v, want outcome_unknown by server_shutdown", completion)
		}
	})
	t.Run("interruption after commit keeps the stored success", func(t *testing.T) {
		fixture := newInterruptionFixture()
		saving, proceed := make(chan struct{}), make(chan struct{})
		fixture.persistResult = func(ctx context.Context) error {
			close(saving)
			<-proceed
			return ctx.Err()
		}
		service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
		if err != nil {
			t.Fatal(err)
		}
		finished := make(chan access.ExecutionCompletion, 1)
		go func() {
			completion, _ := service.Execute(context.Background(), "requester", "request")
			finished <- completion
		}()
		<-saving
		interrupted := make(chan error, 1)
		go func() { interrupted <- service.InterruptActive(context.Background()) }()
		time.Sleep(50 * time.Millisecond)
		close(proceed)
		completion := awaitCompletion(t, finished)
		if err := <-interrupted; err != nil {
			t.Fatal(err)
		}
		if completion.State != access.StateSucceeded || completion.ResultID == "" || completion.InterruptionCause != "" {
			t.Fatalf("committed execution after interruption = %+v", completion)
		}
	})
}
