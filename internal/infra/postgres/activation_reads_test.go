package postgres_test

import (
	"context"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestBindingActivationFiltersConnectionsAndRechecksOutput(t *testing.T) {
	for _, scenario := range []string{"organization list", "connection list", "true condition", "additive scoped lists", "wrong connection detail", "false condition list", "revoked before output", "missing server purpose"} {
		t.Run(scenario, func(t *testing.T) {
			environment := newAdministrationEnv(t)
			fixture := reqPreparationFixtureOver(t, connFixture{pool: environment.pool, store: pg.NewConnectionStore(environment.pool), org: environment.org, user: environment.admin.ID})
			principal := fixture.userWithRole(t, "admin")
			first, second := fixture.liveConn(t, 1), fixture.liveConn(t, 1)
			ctx := context.Background()
			if err := pg.Migrate(ctx, environment.pool); err != nil {
				t.Fatal(err)
			}
			if scenario != "organization list" && scenario != "missing server purpose" {
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set scope_type='connection',connection_id=$2 where user_id=$1`, principal, first); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "true condition" || scenario == "false condition list" {
				expression := "true"
				if scenario == "false condition list" {
					expression = "false"
				}
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set condition_title='Read scenario',condition_expression=$2,condition_language_version='cel-v1',condition_environment_version='portcullis-v1' where user_id=$1`, principal, expression); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "additive scoped lists" {
				if _, err := environment.pool.Exec(ctx, `with head as(insert into public.role_bindings(organization_id,user_id,current_version,created_at) values($1,$2,1,clock_timestamp()) returning id) insert into public.role_binding_versions(binding_id,organization_id,user_id,version,role_id,scope_type,connection_id,created_at) select head.id,$1,$2,1,role.id,'connection',$3,clock_timestamp() from head,public.roles role where role.organization_id=$1 and role.name='admin'`, environment.org, principal, second); err != nil {
					t.Fatal(err)
				}
			}
			store, err := pg.NewAuthorizedConnectionStore(environment.pool, activePolicyFixture(t, environment.pool), environment.org)
			if err != nil {
				t.Fatal(err)
			}
			operation := authz.Operation{Principal: principal, Permission: identity.PermissionConnectionsList, Type: identity.ResourceConnection, Collection: true}
			if scenario != "missing server purpose" {
				ctx = authz.WithOperation(ctx, operation)
			}
			ctx = authz.WithOutputFence(ctx)
			if scenario == "wrong connection detail" {
				ctx = authz.WithOperation(ctx, authz.Operation{Principal: principal, Permission: identity.PermissionConnectionsGet, Type: identity.ResourceConnection, ID: string(second)})
				if _, err := store.GetByID(ctx, environment.org, second); err == nil {
					t.Fatal("out-of-scope detail disclosed")
				}
				return
			}
			rows, err := store.List(ctx, environment.org, false)
			if scenario == "missing server purpose" {
				if err == nil {
					t.Fatal("read without server purpose accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			expected := 1
			if scenario == "organization list" || scenario == "additive scoped lists" {
				expected = 2
			}
			if scenario == "false condition list" {
				expected = 0
			}
			if len(rows) != expected {
				t.Fatalf("visible rows=%d want %d", len(rows), expected)
			}
			if scenario == "revoked before output" {
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set revoked_at=clock_timestamp() where user_id=$1`, principal); err != nil {
					t.Fatal(err)
				}
				if err := authz.CheckOutput(ctx); err == nil {
					t.Fatal("revoked response released")
				}
			} else if err := authz.CheckOutput(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
