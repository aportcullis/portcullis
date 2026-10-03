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
	"github.com/aportcullis/portcullis/internal/infra/executionguard"
)

type recordingAdmission struct{ outcomes []execution.TargetOutcome }

func (a *recordingAdmission) Allow(connection.ConnectionID) (func(execution.TargetOutcome), error) {
	return func(outcome execution.TargetOutcome) { a.outcomes = append(a.outcomes, outcome) }, nil
}

func TestRepeatedOwnerCancellationDoesNotBlockAnotherRequester(t *testing.T) {
	guard := executionguard.New()
	fixture := &blockingExecutionFixture{
		executionFixture: &executionFixture{request: access.Request{OrganizationID: "org", RequesterID: "requester", ConnectionID: "connection", State: access.StateApproved, Class: connection.ClassRead, PolicyVersion: 1, ConnectionConfigVersion: 1, ConnectionDBType: "postgresql"}, verify: true},
		leases:           make(map[access.RequestID]bool), started: make(chan struct{}, 1), release: make(chan struct{}),
	}
	service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
	if err != nil {
		t.Fatal(err)
	}
	service.WithAdmission(guard)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for attempt := range 6 {
		id := access.RequestID(fmt.Sprintf("cancelled-%d", attempt))
		finished := make(chan struct{})
		var completion access.ExecutionCompletion
		var executionErr error
		go func() { completion, executionErr = service.Execute(ctx, "requester", id); close(finished) }()
		select {
		case <-fixture.started:
		case <-ctx.Done():
			t.Fatal("execution did not start")
		}
		if err := service.Cancel(ctx, "requester", id); err != nil {
			t.Fatal(err)
		}
		select {
		case <-finished:
		case <-ctx.Done():
			t.Fatal("cancellation did not finish")
		}
		if executionErr != nil || completion.State != access.StateOutcomeUnknown || completion.ResultID != "" {
			t.Fatalf("cancelled execution: %+v, %v", completion, executionErr)
		}
	}
	other := &executionFixture{request: access.Request{ID: "other-request", OrganizationID: "org", RequesterID: "other", ConnectionID: "connection", State: access.StateApproved, Class: connection.ClassRead, PolicyVersion: 1, ConnectionConfigVersion: 1, ConnectionDBType: "postgresql"}, verify: true}
	otherService, err := execution.New(other, other, other, other, credentialCodec{}, other, other, "server", 1)
	if err != nil {
		t.Fatal(err)
	}
	completion, err := otherService.WithAdmission(guard).Execute(ctx, "other", "other-request")
	if err != nil || completion.State != access.StateSucceeded {
		t.Fatalf("cancellations blocked another requester: %+v, %v", completion, err)
	}
}

func TestBreakerMeasuresTargetHealthInsteadOfSQLSuccess(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		err        error
		leaseTaken bool
		want       execution.TargetOutcome
	}{
		{"successful target", nil, false, execution.TargetHealthy},
		{"constraint violation", &query.ExecError{SQLState: "23505"}, false, execution.TargetHealthy},
		{"syntax error", &query.ExecError{SQLState: "42601"}, false, execution.TargetHealthy},
		{"connection lost", &query.ExecError{SQLState: "08006"}, false, execution.TargetUnhealthy},
		{"target unreachable", &connection.TestError{Bucket: connection.TestBucketUnreachable}, false, execution.TargetUnhealthy},
		{"policy deadline", context.DeadlineExceeded, false, execution.TargetInconclusive},
		{"owner cancellation", context.Canceled, false, execution.TargetInconclusive},
		{"local response bound", query.ErrResponseLimit, false, execution.TargetInconclusive},
		{"unconfirmed transport failure", errors.New("transport disconnected"), false, execution.TargetUnhealthy},
		{"lease race loser", nil, true, execution.TargetNotAttempted},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := &executionFixture{request: access.Request{ID: "request", OrganizationID: "org", RequesterID: "requester", ConnectionID: "connection", State: access.StateApproved, Class: connection.ClassRead, PolicyVersion: 1, ConnectionConfigVersion: 1, ConnectionDBType: "postgresql"}, verify: true, execErr: scenario.err, acquired: scenario.leaseTaken}
			admission := &recordingAdmission{}
			service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
			if err != nil {
				t.Fatal(err)
			}
			completion, err := service.WithAdmission(admission).Execute(context.Background(), "requester", "request")
			if scenario.leaseTaken && !errors.Is(err, access.ErrNotExecutable) {
				t.Fatalf("lease race: %v", err)
			}
			if !scenario.leaseTaken && err != nil {
				t.Fatal(err)
			}
			if len(admission.outcomes) != 1 || admission.outcomes[0] != scenario.want {
				t.Fatalf("target outcome = %v, want %v", admission.outcomes, scenario.want)
			}
			if scenario.want == execution.TargetInconclusive && (completion.State != access.StateOutcomeUnknown || completion.ResultID != "") {
				t.Fatalf("inconclusive health must preserve uncertain execution without a result: %+v", completion)
			}
		})
	}
}
