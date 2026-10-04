package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/google/uuid"
)

func TestExecutionLeaseExecutesOnceAndFencesLateCompletion(t *testing.T) {
	f := newReqFixture(t)
	r := approvedExecutionRequest(t, f)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var acquired []access.ExecutionLease
	for range 8 {
		wg.Go(func() {
			lease, err := f.requests.AcquireExecution(ctx, f.org, r.ID, f.requester, "server-one", uuid.NewString(), r.Digest, reqEvent(audit.Action("EXECUTION_STARTED"), r.ID))
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				acquired = append(acquired, lease)
			} else if !errors.Is(err, access.ErrNotExecutable) {
				t.Errorf("acquire: %v", err)
			}
		})
	}
	wg.Wait()
	if len(acquired) != 1 {
		t.Fatalf("successful execution leases = %d, want 1", len(acquired))
	}
	lease := acquired[0]
	wrong := lease
	wrong.AttemptID = uuid.NewString()
	if err := f.requests.HeartbeatExecution(ctx, f.org, wrong); !errors.Is(err, access.ErrLeaseLost) {
		t.Fatalf("foreign attempt heartbeat: %v", err)
	}
	if err := f.requests.HeartbeatExecution(ctx, f.org, lease); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `update public.query_executions set deadline = $1 where request_id = $2`, time.Now().Add(-time.Minute), string(r.ID)); err != nil {
		t.Fatal(err)
	}
	batch, err := f.requests.ReconcileExecutions(ctx, f.org, access.ExecutionReconcileBatchSize)
	if err != nil || batch.Recovered != 1 {
		t.Fatalf("reconcile = %+v, %v", batch, err)
	}
	if err := f.requests.CompleteExecution(ctx, f.org, lease, access.ExecutionCompletion{State: access.StateSucceeded}, reqEvent(audit.Action("EXECUTION_FINISHED"), r.ID)); !errors.Is(err, access.ErrLeaseLost) {
		t.Fatalf("late completion: %v", err)
	}
	view, _, err := f.requests.Get(ctx, f.org, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Request.State != access.StateOutcomeUnknown {
		t.Fatalf("late completion overwrote state: %s", view.Request.State)
	}
	var started, finished, late int
	if err := f.pool.QueryRow(ctx, `select count(*) filter (where action='EXECUTION_STARTED'), count(*) filter (where action='EXECUTION_FINISHED'), count(*) filter (where action='LATE_COMPLETION_OBSERVED') from public.audit_events where target_id=$1`, string(r.ID)).Scan(&started, &finished, &late); err != nil {
		t.Fatal(err)
	}
	if started != 1 || finished != 1 || late != 1 {
		t.Fatalf("audit counts = %d, %d, %d", started, finished, late)
	}
}

