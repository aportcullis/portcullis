package postgres_test

import (
	"context"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedActiveFixtureRole creates owner-controlled independent membership and one explicit organization grant.
func seedActiveFixtureRole(t *testing.T, pool *pgxpool.Pool, organization identity.OrganizationID, user identity.UserID, role identity.RoleID) {
	t.Helper()
	ctx := context.Background()
	transaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `insert into public.organization_memberships(organization_id,user_id) values($1,$2) on conflict(organization_id,user_id) do nothing`, organization, user); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `with head as(insert into public.role_bindings(organization_id,user_id,current_version,created_at) values($1,$2,1,clock_timestamp()) returning id) insert into public.role_binding_versions(binding_id,organization_id,user_id,version,role_id,scope_type,created_at) select head.id,$1,$2,1,$3,'organization',clock_timestamp() from head`, organization, user, role); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
