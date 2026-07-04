package postgres_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

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

func TestMigratePinsPublicSearchPath(t *testing.T) {
	base := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if _, err := base.Exec(ctx, `create schema trap`); err != nil {
		t.Fatalf("create trap schema: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(base.Config().ConnString())
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "trap"
	cfg.MaxConns = 1 // force Migrate to reuse the connection carrying the temp table below
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open trap-search-path pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `create temporary table users (id integer)`); err != nil {
		t.Fatalf("create shadow temp table: %v", err)
	}

	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var inPublic, inTrap, inTemp bool
	if err := pool.QueryRow(ctx,
		`select to_regclass('public.users') is not null,
		        to_regclass('trap.users') is not null,
		        to_regclass('pg_temp.users') is not null`,
	).Scan(&inPublic, &inTrap, &inTemp); err != nil {
		t.Fatalf("inspect migrated relations: %v", err)
	}
	if !inPublic || inTrap || inTemp {
		t.Fatalf("migration namespace escaped public: public.users=%t trap.users=%t temp.users=%t", inPublic, inTrap, inTemp)
	}
}

func TestMigrateSerializesConcurrentCallers(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()

	// Several instances booting against the same empty database at once: the
	// advisory lock must serialize them so none races the check-then-apply loop.
	const callers = 4
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- pg.Migrate(ctx, pool)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Migrate: %v", err)
		}
	}

	// Every migration recorded exactly once (no duplicate-apply, no gaps).
	var dups int
	if err := pool.QueryRow(ctx,
		`select count(*) from (select version from schema_migrations group by version having count(*) > 1) d`,
	).Scan(&dups); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if dups != 0 {
		t.Errorf("found %d migrations applied more than once", dups)
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
