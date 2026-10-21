package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestAuthorizedHistoryKeepsOneSnapshotAcrossBatches(t *testing.T) {
	for _, kind := range []string{"audit", "request"} {
		t.Run(kind, func(t *testing.T) {
			environment := newAdministrationEnv(t)
			fixture := reqPreparationFixtureOver(t, connFixture{pool: environment.pool, store: pg.NewConnectionStore(environment.pool), org: environment.org, user: environment.admin.ID})
			connectionID := fixture.liveConn(t, 1)
			request := fixture.draft(t, connectionID)
			changedRequest := request
			if kind == "request" {
				changedRequest = fixture.draft(t, connectionID)
			}
			ctx := context.Background()
			if err := pg.Migrate(ctx, environment.pool); err != nil {
				t.Fatal(err)
			}
			settings := authz.DefaultReadConfig()
			settings.MaxCandidates = 1
			batches := 0
			changed := false
			pool := fixture.tracedPool(t, func(statement string) {
				listQuery := "-- name: ListAuditEventsAsc"
				if kind == "request" {
					listQuery = "-- name: ListAccessRequestsAsc"
				}
				if !strings.Contains(statement, listQuery) {
					return
				}
				batches++
				if batches != 2 {
					return
				}
				var err error
				if kind == "audit" {
					_, err = environment.pool.Exec(ctx, `insert into public.audit_events(organization_id,occurred_at,actor_type,actor_service,action,target_type,outcome) values($1,'2000-01-01','service','concurrent','AUTH_LOGIN','user','succeeded')`, environment.org)
				} else {
					_, err = environment.pool.Exec(ctx, `update public.access_requests set state='cancelled' where organization_id=$1 and id=$2`, environment.org, changedRequest.ID)
				}
				if err != nil {
					t.Errorf("concurrent history change: %v", err)
				} else {
					changed = true
				}
			})
			policy := historyPolicyFixtureWithReads(t, environment.pool, settings)
			if kind == "audit" {
				var count int64
				if err := environment.pool.QueryRow(ctx, `select count(*) from public.audit_events where organization_id=$1`, environment.org).Scan(&count); err != nil {
					t.Fatal(err)
				}
				scoped := authz.WithOutputFence(authz.WithOperation(ctx, authz.Operation{Principal: environment.admin.ID, Permission: identity.PermissionAuditList, Type: identity.ResourceAudit, Collection: true}))
				store, err := pg.NewAuthorizedAuditStore(pool, policy, environment.org)
				if err != nil {
					t.Fatal(err)
				}
				result, err := store.List(scoped, environment.org, audit.ListParams{Page: 1, PageSize: 25})
				if err != nil || result.TotalCount != count || len(result.Events) != int(count) {
					t.Fatalf("mixed audit snapshots: total=%d rows=%d want=%d error=%v", result.TotalCount, len(result.Events), count, err)
				}
				if err := authz.CheckOutput(scoped); err != nil {
					t.Fatalf("a concurrent append must not invalidate an authorized prior snapshot: %v", err)
				}
			} else {
				scoped := authz.WithOutputFence(authz.WithOperation(ctx, authz.Operation{Principal: fixture.requester, Permission: identity.PermissionRequestsList, Type: identity.ResourceRequest, Collection: true}))
				store, err := pg.NewAuthorizedAccessRequestStore(pool, policy, environment.org)
				if err != nil {
					t.Fatal(err)
				}
				result, err := store.List(scoped, environment.org, access.ListQuery{Page: 1, PageSize: 25, State: access.StateDraft})
				if err != nil || result.TotalCount != 2 || len(result.Items) != 2 {
					t.Fatalf("mixed request snapshots: %+v error=%v", result, err)
				}
				for _, item := range result.Items {
					if (item.Request.ID != request.ID && item.Request.ID != changedRequest.ID) || item.EffectiveState != access.StateDraft {
						t.Fatalf("filter and row snapshots disagree: %+v", item)
					}
				}
				if err := authz.CheckOutput(scoped); err != nil {
					t.Fatal(err)
				}
			}
			if !changed {
				t.Fatal("scenario did not reach a concurrent change between history batches")
			}
		})
	}
}

