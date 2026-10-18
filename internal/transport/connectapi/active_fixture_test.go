package connectapi_test

import (
	"context"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/celcondition"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// activeAPIPolicy qualifies the same resource-aware authorizer and CEL adapter as the production root.
func activeAPIPolicy(t *testing.T, pool *pgxpool.Pool) (identity.OrganizationID, *pg.BindingPolicyEvaluator, *authz.ActiveService) {
	t.Helper()
	store := pg.NewIdentityStore(pool)
	organization, err := store.DefaultOrganizationID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := store.ListPermissions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	conditions, err := condition.New(adapter, adapter, condition.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	settings := authz.DefaultResourceConfig()
	settings.Timeout = 5 * time.Second
	policy, err := pg.NewBindingPolicyEvaluator(conditions, catalog, settings, authz.DefaultReadConfig())
	if err != nil {
		t.Fatal(err)
	}
	repository, err := pg.NewBindingPolicyStore(pool, policy, organization)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ValidateActivation(context.Background()); err != nil {
		t.Fatal(err)
	}
	service, err := authz.NewActive(repository)
	if err != nil {
		t.Fatal(err)
	}
	return organization, policy, service
}

// seedAPIMembership creates grantless membership separately from role grants.
func seedAPIMembership(t *testing.T, pool *pgxpool.Pool, organization, user string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `insert into public.organization_memberships(organization_id,user_id) values($1,$2) on conflict(organization_id,user_id) do nothing`, organization, user); err != nil {
		t.Fatal(err)
	}
}

// seedAPIRole creates an explicit owner-controlled organization Binding for integration fixtures.
func seedAPIRole(t *testing.T, pool *pgxpool.Pool, organization, user, role string) {
	t.Helper()
	seedAPIMembership(t, pool, organization, user)
	if _, err := pool.Exec(context.Background(), `with head as(insert into public.role_bindings(organization_id,user_id,current_version,created_at) values($1,$2,1,clock_timestamp()) returning id) insert into public.role_binding_versions(binding_id,organization_id,user_id,version,role_id,scope_type,created_at) select head.id,$1,$2,1,$3,'organization',clock_timestamp() from head`, organization, user, role); err != nil {
		t.Fatal(err)
	}
}
