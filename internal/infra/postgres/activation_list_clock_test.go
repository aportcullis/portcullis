package postgres_test

import (
	"context"
	"hash/fnv"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBindingActivationRequestStateFilterUsesPostLockInstant(t *testing.T) {
	for _, state := range []access.State{access.StateApproved, access.StateExpired} {
		t.Run(string(state), func(t *testing.T) {
			environment := newAdministrationEnv(t)
			fixture := reqPreparationFixtureOver(t, connFixture{pool: environment.pool, store: pg.NewConnectionStore(environment.pool), org: environment.org, user: environment.admin.ID})
			approver := fixture.userWithRole(t, "approver")
			request := fixture.submitted(t, fixture.liveConn(t, 1), 1)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := pg.Migrate(ctx, environment.pool); err != nil {
				t.Fatal(err)
			}
			store, err := pg.NewAuthorizedAccessRequestStore(environment.pool, activePolicyFixture(t, environment.pool), environment.org)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Approve(ctx, fixture.org, request.ID, approver, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, request.ID)); err != nil {
				t.Fatal(err)
			}
			if _, err := environment.pool.Exec(ctx, `update public.access_requests set expires_at=clock_timestamp()+interval '1 second' where id=$1`, request.ID); err != nil {
				t.Fatal(err)
			}
			blocker, err := environment.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = blocker.Rollback(context.Background()) }()
			coordinator := fnv.New32a()
			_, _ = coordinator.Write([]byte(environment.org))
			object := int64(coordinator.Sum32())
			if object > 2147483647 {
				object -= 4294967296
			}
			if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock(5,$1::integer)`, object); err != nil {
				t.Fatal(err)
			}
			settings := environment.pool.Config().Copy()
			settings.ConnConfig.RuntimeParams["application_name"] = "activation-state-filter"
			waiting, err := pgxpool.NewWithConfig(ctx, settings)
			if err != nil {
				t.Fatal(err)
			}
			defer waiting.Close()
			reader, err := pg.NewAuthorizedAccessRequestStore(waiting, activePolicyFixture(t, environment.pool), environment.org)
			if err != nil {
				t.Fatal(err)
			}
			scoped := authz.WithOperation(ctx, authz.Operation{Principal: fixture.requester, Permission: identity.PermissionRequestsList, Type: identity.ResourceRequest, Collection: true})
			type outcome struct {
				page access.RequestPage
				err  error
			}
			results := make(chan outcome, 1)
			go func() {
				page, err := reader.List(scoped, fixture.org, access.ListQuery{Page: 1, PageSize: 10, State: state})
				results <- outcome{page: page, err: err}
			}()
			waitBindingPolicyLock(t, ctx, environment.pool, "activation-state-filter")
			if _, err := blocker.Exec(ctx, `select pg_catalog.pg_sleep(1.1)`); err != nil {
				t.Fatal(err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			result := <-results
			if result.err != nil {
				t.Fatal(result.err)
			}
			wanted := int64(0)
			if state == access.StateExpired {
				wanted = 1
			}
			if result.page.TotalCount != wanted || int64(len(result.page.Items)) != wanted {
				t.Fatalf("filter=%s total=%d items=%d want %d", state, result.page.TotalCount, len(result.page.Items), wanted)
			}
			for _, item := range result.page.Items {
				if item.EffectiveState != state {
					t.Fatalf("state filter and visible badge disagree: %s %s", state, item.EffectiveState)
				}
			}
		})
	}
}
