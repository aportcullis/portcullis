package execution_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/aportcullis/portcullis/internal/app/execution"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"sync"
	"testing"
	"time"
)

type executionFixture struct {
	narrativeBody     string
	expectedCanonical []byte
	request           access.Request
	verify            bool
	acquired          bool
	executions        int
	execErr           error
	resultErr         error
	completion        access.ExecutionCompletion
	// queryTimeoutSeconds overrides the default policy's statement timeout when positive.
	queryTimeoutSeconds int
	// executeTarget replaces the canned target stream when set.
	executeTarget func(context.Context, query.Execution) (query.ResultStream, error)
	// persistResult replaces the canned snapshot-store outcome when set.
	persistResult func(context.Context) error
}

type blockingExecutionFixture struct {
	*executionFixture
	mu       sync.Mutex
	leases   map[access.RequestID]bool
	refusals int
	started  chan struct{}
	release  chan struct{}
}

func (f *blockingExecutionFixture) RecordExecutionRefusal(context.Context, identity.OrganizationID, access.RequestID, identity.UserID, string, audit.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refusals++
	return nil
}

func (f *blockingExecutionFixture) GetSealed(_ context.Context, _ identity.OrganizationID, id access.RequestID) (access.Request, access.SealedPayload, error) {
	r := f.request
	r.ID = id
	return r, access.SealedPayload{}, nil
}

func (f *blockingExecutionFixture) AcquireExecution(_ context.Context, _ identity.OrganizationID, id access.RequestID, _ identity.UserID, owner, attempt string, _ []byte, _ audit.Event) (access.ExecutionLease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.leases[id] {
		return access.ExecutionLease{}, access.ErrNotExecutable
	}
	f.leases[id] = true
	return access.ExecutionLease{RequestID: id, Owner: owner, AttemptID: attempt, Deadline: time.Now().Add(time.Minute)}, nil
}

func (f *blockingExecutionFixture) CompleteExecution(context.Context, identity.OrganizationID, access.ExecutionLease, access.ExecutionCompletion, audit.Event) error {
	return nil
}

func (f *blockingExecutionFixture) Execute(ctx context.Context, _ connection.Target, _ connection.TLSMode, _ connection.Credential, _ query.Execution) (query.ResultStream, error) {
	f.started <- struct{}{}
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %w", query.ErrInterruptedBeforeCommit, ctx.Err())
	case <-f.release:
		return &resultStream{}, nil
	}
}

func TestExecutionSaturationAndOwnerCancellationPreserveOtherLeases(t *testing.T) {
	f := &blockingExecutionFixture{
		executionFixture: &executionFixture{request: access.Request{OrganizationID: "org", RequesterID: "requester", ConnectionID: "connection", State: access.StateApproved, Class: connection.ClassRead, PolicyVersion: 1, ConnectionConfigVersion: 1, ConnectionDBType: "postgresql"}, verify: true},
		leases:           make(map[access.RequestID]bool), started: make(chan struct{}, 2), release: make(chan struct{}),
	}
	svc, err := execution.New(f, f, f, f, credentialCodec{}, f, f, "server", 2)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, second := make(chan access.ExecutionCompletion, 1), make(chan access.ExecutionCompletion, 1)
	go func() { c, _ := svc.Execute(ctx, "requester", "first"); first <- c }()
	go func() { c, _ := svc.Execute(ctx, "requester", "second"); second <- c }()
	for range 2 {
		select {
		case <-f.started:
		case <-ctx.Done():
			t.Fatal("workers did not start")
		}
	}
	for _, id := range []access.RequestID{"third", "first"} {
		if _, err := svc.Execute(ctx, "requester", id); !errors.Is(err, query.ErrResultBusy) {
			t.Fatalf("saturation accepted %s: %v", id, err)
		}
	}
	f.mu.Lock()
	refusals := f.refusals
	f.mu.Unlock()
	if refusals != 2 {
		t.Fatalf("missing saturation refusal audit: %d", refusals)
	}
	if err := svc.Cancel(ctx, "other", "first"); !errors.Is(err, access.ErrNotFound) {
		t.Fatalf("foreign cancellation: %v", err)
	}
	select {
	case <-first:
		t.Fatal("rejected request cancelled the owner")
	default:
	}
	if err := svc.Cancel(ctx, "requester", "first"); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-first:
		if c.State != access.StateCancelled {
			t.Fatalf("cancel outcome: %s", c.State)
		}
	case <-ctx.Done():
		t.Fatal("owner cancellation did not finish")
	}
	f.mu.Lock()
	count := len(f.leases)
	f.mu.Unlock()
	if count != 2 {
		t.Fatalf("refused request acquired a lease: %d", count)
	}
	close(f.release)
	select {
	case c := <-second:
		if c.State != access.StateSucceeded {
			t.Fatalf("other execution changed: %s", c.State)
		}
	case <-ctx.Done():
		t.Fatal("other execution did not finish")
	}
}

