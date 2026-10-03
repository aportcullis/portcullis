package accessrequest_test

import (
	"context"
	"errors"
	"github.com/aportcullis/portcullis/internal/infra/dialectregistry"
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
			registry, err := dialectregistry.New(dialectregistry.Registration{Engine: connection.DBTypePostgreSQL, Adapter: &registeredSubmissionAdapter{dialect}})
			if err != nil {
				t.Fatal(err)
			}
			svc, err := accessrequest.NewWithDialects(engineTargetRepository{repo, engine}, repo, fakeCodec{digestKV: 1}, registry, time.Hour)
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

type registeredSubmissionAdapter struct{ *observedSubmissionDialect }

func (*registeredSubmissionAdapter) Execute(context.Context, connection.Target, connection.TLSMode, connection.Credential, query.Execution) (query.ResultStream, error) {
	return nil, errors.New("submission cannot execute")
}

func TestRegisteredEngineSubmissionPinsTheSelectedEngine(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	selected := &registeredSubmissionAdapter{&observedSubmissionDialect{}}
	unused := &registeredSubmissionAdapter{&observedSubmissionDialect{}}
	registry, err := dialectregistry.New(dialectregistry.Registration{Engine: "mysql", Adapter: selected}, dialectregistry.Registration{Engine: "postgresql", Adapter: unused})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := accessrequest.NewWithDialects(engineTargetRepository{repo, "mysql"}, repo, fakeCodec{digestKV: 1}, registry, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	draft := createDraft(t, svc, connRead, "select 1")
	result, err := svc.Submit(context.Background(), requester, draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.ConnectionDBType != "mysql" || result.Request.State != access.StatePending {
		t.Fatalf("wrong approval snapshot: %+v", result.Request)
	}
	if selected.binds != 1 || unused.binds != 0 {
		t.Fatalf("wrong engine binder: selected=%d unused=%d", selected.binds, unused.binds)
	}
}
