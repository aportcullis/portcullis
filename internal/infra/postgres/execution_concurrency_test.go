package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/google/uuid"
)

func TestConcurrentExecutionReportsPreserveOneTerminalOutcome(t *testing.T) {
	f := newReqFixtureFresh(t)
	r := approvedExecutionRequest(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	lease, err := f.requests.AcquireExecution(ctx, f.org, r.ID, f.requester, "owner", uuid.NewString(), r.Digest, reqEvent(audit.ActionExecutionStarted, r.ID))
	if err != nil {
		t.Fatal(err)
	}
	type report struct {
		state access.State
		err   error
	}
	const reporters = 8
	start := make(chan struct{})
	reports := make(chan report, reporters)
	var workers sync.WaitGroup
	for reporter := range reporters {
		state := access.StateSucceeded
		if reporter%2 != 0 {
			state = access.StateFailed
		}
		store := pg.NewAccessRequestStore(f.pool)
		workers.Go(func() {
			<-start
			err := store.CompleteExecution(ctx, f.org, lease, access.ExecutionCompletion{State: state}, reqEvent(audit.ActionExecutionFinished, r.ID))
			reports <- report{state: state, err: err}
		})
	}
	close(start)
	workers.Wait()
	close(reports)
	var winner access.State
	winners, fenced := 0, 0
	for report := range reports {
		switch {
		case report.err == nil:
			winners++
			winner = report.state
		case errors.Is(report.err, access.ErrLeaseLost):
			fenced++
		default:
			t.Fatalf("concurrent report failed: %v", report.err)
		}
	}
	if winners != 1 || fenced != reporters-1 {
		t.Fatalf("completion reports: %d winners, %d fenced", winners, fenced)
	}
	view, _, err := f.requests.Get(ctx, f.org, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	completion, err := f.requests.GetExecution(ctx, f.org, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Request.State != winner || completion.State != winner {
		t.Fatalf("terminal outcome changed: request=%s execution=%s winner=%s", view.Request.State, completion.State, winner)
	}
	if finished := f.countEvents(t, audit.ActionExecutionFinished, r.ID); finished != 1 {
		t.Fatalf("execution finished audit count: %d", finished)
	}
	if late := f.countEvents(t, audit.ActionLateCompletionObserved, r.ID); late != reporters-1 {
		t.Fatalf("fenced reports lost audit evidence: %d", late)
	}
}
