package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

const historyPopulation = 1281

// historyPolicyFixtureWithReads separates history correctness from tight per-decision performance budgets.
func historyPolicyFixtureWithReads(t *testing.T, pool *pgxpool.Pool, readSettings authz.ReadConfig) *pg.BindingPolicyEvaluator {
	t.Helper()
	conditionSettings := condition.DefaultConfig()
	conditionSettings.Timeout = time.Second
	resourceSettings := authz.DefaultResourceConfig()
	resourceSettings.Timeout = 5 * time.Second
	return activePolicyFixtureWithLimits(t, pool, conditionSettings, resourceSettings, readSettings)
}

// setHistoryCondition changes only the fixture's current policy before exercising the reader.
func setHistoryCondition(t *testing.T, environment administrationEnv, principal identity.UserID, expression string) {
	t.Helper()
	if _, err := environment.pool.Exec(context.Background(), `update public.role_binding_versions set condition_title='History visibility',condition_expression=$2,condition_language_version='cel-v1',condition_environment_version='portcullis-v1' where user_id=$1`, principal, expression); err != nil {
		t.Fatal(err)
	}
}

// seedHistoryBindingOverflow preserves the per-resource grant ceiling independently of history batching.
func seedHistoryBindingOverflow(t *testing.T, environment administrationEnv, principal identity.UserID) {
	t.Helper()
	if _, err := environment.pool.Exec(context.Background(), `with heads as(insert into public.role_bindings(organization_id,user_id,current_version,created_at) select $1,$2,1,clock_timestamp() from generate_series(1,$3) returning id,organization_id,user_id) insert into public.role_binding_versions(binding_id,organization_id,user_id,version,role_id,scope_type,created_at) select heads.id,heads.organization_id,heads.user_id,1,source.role_id,'organization',clock_timestamp() from heads cross join lateral(select role_id from public.role_binding_versions where organization_id=$1 and user_id=$2 limit 1) source`, environment.org, principal, authz.MaxResourceBindings); err != nil {
		t.Fatal(err)
	}
}

// assertHistoryRefusal checks that operational and binding-budget failures retain their public identity.
func assertHistoryRefusal(t *testing.T, scenario string, err error) {
	t.Helper()
	if scenario == "expired read budget" && (!errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, authz.ErrSnapshotUnavailable)) {
		t.Fatalf("read deadline identity lost: %v", err)
	}
	if scenario == "binding overflow" && !errors.Is(err, authz.ErrBindingLimitExceeded) {
		t.Fatalf("binding limit identity lost: %v", err)
	}
}
