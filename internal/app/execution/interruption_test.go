package execution_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/execution"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

// failingStream ends without rows and reports the dialect's terminal error.
type failingStream struct{ err error }

func (*failingStream) Columns() []query.Column { return nil }
func (*failingStream) Next() bool              { return false }
func (*failingStream) Row() []query.CellValue  { return nil }
func (s *failingStream) Err() error            { return s.err }
func (*failingStream) RowsAffected() int64     { return 0 }
func (*failingStream) Truncated() bool         { return false }
func (*failingStream) Close() error            { return nil }

// timedStream emulates a statement that finishes after its duration unless the context ends first, classifying the interruption as the dialect does before COMMIT.
type timedStream struct {
	ctx      context.Context
	finishAt time.Time
	read     bool
	err      error
}

func (*timedStream) Columns() []query.Column {
	return []query.Column{{Name: "value", Logical: query.LogicalInt}}
}

func (s *timedStream) Next() bool {
	if s.read || s.err != nil {
		return false
	}
	timer := time.NewTimer(time.Until(s.finishAt))
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
		s.err = fmt.Errorf("%w: %w", query.ErrInterruptedBeforeCommit, s.ctx.Err())
		return false
	case <-timer.C:
		s.read = true
		return true
	}
}
func (*timedStream) Row() []query.CellValue {
	return []query.CellValue{{Kind: query.CellInt, Text: "1"}}
}
func (s *timedStream) Err() error        { return s.err }
func (*timedStream) RowsAffected() int64 { return 1 }
func (*timedStream) Truncated() bool     { return false }
func (*timedStream) Close() error        { return nil }

func newInterruptionFixture() *executionFixture {
	return &executionFixture{request: access.Request{ID: "request", OrganizationID: "org", RequesterID: "requester", ConnectionID: "connection", State: access.StateApproved, Class: connection.ClassRead, PolicyVersion: 1, ConnectionConfigVersion: 1, ConnectionDBType: "postgresql"}, verify: true}
}

func TestInterruptedExecutionRecordsOutcomeByCommitPhase(t *testing.T) {
	beforeCommit := func(cause error) error { return fmt.Errorf("%w: %w", query.ErrInterruptedBeforeCommit, cause) }
	for _, scenario := range []struct {
		name       string
		executeErr error
		streamErr  error
		wantState  access.State
		wantHealth execution.TargetOutcome
	}{
		{"server statement timeout rolled back", nil, &query.ExecError{SQLState: "57014"}, access.StateFailed, execution.TargetInconclusive},
		{"local deadline before commit", beforeCommit(context.DeadlineExceeded), nil, access.StateFailed, execution.TargetInconclusive},
		{"cancelled while dialing", beforeCommit(context.Canceled), nil, access.StateCancelled, execution.TargetInconclusive},
		{"cancelled while streaming rows", nil, beforeCommit(context.Canceled), access.StateCancelled, execution.TargetInconclusive},
		{"cancelled during commit", nil, fmt.Errorf("execution interrupted: %w", context.Canceled), access.StateOutcomeUnknown, execution.TargetInconclusive},
		{"deadline during commit", nil, fmt.Errorf("execution interrupted: %w", context.DeadlineExceeded), access.StateOutcomeUnknown, execution.TargetInconclusive},
		{"connection lost before commit", nil, &query.ExecError{SQLState: "08006"}, access.StateOutcomeUnknown, execution.TargetUnhealthy},
		{"commit marker without an interruption cause", nil, query.ErrInterruptedBeforeCommit, access.StateOutcomeUnknown, execution.TargetUnhealthy},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newInterruptionFixture()
			fixture.execErr = scenario.executeErr
			if scenario.streamErr != nil {
				fixture.executeTarget = func(context.Context, query.Execution) (query.ResultStream, error) {
					return &failingStream{err: scenario.streamErr}, nil
				}
			}
			admission := &recordingAdmission{}
			service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
			if err != nil {
				t.Fatal(err)
			}
			completion, err := service.WithAdmission(admission).Execute(context.Background(), "requester", "request")
			if err != nil {
				t.Fatal(err)
			}
			if completion.State != scenario.wantState || completion.ResultID != "" {
				t.Fatalf("completion = %+v, want state %s without a result", completion, scenario.wantState)
			}
			if fixture.completion.State != scenario.wantState {
				t.Fatalf("recorded state = %s, want %s", fixture.completion.State, scenario.wantState)
			}
			if len(admission.outcomes) != 1 || admission.outcomes[0] != scenario.wantHealth {
				t.Fatalf("target health = %v, want %v", admission.outcomes, scenario.wantHealth)
			}
		})
	}
}