func (f *executionFixture) RecordExecutionRefusal(context.Context, identity.OrganizationID, access.RequestID, identity.UserID, string, audit.Event) error {
	return nil
}

func (f *executionFixture) GetExecution(context.Context, identity.OrganizationID, access.RequestID) (access.ExecutionCompletion, error) {
	return f.completion, nil
}

func (f *executionFixture) DefaultOrganizationID(context.Context) (identity.OrganizationID, error) {
	return "org", nil
}
func (f *executionFixture) GetSealed(context.Context, identity.OrganizationID, access.RequestID) (access.Request, access.SealedPayload, error) {
	return f.request, access.SealedPayload{}, nil
}
func (f *executionFixture) CurrentTarget(context.Context, identity.OrganizationID, connection.ConnectionID) (access.SubmitTarget, error) {
	policy := connection.DefaultPolicy()
	if f.queryTimeoutSeconds > 0 {
		policy.Limits.QueryTimeoutSeconds = f.queryTimeoutSeconds
	}
	return access.SubmitTarget{Policy: policy, ConfigVersion: 1, DBType: "postgresql"}, nil
}
func (f *executionFixture) TestMaterial(context.Context, identity.OrganizationID, connection.ConnectionID) (connection.Connection, connection.SealedCredential, error) {
	return connection.Connection{ConfigVersion: 1, DBType: connection.DBTypePostgreSQL}, connection.SealedCredential{}, nil
}
func (f *executionFixture) Open(identity.OrganizationID, access.RequestID, access.SealedPayload) (access.Payload, error) {
	return access.Payload{Title: f.request.Title, Body: f.narrativeBody, SQL: "SELECT :value", Params: []query.Parameter{{Name: "value", Value: query.TypedValue{Type: query.ParamInteger, Text: "1"}}}}, nil
}
func (f *executionFixture) Verify(canonical []byte, _ []byte, _ uint32) (bool, error) {
	if f.expectedCanonical != nil {
		return bytes.Equal(canonical, f.expectedCanonical), nil
	}
	return f.verify, nil
}

type credentialCodec struct{}

func (credentialCodec) Open(identity.OrganizationID, connection.ConnectionID, connection.SealedCredential) (connection.Credential, error) {
	return connection.Credential{}, nil
}
func (f *executionFixture) AcquireExecution(_ context.Context, _ identity.OrganizationID, _ access.RequestID, _ identity.UserID, owner, attempt string, _ []byte, _ audit.Event) (access.ExecutionLease, error) {
	if f.acquired {
		return access.ExecutionLease{}, access.ErrNotExecutable
	}
	f.acquired = true
	return access.ExecutionLease{RequestID: f.request.ID, Owner: owner, AttemptID: attempt, Deadline: time.Now().Add(time.Minute)}, nil
}
func (f *executionFixture) HeartbeatExecution(context.Context, identity.OrganizationID, access.ExecutionLease) error {
	return nil
}
func (f *executionFixture) CompleteExecution(_ context.Context, _ identity.OrganizationID, _ access.ExecutionLease, c access.ExecutionCompletion, _ audit.Event) error {
	f.completion = c
	f.request.State = c.State
	return nil
}
func (f *executionFixture) ReconcileExecutions(context.Context, identity.OrganizationID, access.ReconcileCursor, int) (access.ReconcileBatch, error) {
	return access.ReconcileBatch{}, nil
}

type statement string

func (s statement) Text() string { return string(s) }
func (f *executionFixture) ParseSingle(sql string) (query.Statement, error) {
	return statement(sql), nil
}
func (f *executionFixture) Classify(query.Statement) (query.StatementClass, error) {
	return query.ClassRead, nil
}
func (f *executionFixture) BindNamed(string, []query.Parameter) (string, []query.TypedValue, error) {
	return "SELECT $1", []query.TypedValue{{Type: query.ParamInteger, Text: "1"}}, nil
}
func (f *executionFixture) Execute(ctx context.Context, _ connection.Target, _ connection.TLSMode, _ connection.Credential, exec query.Execution) (query.ResultStream, error) {
	f.executions++
	if !f.acquired || exec.Ungoverned {
		return nil, errors.New("unleased or ungoverned target execution")
	}
	if f.execErr != nil {
		return nil, f.execErr
	}
	if f.executeTarget != nil {
		return f.executeTarget(ctx, exec)
	}
	return &resultStream{}, nil
}
func (f *executionFixture) Save(ctx context.Context, m query.SnapshotMetadata, _ []query.Column, _ [][]query.CellValue, _ int64) (query.SnapshotMetadata, error) {
	m.RowCount = 1
	m.ExpiresAt = time.Now().Add(query.ResultTTL)
	if f.persistResult != nil {
		if err := f.persistResult(ctx); err != nil {
			return query.SnapshotMetadata{}, err
		}
		return m, nil
	}
	if f.resultErr != nil {
		return query.SnapshotMetadata{}, f.resultErr
	}
	return m, nil
}

