package accessrequest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/accessrequest"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

type engineTargetRepository struct {
	*fakeRepo
	engine string
}

func (r engineTargetRepository) CurrentTarget(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (access.SubmitTarget, error) {
	target, err := r.fakeRepo.CurrentTarget(ctx, org, id)
	target.DBType = r.engine
	return target, err
}

type observedSubmissionDialect struct {
	fakeDialect
	binds int
}

func (d *observedSubmissionDialect) BindNamed(sql string, params []query.Parameter) (string, []query.TypedValue, error) {
	d.binds++
	return d.fakeDialect.BindNamed(sql, params)
}

func TestUnregisteredEngineCannotSubmitThroughPostgreSQLDialect(t *testing.T) {
	t.Parallel()
	for _, engine := range []string{"mysql", "sqlite", "unknown", ""} {
		t.Run(engine, func(t *testing.T) {
			repo := newRepo()
			dialect := &observedSubmissionDialect{}
			svc, err := accessrequest.New(engineTargetRepository{repo, engine}, repo, fakeCodec{digestKV: 1}, dialect, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			draft := createDraft(t, svc, connRead, "select 1")
			_, err = svc.Submit(context.Background(), requester, draft.ID, draft.Version)
			if !errors.Is(err, connection.ErrUnsupportedDBType) {
				t.Fatalf("unregistered engine submitted: %v", err)
			}
			if dialect.binds != 0 {
				t.Fatalf("foreign engine reached PostgreSQL binder: %d", dialect.binds)
			}
			if repo.requests[draft.ID].State != access.StateDraft {
				t.Fatal("refusal changed draft state")
			}
		})
	}
}