func TestStatementWithinServerTimeoutIsNotCutOffBySetupLatency(t *testing.T) {
	t.Parallel()
	fixture := newInterruptionFixture()
	fixture.queryTimeoutSeconds = 1
	fixture.executeTarget = func(ctx context.Context, exec query.Execution) (query.ResultStream, error) {
		// Dial, BEGIN and the catalog gate consume time before the server starts the statement clock.
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: %w", query.ErrInterruptedBeforeCommit, ctx.Err())
		case <-time.After(400 * time.Millisecond):
		}
		statementDuration := time.Duration(exec.TimeoutSeconds)*time.Second - 100*time.Millisecond
		return &timedStream{ctx: ctx, finishAt: time.Now().Add(statementDuration)}, nil
	}
	service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
	if err != nil {
		t.Fatal(err)
	}
	completion, err := service.Execute(context.Background(), "requester", "request")
	if err != nil || completion.State != access.StateSucceeded {
		t.Fatalf("statement inside its server timeout = %+v, %v; want succeeded", completion, err)
	}
}

// startBlockedExecution runs one execution whose statement waits for release or cancellation.
func startBlockedExecution(t *testing.T, fixture *executionFixture, admission execution.Admission) (*execution.Service, <-chan access.ExecutionCompletion, chan<- struct{}) {
	t.Helper()
	started, release := make(chan struct{}), make(chan struct{})
	fixture.executeTarget = func(ctx context.Context, _ query.Execution) (query.ResultStream, error) {
		close(started)
		select {
		case <-ctx.Done():
			return &failingStream{err: fmt.Errorf("%w: %w", query.ErrInterruptedBeforeCommit, ctx.Err())}, nil
		case <-release:
			return &resultStream{}, nil
		}
	}
	service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
	if err != nil {
		t.Fatal(err)
	}
	if admission != nil {
		service.WithAdmission(admission)
	}
	finished := make(chan access.ExecutionCompletion, 1)
	go func() {
		completion, err := service.Execute(context.Background(), "requester", "request")
		if err != nil {
			t.Error(err)
		}
		finished <- completion
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("execution did not start")
	}
	return service, finished, release
}

func awaitCompletion(t *testing.T, finished <-chan access.ExecutionCompletion) access.ExecutionCompletion {
	t.Helper()
	select {
	case completion := <-finished:
		return completion
	case <-time.After(5 * time.Second):
		t.Fatal("execution did not finish")
		return access.ExecutionCompletion{}
	}
}

func TestOwnerCancellationRacesWithCompletion(t *testing.T) {
	t.Run("cancel before commit records cancelled", func(t *testing.T) {
		admission := &recordingAdmission{}
		service, finished, _ := startBlockedExecution(t, newInterruptionFixture(), admission)
		if err := service.Cancel(context.Background(), "requester", "request"); err != nil {
			t.Fatal(err)
		}
		completion := awaitCompletion(t, finished)
		if completion.State != access.StateCancelled || completion.ResultID != "" {
			t.Fatalf("cancelled execution = %+v", completion)
		}
		if len(admission.outcomes) != 1 || admission.outcomes[0] != execution.TargetInconclusive {
			t.Fatalf("cancellation health = %v", admission.outcomes)
		}
	})
	t.Run("cancel after completion is refused and keeps success", func(t *testing.T) {
		fixture := newInterruptionFixture()
		service, finished, release := startBlockedExecution(t, fixture, nil)
		close(release)
		if completion := awaitCompletion(t, finished); completion.State != access.StateSucceeded {
			t.Fatalf("completed execution = %+v", completion)
		}
		if err := service.Cancel(context.Background(), "requester", "request"); !errors.Is(err, access.ErrNotExecutable) {
			t.Fatalf("late cancellation = %v, want ErrNotExecutable", err)
		}
		if fixture.completion.State != access.StateSucceeded {
			t.Fatalf("late cancellation rewrote outcome: %s", fixture.completion.State)
		}
	})
	t.Run("foreign cancel cannot interrupt the owner", func(t *testing.T) {
		service, finished, release := startBlockedExecution(t, newInterruptionFixture(), nil)
		if err := service.Cancel(context.Background(), "intruder", "request"); !errors.Is(err, access.ErrNotFound) {
			t.Fatalf("foreign cancellation = %v, want ErrNotFound", err)
		}
		close(release)
		if completion := awaitCompletion(t, finished); completion.State != access.StateSucceeded {
			t.Fatalf("foreign cancellation changed outcome: %+v", completion)
		}
	})
	t.Run("repeated cancel records cancelled once", func(t *testing.T) {
		fixture := newInterruptionFixture()
		service, finished, _ := startBlockedExecution(t, fixture, nil)
		if err := service.Cancel(context.Background(), "requester", "request"); err != nil {
			t.Fatal(err)
		}
		completion := awaitCompletion(t, finished)
		if err := service.Cancel(context.Background(), "requester", "request"); !errors.Is(err, access.ErrNotExecutable) {
			t.Fatalf("second cancellation = %v, want ErrNotExecutable", err)
		}
		if completion.State != access.StateCancelled || fixture.completion.State != access.StateCancelled {
			t.Fatalf("repeated cancellation = %+v recorded %s", completion, fixture.completion.State)
		}
	})
}
