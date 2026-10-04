package execution_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/execution"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

// errBoundlessSave reports that snapshot persistence ran without a deadline of its own.
var errBoundlessSave = errors.New("snapshot persistence had no deadline")

func TestResultPersistenceRecordsSafeUnavailableReasonWithoutFailingSQL(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		persist    func(context.Context) error
		executeErr error
		wantState  access.State
		wantStored bool
		wantReason access.ResultUnavailableReason
	}{
		{"stored snapshot", nil, nil, access.StateSucceeded, true, ""},
		{"result store full", func(context.Context) error { return query.ErrResultStoreFull }, nil, access.StateSucceeded, false, access.ResultUnavailableStoreFull},
		{"wrapped store full", func(context.Context) error { return errors.Join(errors.New("admission"), query.ErrResultStoreFull) }, nil, access.StateSucceeded, false, access.ResultUnavailableStoreFull},
		{"store error carrying sensitive text", func(context.Context) error { return errors.New("insert failed: password=hunter2 row=secret") }, nil, access.StateSucceeded, false, access.ResultUnavailablePersistenceFailed},
		{"SQL refusal never reaches persistence", func(context.Context) error { return errors.New("unexpected save") }, &query.ExecError{SQLState: "23505"}, access.StateFailed, false, ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newInterruptionFixture()
			fixture.persistResult = scenario.persist
			fixture.execErr = scenario.executeErr
			service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
			if err != nil {
				t.Fatal(err)
			}
			completion, err := service.Execute(context.Background(), "requester", "request")
			if err != nil {
				t.Fatal(err)
			}
			recorded := fixture.completion
			if recorded.State != scenario.wantState || (recorded.ResultID != "") != scenario.wantStored || recorded.ResultUnavailableReason != scenario.wantReason {
				t.Fatalf("recorded completion = %+v, want state %s stored=%v reason %q", recorded, scenario.wantState, scenario.wantStored, scenario.wantReason)
			}
			if completion.ResultUnavailableReason != scenario.wantReason || !recorded.Valid() {
				t.Fatalf("returned completion = %+v", completion)
			}
			if strings.Contains(string(recorded.ResultUnavailableReason), "hunter2") {
				t.Fatal("store error text reached execution evidence")
			}
		})
	}
}

func TestResultPersistenceSurvivesCancellationAfterTargetSuccess(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		interrupt func(*execution.Service) error
	}{
		{"owner cancel after the statement committed", func(service *execution.Service) error {
			return service.Cancel(context.Background(), "requester", "request")
		}},
		{"requester disconnect after the statement committed", nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newInterruptionFixture()
			saving, proceed := make(chan struct{}), make(chan struct{})
			fixture.persistResult = func(ctx context.Context) error {
				close(saving)
				<-proceed
				if _, bounded := ctx.Deadline(); !bounded {
					return errBoundlessSave
				}
				return ctx.Err()
			}
			service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
			if err != nil {
				t.Fatal(err)
			}
			requestCtx, disconnect := context.WithCancel(context.Background())
			defer disconnect()
			finished := make(chan access.ExecutionCompletion, 1)
			go func() {
				completion, err := service.Execute(requestCtx, "requester", "request")
				if err != nil {
					t.Error(err)
				}
				finished <- completion
			}()
			select {
			case <-saving:
			case <-time.After(5 * time.Second):
				t.Fatal("persistence did not start")
			}
			if scenario.interrupt != nil {
				if err := scenario.interrupt(service); err != nil {
					t.Fatal(err)
				}
			} else {
				disconnect()
			}
			close(proceed)
			completion := awaitCompletion(t, finished)
			if completion.State != access.StateSucceeded || completion.ResultID == "" || completion.ResultUnavailableReason != "" {
				t.Fatalf("committed result after interruption = %+v, want a stored success", completion)
			}
		})
	}
}
