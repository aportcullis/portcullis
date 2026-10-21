package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/infra/celcondition"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// activePolicyFixture injects the restricted CEL implementation into workflow qualification.
func activePolicyFixture(t *testing.T, pool *pgxpool.Pool) *pg.BindingPolicyEvaluator {
	return activePolicyFixtureWithReads(t, pool, authz.DefaultReadConfig())
}

// activePolicyFixtureWithReads qualifies finite scan budgets using the real condition adapter.
func activePolicyFixtureWithReads(t *testing.T, pool *pgxpool.Pool, readSettings authz.ReadConfig) *pg.BindingPolicyEvaluator {
	t.Helper()
	return activePolicyFixtureWithLimits(t, pool, condition.DefaultConfig(), authz.DefaultResourceConfig(), readSettings)
}

// activePolicyFixtureWithLimits keeps functional fixture budgets explicit without changing runtime defaults.
func activePolicyFixtureWithLimits(
	t *testing.T,
	pool *pgxpool.Pool,
	conditionSettings condition.Config,
	resourceSettings authz.ResourceConfig,
	readSettings authz.ReadConfig,
) *pg.BindingPolicyEvaluator {
	t.Helper()
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	conditions, err := condition.New(adapter, adapter, conditionSettings)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := pg.NewIdentityStore(pool).ListPermissions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := pg.NewBindingPolicyEvaluator(conditions, catalog, resourceSettings, readSettings)
	if err != nil {
		t.Fatal(err)
	}
	return evaluator
}

func TestBindingActivationGovernanceUsesCurrentGrants(t *testing.T) {
	for _, scenario := range []string{"preserved organization grant", "matching connection grant", "true condition", "additive grant", "wrong connection", "false condition", "revoked grant", "disabled approver"} {
		t.Run(scenario, func(t *testing.T) {
			environment := newAdministrationEnv(t)
			fixture := reqPreparationFixtureOver(t, connFixture{pool: environment.pool, store: pg.NewConnectionStore(environment.pool), org: environment.org, user: environment.admin.ID})
			approver := fixture.userWithRole(t, "approver")
			connectionID := fixture.liveConn(t, 1)
			request := fixture.submitted(t, connectionID, 1)
			otherConnection := fixture.liveConn(t, 1)
			ctx := context.Background()
			if err := pg.Migrate(ctx, environment.pool); err != nil {
				t.Fatal(err)
			}
			success := true
			switch scenario {
			case "matching connection grant", "wrong connection":
				target := connectionID
				if scenario == "wrong connection" {
					target = otherConnection
					success = false
				}
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set scope_type='connection',connection_id=$2 where binding_id in(select id from public.role_bindings where user_id=$1)`, approver, target); err != nil {
					t.Fatal(err)
				}
			case "true condition", "false condition", "additive grant":
				expression := "true"
				if scenario != "true condition" {
					expression = "false"
				}
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set condition_title='Scenario',condition_expression=$2,condition_language_version='cel-v1',condition_environment_version='portcullis-v1' where binding_id in(select id from public.role_bindings where user_id=$1)`, approver, expression); err != nil {
					t.Fatal(err)
				}
				if scenario == "false condition" {
					success = false
				}
				if scenario == "additive grant" {
					if _, err := environment.pool.Exec(ctx, `with head as(insert into public.role_bindings(organization_id,user_id,current_version,created_at) values($1,$2,1,clock_timestamp()) returning id) insert into public.role_binding_versions(binding_id,organization_id,user_id,version,role_id,scope_type,created_at) select head.id,$1,$2,1,role.id,'organization',clock_timestamp() from head,public.roles role where role.organization_id=$1 and role.name='approver'`, environment.org, approver); err != nil {
						t.Fatal(err)
					}
				}
			case "revoked grant":
				success = false
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set revoked_at=clock_timestamp() where binding_id in(select id from public.role_bindings where user_id=$1)`, approver); err != nil {
					t.Fatal(err)
				}
			case "disabled approver":
				success = false
				if _, err := environment.pool.Exec(ctx, `update public.users set status='disabled' where id=$1`, approver); err != nil {
					t.Fatal(err)
				}
			}
			store, err := pg.NewAuthorizedAccessRequestStore(environment.pool, activePolicyFixture(t, environment.pool), environment.org)
			if err != nil {
				t.Fatal(err)
			}
			view, err := store.Approve(ctx, fixture.org, request.ID, approver, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, request.ID))
			if success {
				if err != nil || view.Request.State != access.StateApproved || view.ValidApprovals != 1 {
					t.Fatalf("approval state=%s valid=%d error=%v", view.Request.State, view.ValidApprovals, err)
				}
				var evidence bool
				if err := environment.pool.QueryRow(ctx, `select coalesce((metadata #>> '{authorization,revision}')::bigint>0 and metadata #>> '{authorization,binding_id}' is not null and metadata #>> '{authorization,permission}'='requests.approve',false) from public.audit_events where target_id=$1 and action='ACCESS_REQUEST_APPROVED'`, request.ID).Scan(&evidence); err != nil || !evidence {
					t.Fatalf("current authorization evidence missing: %v %v", evidence, err)
				}
			} else {
				if err == nil {
					t.Fatal("ineligible decision accepted")
				}
				var count int
				if err := environment.pool.QueryRow(ctx, `select count(*) from public.approvals where request_id=$1`, request.ID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("denied decision persisted: %d %v", count, err)
				}
			}
		})
	}
}