type resultStream struct{ read bool }

func (*resultStream) Columns() []query.Column {
	return []query.Column{{Name: "value", Logical: query.LogicalInt}}
}
func (s *resultStream) Next() bool {
	if s.read {
		return false
	}
	s.read = true
	return true
}
func (*resultStream) Row() []query.CellValue {
	return []query.CellValue{{Kind: query.CellInt, Text: "1"}}
}
func (*resultStream) Err() error          { return nil }
func (*resultStream) RowsAffected() int64 { return 1 }
func (*resultStream) Truncated() bool     { return false }
func (*resultStream) Close() error        { return nil }

func TestApprovedExecutionRejectsTamperingAndNeverRetriesUnknownOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name               string
		verify             bool
		execErr, resultErr error
		want               access.State
		calls              int
	}{
		{"success", true, nil, nil, access.StateSucceeded, 1},
		{"tampered", false, nil, nil, "", 0},
		{"unconfirmed timeout", true, context.DeadlineExceeded, nil, access.StateOutcomeUnknown, 1},
		{"commit connection lost", true, errors.New("connection lost"), nil, access.StateOutcomeUnknown, 1},
		{"SQL refusal", true, &query.ExecError{SQLState: "23505"}, nil, access.StateFailed, 1},
		{"store full", true, nil, query.ErrResultStoreFull, access.StateSucceeded, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &executionFixture{request: access.Request{ID: "request", OrganizationID: "org", RequesterID: "requester", ConnectionID: "connection", State: access.StateApproved, Class: connection.ClassRead, PolicyVersion: 1, ConnectionConfigVersion: 1, ConnectionDBType: "postgresql"}, verify: tc.verify, execErr: tc.execErr, resultErr: tc.resultErr}
			svc, err := execution.New(f, f, f, f, credentialCodec{}, f, f, "server", 2)
			if err != nil {
				t.Fatal(err)
			}
			completion, err := svc.Execute(context.Background(), "requester", "request")
			if tc.verify && err != nil {
				t.Fatal(err)
			}
			if !tc.verify && !errors.Is(err, access.ErrPayloadIntegrity) {
				t.Fatalf("tamper refusal: %v", err)
			}
			if f.executions != tc.calls || completion.State != tc.want {
				t.Fatalf("calls=%d outcome=%s", f.executions, completion.State)
			}
			if tc.verify {
				if _, err := svc.Execute(context.Background(), "requester", "request"); err == nil {
					t.Fatal("terminal replay accepted")
				}
				if f.executions != 1 {
					t.Fatal("target SQL was retried")
				}
			}
		})
	}
}

func TestExecutionRefusesChangedRequestNarrative(t *testing.T) {
	for _, tc := range []struct {
		name, title, body string
		refused           bool
	}{
		{"approved narrative", "Revenue", "Reviewed purpose", false},
		{"title changed", "Different review", "Reviewed purpose", true},
		{"body changed", "Revenue", "Unreviewed purpose", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &executionFixture{request: access.Request{ID: "request", OrganizationID: "org", RequesterID: "requester", ConnectionID: "connection", State: access.StateApproved, Class: connection.ClassRead, PolicyVersion: 1, ConnectionConfigVersion: 1, ConnectionDBType: "postgresql", Title: tc.title}, narrativeBody: tc.body}
			canonical, err := access.CanonicalPayload(access.ApprovalUnit{OrganizationID: "org", RequesterID: "requester", ConnectionID: "connection", Class: connection.ClassRead, PolicyVersion: 1, ConnectionConfigVersion: 1, Title: "Revenue", Body: "Reviewed purpose", SQL: "SELECT :value", Params: []query.Parameter{{Name: "value", Value: query.TypedValue{Type: query.ParamInteger, Text: "1"}}}})
			if err != nil {
				t.Fatal(err)
			}
			f.expectedCanonical = canonical
			svc, err := execution.New(f, f, f, f, credentialCodec{}, f, f, "server", 2)
			if err != nil {
				t.Fatal(err)
			}
			completion, err := svc.Execute(context.Background(), "requester", "request")
			if tc.refused {
				if !errors.Is(err, access.ErrPayloadIntegrity) || f.acquired || f.executions != 0 {
					t.Fatalf("changed narrative reached execution: err=%v acquired=%v calls=%d", err, f.acquired, f.executions)
				}
			} else if err != nil || completion.State != access.StateSucceeded || f.executions != 1 {
				t.Fatalf("approved narrative execution: err=%v state=%v calls=%d", err, completion.State, f.executions)
			}
		})
	}
}
