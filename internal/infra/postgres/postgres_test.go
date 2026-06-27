package postgres_test

import (
	"context"
	"testing"

	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestMigrateIdempotentAndSeedsDefaultOrg(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()

	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Running again must be a no-op, not an error.
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate (second run): %v", err)
	}

	var orgs int
	if err := pool.QueryRow(ctx, `select count(*) from organizations where slug = 'default'`).Scan(&orgs); err != nil {
		t.Fatalf("query default org: %v", err)
	}
	if orgs != 1 {
		t.Errorf("default org count = %d, want 1", orgs)
	}
}

func TestAuditEventsAreAppendOnly(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()

	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var id string
	err := pool.QueryRow(ctx, `
		insert into audit_events (organization_id, actor_type, action, target_type, outcome)
		select id, 'system', 'test', 'test', 'ok' from organizations where slug = 'default'
		returning id`).Scan(&id)
	if err != nil {
		t.Fatalf("insert audit event: %v", err)
	}

	if _, err := pool.Exec(ctx, `update audit_events set action = 'tampered' where id = $1`, id); err == nil {
		t.Error("UPDATE on audit_events must be blocked")
	}
	if _, err := pool.Exec(ctx, `delete from audit_events where id = $1`, id); err == nil {
		t.Error("DELETE on audit_events must be blocked")
	}
}
