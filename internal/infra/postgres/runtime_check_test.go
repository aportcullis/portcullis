package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// loginPool opens a second pool to the SAME database as pool, authenticated as
// the given login user (created by the caller).
func loginPool(t *testing.T, pool *pgxpool.Pool, user, password string) *pgxpool.Pool {
	t.Helper()
	cfg := pool.Config().Copy()
	cfg.ConnConfig.User = user
	cfg.ConnConfig.Password = password
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open pool as %s: %v", user, err)
	}
	t.Cleanup(p.Close)
	return p
}

// Scenario (ADR-0009): the boundary must hold for the CONNECTION the server
// actually runs on, not just the configured group role — an owner/superuser
// runtime DSN, or a login user with direct grants, must fail verification.
func TestVerifyRuntimeConnection(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// A proper least-privilege login user: member of the runtime role only.
	if _, err := pool.Exec(ctx, `
		do $$ begin
			if not exists (select 1 from pg_roles where rolname = 'pc_runtime_login') then
				create role pc_runtime_login login password 'pc-test-pw';
			end if;
		end $$`); err != nil {
		t.Fatalf("create login user: %v", err)
	}
	if _, err := pool.Exec(ctx, `grant portcullis_runtime to pc_runtime_login`); err != nil {
		t.Fatalf("grant runtime role: %v", err)
	}
	runtime := loginPool(t, pool, "pc_runtime_login", "pc-test-pw")

	if err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime"); err != nil {
		t.Errorf("least-privilege login user must pass: %v", err)
	}

	// The owner/admin connection holds implicit full privileges — must fail.
	if err := pg.VerifyRuntimeConnection(ctx, pool, "portcullis_runtime"); err == nil {
		t.Error("owner/superuser runtime connection must fail verification")
	}

	// Direct-grant drift on the LOGIN USER (not the group role) must be caught.
	if _, err := pool.Exec(ctx, `grant update on audit_events to pc_runtime_login`); err != nil {
		t.Fatalf("grant update: %v", err)
	}
	err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime")
	if err == nil {
		t.Error("login user with direct audit UPDATE must fail verification")
	} else if !strings.Contains(err.Error(), "UPDATE") {
		t.Errorf("error should name the violated privilege, got: %v", err)
	}
	if _, err := pool.Exec(ctx, `revoke update on audit_events from pc_runtime_login`); err != nil {
		t.Fatalf("revoke update: %v", err)
	}
	if err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime"); err != nil {
		t.Errorf("after revoking the drift the connection must pass again: %v", err)
	}

	// A user that is NOT a member of the configured runtime role must be caught
	// even if it happens to hold similar privileges.
	if _, err := pool.Exec(ctx, `revoke portcullis_runtime from pc_runtime_login`); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}
	if err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime"); err == nil {
		t.Error("a non-member runtime user must fail verification")
	}
}

// A runtime DSN pointing at a database that was never migrated (wrong DB) must
// fail with a clear message, not a confusing SQL error at first use.
func TestVerifyRuntimeConnectionWrongDatabase(t *testing.T) {
	pool := dbtest.FreshPostgres(t) // fresh DB, NO migrations applied
	ctx := context.Background()
	err := pg.VerifyRuntimeConnection(ctx, pool, "portcullis_runtime")
	if err == nil {
		t.Fatal("verification against an unmigrated database must fail")
	}
	if !strings.Contains(err.Error(), "audit_events") {
		t.Errorf("error should point at the missing schema, got: %v", err)
	}
}
