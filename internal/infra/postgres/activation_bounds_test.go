package postgres_test

import (
	"context"
	"errors"
	"hash/fnv"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

type deadlineOnlyConditions struct{}

// Prepare simulates an unavailable preparer that honors its caller's deadline.
func (deadlineOnlyConditions) Prepare(ctx context.Context, _ identity.Condition) (condition.CheckedCondition, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// Evaluate cannot grant access after failed preparation.
func (deadlineOnlyConditions) Evaluate(context.Context, condition.CheckedCondition, condition.AttributeSnapshot) condition.Result {
	return condition.Result{Outcome: condition.NoMatch}
}

func TestBindingActivationReadDeadlineIncludesConditionWork(t *testing.T) {
	environment := newAdministrationEnv(t)
	fixture := reqPreparationFixtureOver(t, connFixture{pool: environment.pool, store: pg.NewConnectionStore(environment.pool), org: environment.org, user: environment.admin.ID})
	fixture.liveConn(t, 1)
	ctx := context.Background()
	if err := pg.Migrate(ctx, environment.pool); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set condition_title='Bounded preparation',condition_expression='true',condition_language_version='cel-v1',condition_environment_version='portcullis-v1' where user_id=$1`, environment.admin.ID); err != nil {
		t.Fatal(err)
	}
	catalog, err := environment.store.ListPermissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings := authz.DefaultResourceConfig()
	settings.Timeout = 2 * time.Second
	reads := authz.DefaultReadConfig()
	reads.Timeout = 50 * time.Millisecond
	policy, err := pg.NewBindingPolicyEvaluator(deadlineOnlyConditions{}, catalog, settings, reads)
	if err != nil {
		t.Fatal(err)
	}
	store, err := pg.NewAuthorizedConnectionStore(environment.pool, policy, environment.org)
	if err != nil {
		t.Fatal(err)
	}
	ctx = authz.WithOperation(ctx, authz.Operation{Principal: environment.admin.ID, Permission: identity.PermissionConnectionsList, Type: identity.ResourceConnection, Collection: true})
	started := time.Now()
	rows, err := store.List(ctx, environment.org, false)
	if err == nil || len(rows) != 0 || time.Since(started) > time.Second {
		t.Fatalf("condition work escaped read budget: rows=%d error=%v elapsed=%s", len(rows), err, time.Since(started))
	}
}

func TestBindingActivationReadBudgetsRefuseIncompleteResponses(t *testing.T) {
	for _, scenario := range []string{"complete scan", "overflow witness", "policy revision changed", "condition expired before output", "cancelled caller", "policy lock deadline"} {
		t.Run(scenario, func(t *testing.T) {
			environment := newAdministrationEnv(t)
			fixture := reqPreparationFixtureOver(t, connFixture{pool: environment.pool, store: pg.NewConnectionStore(environment.pool), org: environment.org, user: environment.admin.ID})
			fixture.liveConn(t, 1)
			fixture.liveConn(t, 1)
			ctx := context.Background()
			if err := pg.Migrate(ctx, environment.pool); err != nil {
				t.Fatal(err)
			}
			settings := authz.DefaultReadConfig()
			if scenario == "overflow witness" {
				settings.MaxCandidates = 1
			}
			if scenario == "policy lock deadline" {
				settings.Timeout = 100 * time.Millisecond
			}
			policy := activePolicyFixtureWithReads(t, environment.pool, settings)
			store, err := pg.NewAuthorizedConnectionStore(environment.pool, policy, environment.org)
			if err != nil {
				t.Fatal(err)
			}
			ctx = authz.WithOutputFence(authz.WithOperation(ctx, authz.Operation{Principal: environment.admin.ID, Permission: identity.PermissionConnectionsList, Type: identity.ResourceConnection, Collection: true}))
			if scenario == "cancelled caller" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			if scenario == "policy lock deadline" {
				blocker, err := environment.pool.Begin(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = blocker.Rollback(context.Background()) }()
				hash := fnv.New32a()
				_, _ = hash.Write([]byte(environment.org))
				object := int64(hash.Sum32())
				if object > 2147483647 {
					object -= 4294967296
				}
				if _, err := blocker.Exec(context.Background(), `select pg_catalog.pg_advisory_xact_lock(5,$1::integer)`, object); err != nil {
					t.Fatal(err)
				}
				started := time.Now()
				rows, err := store.List(ctx, environment.org, false)
				if err == nil || len(rows) != 0 || time.Since(started) > time.Second {
					t.Fatalf("unbounded lock wait or response: rows=%d error=%v elapsed=%s", len(rows), err, time.Since(started))
				}
				return
			}
			rows, err := store.List(ctx, environment.org, false)
			if scenario == "overflow witness" {
				if !errors.Is(err, authz.ErrBindingLimitExceeded) || len(rows) != 0 {
					t.Fatalf("partial overflow: %d %v", len(rows), err)
				}
				return
			}
			if scenario == "cancelled caller" {
				if err == nil || len(rows) != 0 {
					t.Fatal("cancelled scan returned data")
				}
				return
			}
			if err != nil || len(rows) != 2 {
				t.Fatalf("complete scan: %d %v", len(rows), err)
			}
			if scenario == "policy revision changed" {
				if _, err := environment.pool.Exec(ctx, `update public.organization_authorization_revisions set revision=revision+1 where organization_id=$1`, environment.org); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "condition expired before output" {
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set condition_title='Expired',condition_expression='false',condition_language_version='cel-v1',condition_environment_version='portcullis-v1' where user_id=$1`, environment.admin.ID); err != nil {
					t.Fatal(err)
				}
			}
			err = authz.CheckOutput(ctx)
			if scenario == "complete scan" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("stale response escaped output fence")
			}
		})
	}
}

func TestBindingActivationRuntimePrivilegesPreserveImmutablePolicyHistory(t *testing.T) {
	for _, scenario := range []string{"correct active matrix", "missing binding insert", "mutable version history", "runtime revision creation"} {
		t.Run(scenario, func(t *testing.T) {
			pool := dbtest.FreshPostgres(t)
			ctx := context.Background()
			if err := pg.Migrate(ctx, pool); err != nil {
				t.Fatal(err)
			}
			statements := map[string]string{"missing binding insert": `revoke insert on public.role_bindings from portcullis_runtime`, "mutable version history": `grant update on public.role_binding_versions to portcullis_runtime`, "runtime revision creation": `grant insert on public.organization_authorization_revisions to portcullis_runtime`}
			if statement := statements[scenario]; statement != "" {
				if _, err := pool.Exec(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			err := pg.Migrate(ctx, pool)
			if scenario == "correct active matrix" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unsafe active privilege matrix accepted")
			}
		})
	}
}
