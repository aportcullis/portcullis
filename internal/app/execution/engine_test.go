package execution_test

import (
	"context"
	"errors"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/dialectregistry"
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
			registry, err := dialectregistry.New(dialectregistry.Registration{Engine: connection.DBTypePostgreSQL, Adapter: fixture})
			if err != nil {
				t.Fatal(err)
			}
			svc, err := execution.NewWithDialects(fixture, fixture, fixture, fixture, credentialCodec{}, registry, fixture, "server", 1)
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

func TestRegisteredEngineUsesOneAdapterFromBindingToSingleUseExecution(t *testing.T) {
	t.Parallel()
	base := &executionFixture{request: access.Request{ID: "request", OrganizationID: "org", RequesterID: "requester", ConnectionID: "connection", State: access.StateApproved, Class: connection.ClassRead, PolicyVersion: 1, ConnectionConfigVersion: 1, ConnectionDBType: "mysql"}, verify: true}
	target := engineExecutionFixture{base, "mysql", "mysql"}
	selected := &selectedExecutionDialect{executionFixture: base}
	unused := &executionFixture{}
	registry, err := dialectregistry.New(dialectregistry.Registration{Engine: "postgresql", Adapter: unused}, dialectregistry.Registration{Engine: "mysql", Adapter: selected})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := execution.NewWithDialects(target, target, target, target, credentialCodec{}, registry, target, "server", 1)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Execute(context.Background(), "requester", "request")
	if err != nil || result.State != access.StateSucceeded {
		t.Fatalf("registered engine journey: %v %s", err, result.State)
	}
	if selected.binds != 1 || selected.executions != 1 || unused.executions != 0 {
		t.Fatalf("wrong adapter used: binds=%d selected=%d unused=%d", selected.binds, selected.executions, unused.executions)
	}
	if _, err := svc.Execute(context.Background(), "requester", "request"); !errors.Is(err, access.ErrNotExecutable) {
		t.Fatalf("replayed execution: %v", err)
	}
	if selected.executions != 1 {
		t.Fatal("replay reached target")
	}
}

type selectedExecutionDialect struct {
	*executionFixture
	binds int
}

func (d *selectedExecutionDialect) BindNamed(string, []query.Parameter) (string, []query.TypedValue, error) {
	d.binds++
	return "SELECT ?", []query.TypedValue{{Type: query.ParamInteger, Text: "1"}}, nil
}

func (d *selectedExecutionDialect) Execute(ctx context.Context, target connection.Target, mode connection.TLSMode, credential connection.Credential, request query.Execution) (query.ResultStream, error) {
	if request.SQL != "SELECT ?" {
		return nil, errors.New("foreign bound SQL")
	}
	return d.executionFixture.Execute(ctx, target, mode, credential, request)
}

func (*executionFixture) Redact(statement query.Statement) (query.Redaction, error) {
	return query.Redaction{SQL: statement.Text()}, nil
}
