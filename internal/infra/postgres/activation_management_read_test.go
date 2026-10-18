package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestBindingActivationManagementOutputFencesPolicyRevisionAndExpiry(t *testing.T) {
	for _, scenario := range []string{"user detail revision", "role detail revision", "binding detail revision", "catalog revision", "expired condition"} {
		t.Run(scenario, func(t *testing.T) {
			environment := newAdministrationEnv(t)
			ctx := authz.WithOutputFence(context.Background())
			if err := pg.Migrate(ctx, environment.pool); err != nil {
				t.Fatal(err)
			}
			var binding string
			if err := environment.pool.QueryRow(ctx, `select id::text from public.role_bindings where user_id=$1`, environment.admin.ID).Scan(&binding); err != nil {
				t.Fatal(err)
			}
			query := administration.ManagementQuery{Kind: identity.ResourceUser, ID: string(environment.admin.ID)}
			switch scenario {
			case "role detail revision":
				query = administration.ManagementQuery{Kind: identity.ResourceRole, ID: string(environment.roles["admin"])}
			case "binding detail revision":
				query = administration.ManagementQuery{Kind: identity.ResourceBinding, ID: binding}
			case "catalog revision":
				query = administration.ManagementQuery{Kind: identity.ResourceRole, Catalog: true}
			case "expired condition":
				var expires time.Time
				if err := environment.pool.QueryRow(ctx, `select clock_timestamp()+interval '1 second'`).Scan(&expires); err != nil {
					t.Fatal(err)
				}
				expression := `request.time < timestamp("` + expires.UTC().Format(time.RFC3339Nano) + `")`
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set condition_title='Read window',condition_expression=$2,condition_language_version='cel-v1',condition_environment_version='portcullis-v1' where binding_id=$1`, binding, expression); err != nil {
					t.Fatal(err)
				}
			}
			repository, err := pg.NewAuthorizedManagementReadStore(environment.pool, activePolicyFixture(t, environment.pool), environment.org)
			if err != nil {
				t.Fatal(err)
			}
			reader := managementReaderScenario(t, repository, 64)
			result, err := reader.Read(ctx, environment.org, environment.admin.ID, query)
			if err != nil || result.Revision <= 0 {
				t.Fatalf("initial authorized read: %+v %v", result, err)
			}
			if scenario == "expired condition" {
				if _, err := environment.pool.Exec(ctx, `select pg_catalog.pg_sleep(1.1)`); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := environment.pool.Exec(ctx, `update public.organization_authorization_revisions set revision=revision+1 where organization_id=$1`, environment.org); err != nil {
					t.Fatal(err)
				}
			}
			if err := authz.CheckOutput(ctx); err == nil {
				t.Fatal("stale management data escaped output fence")
			}
		})
	}
}