func TestAuthorizedHistoryCountConditionExpiresBeforeOutput(t *testing.T) {
	for _, kind := range []string{"audit", "request"} {
		t.Run(kind, func(t *testing.T) {
			environment := newAdministrationEnv(t)
			fixture := reqPreparationFixtureOver(t, connFixture{pool: environment.pool, store: pg.NewConnectionStore(environment.pool), org: environment.org, user: environment.admin.ID})
			fixture.draft(t, fixture.liveConn(t, 1))
			ctx := context.Background()
			if err := pg.Migrate(ctx, environment.pool); err != nil {
				t.Fatal(err)
			}
			var expires time.Time
			if err := environment.pool.QueryRow(ctx, `select clock_timestamp()+interval '2 seconds'`).Scan(&expires); err != nil {
				t.Fatal(err)
			}
			setHistoryCondition(t, environment, environment.admin.ID, `request.time < timestamp("`+expires.UTC().Format(time.RFC3339Nano)+`")`)
			operation := authz.Operation{Principal: environment.admin.ID, Permission: identity.PermissionAuditList, Type: identity.ResourceAudit, Collection: true}
			if kind == "request" {
				operation.Permission = identity.PermissionRequestsList
				operation.Type = identity.ResourceRequest
			}
			scoped := authz.WithOutputFence(authz.WithOperation(ctx, operation))
			policy := activePolicyFixture(t, environment.pool)
			if kind == "audit" {
				store, err := pg.NewAuthorizedAuditStore(environment.pool, policy, environment.org)
				if err != nil {
					t.Fatal(err)
				}
				result, err := store.List(scoped, environment.org, audit.ListParams{Page: 1, PageSize: 1})
				if err != nil || result.TotalCount == 0 {
					t.Fatalf("unexpired audit condition: %+v %v", result, err)
				}
			} else {
				store, err := pg.NewAuthorizedAccessRequestStore(environment.pool, policy, environment.org)
				if err != nil {
					t.Fatal(err)
				}
				result, err := store.List(scoped, environment.org, access.ListQuery{Page: 1, PageSize: 1})
				if err != nil || result.TotalCount == 0 {
					t.Fatalf("unexpired request condition: %+v %v", result, err)
				}
			}
			if _, err := environment.pool.Exec(ctx, `select pg_catalog.pg_sleep(greatest(0,extract(epoch from ($1::timestamptz-clock_timestamp())))+0.01)`, expires); err != nil {
				t.Fatal(err)
			}
			if err := authz.CheckOutput(scoped); !errors.Is(err, authz.ErrPermissionDenied) {
				t.Fatalf("expired count condition released: %v", err)
			}
		})
	}
}

func TestAuthorizedHistoryCancellationDiscardsCompletedBatches(t *testing.T) {
	for _, kind := range []string{"audit", "request"} {
		t.Run(kind, func(t *testing.T) {
			environment := newAdministrationEnv(t)
			fixture := reqPreparationFixtureOver(t, connFixture{pool: environment.pool, store: pg.NewConnectionStore(environment.pool), org: environment.org, user: environment.admin.ID})
			fixture.draft(t, fixture.liveConn(t, 1))
			if err := pg.Migrate(context.Background(), environment.pool); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			batches := 0
			pool := fixture.tracedPool(t, func(statement string) {
				listQuery := "-- name: ListAuditEventsAsc"
				if kind == "request" {
					listQuery = "-- name: ListAccessRequestsAsc"
				}
				if strings.Contains(statement, listQuery) {
					batches++
					if batches == 2 {
						cancel()
					}
				}
			})
			settings := authz.DefaultReadConfig()
			settings.MaxCandidates = 1
			policy := historyPolicyFixtureWithReads(t, environment.pool, settings)
			var readError error
			if kind == "audit" {
				store, err := pg.NewAuthorizedAuditStore(pool, policy, environment.org)
				if err != nil {
					t.Fatal(err)
				}
				scoped := authz.WithOperation(ctx, authz.Operation{Principal: environment.admin.ID, Permission: identity.PermissionAuditList, Type: identity.ResourceAudit, Collection: true})
				result, err := store.List(scoped, environment.org, audit.ListParams{Page: 1, PageSize: 1})
				readError = err
				if result.TotalCount != 0 || len(result.Events) != 0 {
					t.Fatalf("cancelled audit disclosed prefix: %+v", result)
				}
			} else {
				store, err := pg.NewAuthorizedAccessRequestStore(pool, policy, environment.org)
				if err != nil {
					t.Fatal(err)
				}
				scoped := authz.WithOperation(ctx, authz.Operation{Principal: fixture.requester, Permission: identity.PermissionRequestsList, Type: identity.ResourceRequest, Collection: true})
				result, err := store.List(scoped, environment.org, access.ListQuery{Page: 1, PageSize: 1})
				readError = err
				if result.TotalCount != 0 || len(result.Items) != 0 {
					t.Fatalf("cancelled requests disclosed prefix: %+v", result)
				}
			}
			if batches < 2 || !errors.Is(readError, context.Canceled) || !errors.Is(readError, authz.ErrSnapshotUnavailable) {
				t.Fatalf("missing bounded cancellation after partial scan: batches=%d error=%v", batches, readError)
			}
		})
	}
}