func TestExecutionStartAuditFailureRollsBackLeaseAndExpiredOwnerCannotRevive(t *testing.T) {
	f := newReqFixtureFresh(t)
	request := approvedExecutionRequest(t, f)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `create function public.reject_execution_start() returns trigger language plpgsql as $$ begin raise exception 'audit refused'; end $$; create trigger reject_execution_start before insert on public.audit_events for each row when (new.action='EXECUTION_STARTED') execute function public.reject_execution_start()`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.AcquireExecution(ctx, f.org, request.ID, f.requester, "server", uuid.NewString(), request.Digest, reqEvent(audit.ActionExecutionStarted, request.ID)); err == nil {
		t.Fatal("lease committed without audit")
	}
	var count int
	if err := f.pool.QueryRow(ctx, `select count(*) from public.query_executions where request_id=$1`, string(request.ID)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed audit retained execution lease")
	}
	if _, err := f.pool.Exec(ctx, `drop trigger reject_execution_start on public.audit_events`); err != nil {
		t.Fatal(err)
	}
	lease, err := f.requests.AcquireExecution(ctx, f.org, request.ID, f.requester, "server", uuid.NewString(), request.Digest, reqEvent(audit.ActionExecutionStarted, request.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `update public.query_executions set deadline=$1 where request_id=$2`, time.Now().Add(-time.Second), string(request.ID)); err != nil {
		t.Fatal(err)
	}
	if err = f.requests.HeartbeatExecution(ctx, f.org, lease); !errors.Is(err, access.ErrLeaseLost) {
		t.Fatalf("expired owner revived: %v", err)
	}
	if err = f.requests.CompleteExecution(ctx, f.org, lease, access.ExecutionCompletion{State: access.StateSucceeded}, reqEvent(audit.ActionExecutionFinished, request.ID)); !errors.Is(err, access.ErrLeaseLost) {
		t.Fatalf("overdue completion not fenced: %v", err)
	}
	if _, err = f.pool.Exec(ctx, `update public.query_executions set outcome='succeeded' where request_id=$1`, string(request.ID)); err == nil {
		t.Fatal("terminal execution evidence rewritten")
	}
}

func TestExecutionLeaseRefusesForeignRequesterAndChangedPayload(t *testing.T) {
	f := newReqFixture(t)
	r := approvedExecutionRequest(t, f)
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		org    identity.OrganizationID
		actor  identity.UserID
		digest []byte
	}{
		{"foreign requester", f.org, f.user, r.Digest},
		{"foreign organization", identity.OrganizationID(uuid.NewString()), f.requester, r.Digest},
		{"changed digest", f.org, f.requester, []byte("tampered")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.requests.AcquireExecution(ctx, tc.org, r.ID, tc.actor, "server", uuid.NewString(), tc.digest, reqEvent(audit.Action("EXECUTION_STARTED"), r.ID))
			if err == nil {
				t.Fatal("unauthorized execution acquired lease")
			}
		})
	}
	lease, err := f.requests.AcquireExecution(ctx, f.org, r.ID, f.requester, "server", uuid.NewString(), r.Digest, reqEvent(audit.Action("EXECUTION_STARTED"), r.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.requests.CompleteExecution(ctx, f.org, lease, access.ExecutionCompletion{State: access.StateSucceeded, RowsAffected: 7}, reqEvent(audit.Action("EXECUTION_FINISHED"), r.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.AcquireExecution(ctx, f.org, r.ID, f.requester, "server", uuid.NewString(), r.Digest, reqEvent(audit.Action("EXECUTION_STARTED"), r.ID)); !errors.Is(err, access.ErrNotExecutable) {
		t.Fatalf("replay: %v", err)
	}
}

func approvedExecutionRequest(t *testing.T, f reqFixture) access.Request {
	t.Helper()
	cid := f.liveConn(t, 0)
	draft := f.draft(t, cid)
	submitted, err := draft.Submitted(submitSnapshot(f.pin(t, cid), 0), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	approved, err := submitted.Approved(time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.requests.Submit(context.Background(), approved, 1, time.Hour, reqEvent(audit.ActionAccessRequestSubmitted, draft.ID))
	if err != nil {
		t.Fatal(err)
	}
	return view.Request
}

// leaseSpan reads the recorded gap between an execution's heartbeat and its deadline.
func leaseSpan(t *testing.T, f reqFixture, id access.RequestID) time.Duration {
	t.Helper()
	var heartbeat, deadline time.Time
	if err := f.pool.QueryRow(context.Background(), `select heartbeat, deadline from public.query_executions where request_id=$1`, string(id)).Scan(&heartbeat, &deadline); err != nil {
		t.Fatal(err)
	}
	return deadline.Sub(heartbeat)
}

func TestExecutionLeaseUsesDomainDurationAndReconcileHonorsBatchSize(t *testing.T) {
	f := newReqFixtureFresh(t)
	ctx := context.Background()
	leases := make([]access.ExecutionLease, 0, 3)
	for range 3 {
		request := approvedExecutionRequest(t, f)
		lease, err := f.requests.AcquireExecution(ctx, f.org, request.ID, f.requester, "server", uuid.NewString(), request.Digest, reqEvent(audit.ActionExecutionStarted, request.ID))
		if err != nil {
			t.Fatal(err)
		}
		if span := leaseSpan(t, f, request.ID); span != access.ExecutionLeaseDuration {
			t.Fatalf("acquired lease span = %s, want %s", span, access.ExecutionLeaseDuration)
		}
		leases = append(leases, lease)
	}
	if err := f.requests.HeartbeatExecution(ctx, f.org, leases[0]); err != nil {
		t.Fatal(err)
	}
	if span := leaseSpan(t, f, leases[0].RequestID); span != access.ExecutionLeaseDuration {
		t.Fatalf("renewed lease span = %s, want %s", span, access.ExecutionLeaseDuration)
	}
	if _, err := f.pool.Exec(ctx, `update public.query_executions set deadline = now() - interval '1 minute' where organization_id = $1`, string(f.org)); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		batchSize int
		want      access.ReconcileBatch
	}{
		{2, access.ReconcileBatch{Listed: 2, Recovered: 2}},
		{2, access.ReconcileBatch{Listed: 1, Recovered: 1}},
		{2, access.ReconcileBatch{}},
	} {
		batch, err := f.requests.ReconcileExecutions(ctx, f.org, step.batchSize)
		if err != nil || batch != step.want {
			t.Fatalf("reconcile batch = %+v, %v; want %+v", batch, err, step.want)
		}
	}
	if err := f.requests.HeartbeatExecution(ctx, f.org, leases[1]); !errors.Is(err, access.ErrLeaseLost) {
		t.Fatalf("recovered owner heartbeat = %v, want ErrLeaseLost", err)
	}
	for _, batchSize := range []int{0, -1} {
		if _, err := f.requests.ReconcileExecutions(ctx, f.org, batchSize); !errors.Is(err, access.ErrInvalidRequest) {
			t.Fatalf("batch size %d = %v, want ErrInvalidRequest", batchSize, err)
		}
	}
}

func TestExecutionCompletionRecordsResultUnavailableReasonAsEvidence(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	for _, scenario := range []struct {
		name       string
		completion access.ExecutionCompletion
		wantReason string
		wantState  access.State
	}{
		{"store full success", access.ExecutionCompletion{State: access.StateSucceeded, RowsAffected: 3, ResultUnavailableReason: access.ResultUnavailableStoreFull}, "result_store_full", access.StateSucceeded},
		{"persistence failure success", access.ExecutionCompletion{State: access.StateSucceeded, ResultUnavailableReason: access.ResultUnavailablePersistenceFailed}, "result_persistence_failed", access.StateSucceeded},
		{"stored success", access.ExecutionCompletion{State: access.StateSucceeded}, "", access.StateSucceeded},
		{"forged free-text reason", access.ExecutionCompletion{State: access.StateSucceeded, ResultUnavailableReason: "password=hunter2"}, "", access.StateExecuting},
		{"reason on a failed outcome", access.ExecutionCompletion{State: access.StateFailed, ResultUnavailableReason: access.ResultUnavailableStoreFull}, "", access.StateExecuting},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			request := approvedExecutionRequest(t, f)
			lease, err := f.requests.AcquireExecution(ctx, f.org, request.ID, f.requester, "server", uuid.NewString(), request.Digest, reqEvent(audit.ActionExecutionStarted, request.ID))
			if err != nil {
				t.Fatal(err)
			}
			err = f.requests.CompleteExecution(ctx, f.org, lease, scenario.completion, reqEvent(audit.ActionExecutionFinished, request.ID))
			if scenario.wantState == access.StateExecuting {
				if !errors.Is(err, access.ErrInvalidRequest) {
					t.Fatalf("invalid completion = %v, want ErrInvalidRequest", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			view, _, err := f.requests.Get(ctx, f.org, request.ID)
			if err != nil {
				t.Fatal(err)
			}
			if view.Request.State != scenario.wantState {
				t.Fatalf("state = %s, want %s", view.Request.State, scenario.wantState)
			}
			var finished int
			var reason *string
			if err := f.pool.QueryRow(ctx, `select count(*), max(metadata->>'result_unavailable_reason') from public.audit_events where target_id=$1 and action='EXECUTION_FINISHED'`, string(request.ID)).Scan(&finished, &reason); err != nil {
				t.Fatal(err)
			}
			recorded := ""
			if reason != nil {
				recorded = *reason
			}
			if recorded != scenario.wantReason || (finished == 1) != (scenario.wantState != access.StateExecuting) {
				t.Fatalf("finished events = %d with reason %q, want reason %q", finished, recorded, scenario.wantReason)
			}
		})
	}
}
