package execution_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/execution"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

type recordingAdmission struct{ outcomes []execution.TargetOutcome }

func (a *recordingAdmission) Allow(connection.ConnectionID) (func(execution.TargetOutcome), error) {
	return func(outcome execution.TargetOutcome) { a.outcomes = append(a.outcomes, outcome) }, nil
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
		{"unconfirmed outcome", context.DeadlineExceeded, false, execution.TargetUnhealthy},
		{"lease race loser", nil, true, execution.TargetNotAttempted},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := &executionFixture{request: access.Request{ID: "request", OrganizationID: "org", RequesterID: "requester", ConnectionID: "connection", State: access.StateApproved, Class: connection.ClassRead, PolicyVersion: 1, ConnectionConfigVersion: 1, ConnectionDBType: "postgresql"}, verify: true, execErr: scenario.err, acquired: scenario.leaseTaken}
			admission := &recordingAdmission{}
			service, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.WithAdmission(admission).Execute(context.Background(), "requester", "request")
			if scenario.leaseTaken && !errors.Is(err, access.ErrNotExecutable) {
				t.Fatalf("lease race: %v", err)
			}
			if !scenario.leaseTaken && err != nil {
				t.Fatal(err)
			}
			if len(admission.outcomes) != 1 || admission.outcomes[0] != scenario.want {
				t.Fatalf("target outcome = %v, want %v", admission.outcomes, scenario.want)
			}
		})
	}
}
