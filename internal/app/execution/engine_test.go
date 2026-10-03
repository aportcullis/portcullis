package execution_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/execution"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

type engineExecutionFixture struct {
	*executionFixture
	targetEngine   string
	materialEngine connection.DBType
}

func (f engineExecutionFixture) CurrentTarget(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (access.SubmitTarget, error) {
	target, err := f.executionFixture.CurrentTarget(ctx, org, id)
	target.DBType = f.targetEngine
	return target, err
}

func (f engineExecutionFixture) TestMaterial(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (connection.Connection, connection.SealedCredential, error) {
	material, credential, err := f.executionFixture.TestMaterial(ctx, org, id)
	material.DBType = f.materialEngine
	return material, credential, err
}

func TestExecutionRejectsChangedOrUnregisteredEngineBeforeLease(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, approved, target string
		material               connection.DBType
		want                   error
	}{
		{"changed target", "postgresql", "mysql", "postgresql", access.ErrNotExecutable},
		{"changed material", "postgresql", "postgresql", "mysql", access.ErrNotExecutable},
		{"unregistered engine", "mysql", "mysql", "mysql", connection.ErrUnsupportedDBType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &executionFixture{request: access.Request{ID: "request", OrganizationID: "org", RequesterID: "requester", ConnectionID: "connection", State: access.StateApproved, Class: connection.ClassRead, PolicyVersion: 1, ConnectionConfigVersion: 1, ConnectionDBType: tc.approved}, verify: true}
			fixture := engineExecutionFixture{base, tc.target, tc.material}
			svc, err := execution.New(fixture, fixture, fixture, fixture, credentialCodec{}, fixture, fixture, "server", 1)
			if err != nil {
				t.Fatal(err)
			}
			_, err = svc.Execute(context.Background(), "requester", "request")
			if !errors.Is(err, tc.want) {
				t.Fatalf("engine refusal: got %v want %v", err, tc.want)
			}
			if base.acquired || base.executions != 0 {
				t.Fatalf("engine refusal consumed lease or executed: lease=%v calls=%d", base.acquired, base.executions)
			}
		})
	}
}
