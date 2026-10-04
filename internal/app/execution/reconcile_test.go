package execution_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/execution"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// reconcileFixture replays scripted reconciliation batches and records the requested batch sizes.
type reconcileFixture struct {
	*executionFixture
	batches    []access.ReconcileBatch
	failAt     int
	batchSizes []int
	cancelAt   int
	cancel     context.CancelFunc
}

var errReconcileBatch = errors.New("reconcile batch failed")

func (f *reconcileFixture) ReconcileExecutions(_ context.Context, _ identity.OrganizationID, batchSize int) (access.ReconcileBatch, error) {
	f.batchSizes = append(f.batchSizes, batchSize)
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
			err = service.Reconcile(ctx)
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
