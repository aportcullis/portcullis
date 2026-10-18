package postgres_test

import (
	"context"
	"slices"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestBindingActivationSessionCapabilitiesDoNotFlattenScopedOrConditionalAuthority(t *testing.T) {
	for _, scenario := range []string{"organization", "connection", "true condition", "grantless", "false condition", "disabled", "no membership", "irrelevant audit history"} {
		t.Run(scenario, func(t *testing.T) {
			environment := newAdministrationEnv(t)
			fixture := reqPreparationFixtureOver(t, connFixture{pool: environment.pool, store: pg.NewConnectionStore(environment.pool), org: environment.org, user: environment.admin.ID})
			principal := fixture.userWithRole(t, "requester")
			connectionID := fixture.liveConn(t, 1)
			ctx := context.Background()
			if err := pg.Migrate(ctx, environment.pool); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "connection":
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set scope_type='connection',connection_id=$2 where user_id=$1`, principal, connectionID); err != nil {
					t.Fatal(err)
				}
			case "true condition", "false condition":
				expression := "true"
				if scenario == "false condition" {
					expression = "false"
				}
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set condition_title='Session window',condition_expression=$2,condition_language_version='cel-v1',condition_environment_version='portcullis-v1' where user_id=$1`, principal, expression); err != nil {
					t.Fatal(err)
				}
			case "grantless":
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set revoked_at=clock_timestamp() where user_id=$1`, principal); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				if _, err := environment.pool.Exec(ctx, `update public.users set status='disabled' where id=$1`, principal); err != nil {
					t.Fatal(err)
				}
			case "no membership":
				user, err := pg.NewIdentityStore(environment.pool).CreateUser(ctx, "outside-capability@example.com", "Outside")
				if err != nil {
					t.Fatal(err)
				}
				principal = user.ID
			case "irrelevant audit history":
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set scope_type='connection',connection_id=$2 where user_id=$1`, principal, connectionID); err != nil {
					t.Fatal(err)
				}
				if _, err := environment.pool.Exec(ctx, `insert into public.audit_events(organization_id,actor_type,actor_service,action,target_type,outcome) select $1,'system','system:fixture','CAPABILITY_FIXTURE','organization','SUCCEEDED' from generate_series(1,1100)`, environment.org); err != nil {
					t.Fatal(err)
				}
			}
			store, err := pg.NewBindingPolicyStore(environment.pool, activePolicyFixture(t, environment.pool), environment.org)
			if err != nil {
				t.Fatal(err)
			}
			view, err := store.SessionCapabilities(ctx, principal)
			if scenario == "disabled" || scenario == "no membership" {
				if err == nil {
					t.Fatal("ineligible session capabilities disclosed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if view.Revision <= 0 {
				t.Fatal("missing authorization revision")
			}
			if scenario == "grantless" || scenario == "false condition" {
				if len(view.Capabilities) != 0 {
					t.Fatalf("unearned capabilities: %v", view.Capabilities)
				}
				return
			}
			if !slices.Contains(view.Capabilities, identity.PermissionRequestsCreate) {
				t.Fatalf("applicable target capability missing: %v", view.Capabilities)
			}
			if scenario != "organization" && len(view.Permissions) != 0 {
				t.Fatalf("scoped/conditional grant flattened into compatibility field: %v", view.Permissions)
			}
		})
	}
}
