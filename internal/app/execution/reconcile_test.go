package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/execution"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// reconcileFixture replays scripted reconciliation batches and records the requested cursors and batch sizes.
type reconcileFixture struct {
	*executionFixture
	batches    []access.ReconcileBatch
	failAt     int
	batchSizes []int
	cursors    []access.ReconcileCursor
	cancelAt   int
	cancel     context.CancelFunc
}

var (
	errReconcileBatch = errors.New("reconcile batch failed")
	errAttemptFailed  = errors.New("attempt recovery failed")
)

func (f *reconcileFixture) ReconcileExecutions(_ context.Context, _ identity.OrganizationID, after access.ReconcileCursor, batchSize int) (access.ReconcileBatch, error) {
	f.batchSizes = append(f.batchSizes, batchSize)
	f.cursors = append(f.cursors, after)
	call := len(f.batchSizes)
	if call == f.cancelAt {
		f.cancel()
	}
	if call == f.failAt {
		return access.ReconcileBatch{}, errReconcileBatch
	}
	if call > len(f.batches) {
		return access.ReconcileBatch{}, nil
	}
	return f.batches[call-1], nil
}

// cursorAt names the position after the attempt overdue since offset minutes before a fixed instant.
func cursorAt(offsetMinutes int, id access.RequestID) access.ReconcileCursor {
	return access.ReconcileCursor{Deadline: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC).Add(time.Duration(offsetMinutes) * time.Minute), RequestID: id}
}

func TestReconcileContinuesWhileFullBatchesAreListed(t *testing.T) {
	full := access.ExecutionReconcileBatchSize
	for _, scenario := range []struct {
		name      string
		batches   []access.ReconcileBatch
		failAt    int
		cancelAt  int
		wantCalls int
		wantErr   error
	}{
		{"partial batch ends the run", []access.ReconcileBatch{{Listed: 3, Recovered: 3}}, 0, 0, 1, nil},
		{"empty listing ends the run", []access.ReconcileBatch{{}}, 0, 0, 1, nil},
		{"full batch of raced rows continues", []access.ReconcileBatch{{Listed: full, Recovered: 40}, {Listed: full}, {Listed: 5, Recovered: 5}}, 0, 0, 3, nil},
		{"exactly one full batch checks once more", []access.ReconcileBatch{{Listed: full, Recovered: full}, {}}, 0, 0, 2, nil},
		{"batch failure stops the run", []access.ReconcileBatch{{Listed: full, Recovered: full}}, 2, 0, 2, errReconcileBatch},
		{"cancelled run stops between full batches", []access.ReconcileBatch{{Listed: full}, {Listed: full}, {Listed: full}}, 0, 1, 1, context.Canceled},
		{"first batch failure stops immediately", nil, 1, 0, 1, errReconcileBatch},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fixture := &reconcileFixture{executionFixture: newInterruptionFixture(), batches: scenario.batches, failAt: scenario.failAt, cancelAt: scenario.cancelAt, cancel: cancel}
			service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Reconcile(ctx)
			if !errors.Is(err, scenario.wantErr) || (scenario.wantErr == nil && err != nil) {
				t.Fatalf("Reconcile = %v, want %v", err, scenario.wantErr)
			}
			if len(fixture.batchSizes) != scenario.wantCalls {
				t.Fatalf("batches requested = %d, want %d", len(fixture.batchSizes), scenario.wantCalls)
			}
			for _, batchSize := range fixture.batchSizes {
				if batchSize != access.ExecutionReconcileBatchSize {
					t.Fatalf("batch size = %d, want %d", batchSize, access.ExecutionReconcileBatchSize)
				}
			}
		})
	}
}

func TestReconcileResumesAfterFailingAttemptsAndReportsThem(t *testing.T) {
	full := access.ExecutionReconcileBatchSize
	for _, scenario := range []struct {
		name        string
		batches     []access.ReconcileBatch
		wantSummary access.ReconcileSummary
	}{
		{"failing attempt in a full batch does not stop later batches", []access.ReconcileBatch{{Listed: full, Recovered: full - 1, Failed: 1, Next: cursorAt(1, "a"), FirstFailure: errAttemptFailed}, {Listed: 2, Recovered: 2, Next: cursorAt(2, "b")}}, access.ReconcileSummary{Recovered: full + 1, Failed: 1, FirstFailure: errAttemptFailed}},
		{"locked attempts are skipped and counted", []access.ReconcileBatch{{Listed: full, Recovered: full - 3, Skipped: 3, Next: cursorAt(1, "a")}, {Listed: 1, Skipped: 1, Next: cursorAt(3, "c")}}, access.ReconcileSummary{Recovered: full - 3, Skipped: 4}},
		{"every attempt failing still pages forward", []access.ReconcileBatch{{Listed: full, Failed: full, Next: cursorAt(1, "a"), FirstFailure: errAttemptFailed}, {Listed: full, Failed: full, Next: cursorAt(2, "b"), FirstFailure: errors.New("later failure")}, {Listed: 1, Failed: 1, Next: cursorAt(3, "c")}}, access.ReconcileSummary{Failed: 2*full + 1, FirstFailure: errAttemptFailed}},
		{"single partial batch keeps its counts", []access.ReconcileBatch{{Listed: 4, Recovered: 2, Skipped: 1, Failed: 1, Next: cursorAt(4, "d"), FirstFailure: errAttemptFailed}}, access.ReconcileSummary{Recovered: 2, Skipped: 1, Failed: 1, FirstFailure: errAttemptFailed}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := &reconcileFixture{executionFixture: newInterruptionFixture(), batches: scenario.batches}
			service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := service.Reconcile(context.Background())
			if err != nil {
				t.Fatalf("failing attempts failed the run: %v", err)
			}
			if summary.Recovered != scenario.wantSummary.Recovered || summary.Skipped != scenario.wantSummary.Skipped || summary.Failed != scenario.wantSummary.Failed || !errors.Is(summary.FirstFailure, scenario.wantSummary.FirstFailure) || (scenario.wantSummary.FirstFailure == nil) != (summary.FirstFailure == nil) {
				t.Fatalf("summary = %+v, want %+v", summary, scenario.wantSummary)
			}
			if len(fixture.cursors) != len(scenario.batches) || !fixture.cursors[0].IsStart() {
				t.Fatalf("cursors = %+v, want %d batches from the start", fixture.cursors, len(scenario.batches))
			}
			for idx := 1; idx < len(fixture.cursors); idx++ {
				if fixture.cursors[idx] != scenario.batches[idx-1].Next {
					t.Fatalf("batch %d resumed at %+v, want %+v", idx, fixture.cursors[idx], scenario.batches[idx-1].Next)
				}
			}
		})
	}
}
