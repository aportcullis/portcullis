package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

const testLoginPassword = "pc-test-pw"

func createTestLogin(t *testing.T, pool *pgxpool.Pool, name, attrs string) {
	t.Helper()
	stmt := fmt.Sprintf(`
		do $$ begin
			if not exists (select 1 from pg_roles where rolname = '%s') then
				create role %s login password '%s' %s;
			end if;
		end $$`, name, name, testLoginPassword, attrs)
	if _, err := pool.Exec(context.Background(), stmt); err != nil {
		t.Fatalf("create login role %s: %v", name, err)
	}
}

func loginPool(t *testing.T, pool *pgxpool.Pool, user string) *pgxpool.Pool {
	t.Helper()
	cfg := pool.Config().Copy()
	cfg.ConnConfig.User = user
	cfg.ConnConfig.Password = testLoginPassword
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open pool as %s: %v", user, err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestVerifyRuntimeConnection(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	createTestLogin(t, pool, "pc_runtime_login", "")
	if _, err := pool.Exec(ctx, `grant portcullis_runtime to pc_runtime_login`); err != nil {
		t.Fatalf("grant runtime role: %v", err)
	}
	runtime := loginPool(t, pool, "pc_runtime_login")

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

	// A user that is NOT a member of the configured runtime role must be caught even if it happens to hold similar privileges.
	if _, err := pool.Exec(ctx, `revoke portcullis_runtime from pc_runtime_login`); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}
	if err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime"); err == nil {
		t.Error("a non-member runtime user must fail verification")
	}
}

func TestVerifyRuntimeConnectionRejectsSetRoleEscalation(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	createTestLogin(t, pool, "pc_set_login", "")
	if _, err := pool.Exec(ctx, `
		do $$ begin
			if not exists (select 1 from pg_roles where rolname = 'pc_priv_grp') then
				create role pc_priv_grp nologin createdb;
			end if;
		end $$`); err != nil {
		t.Fatalf("create priv group: %v", err)
	}
	if _, err := pool.Exec(ctx, `grant portcullis_runtime to pc_set_login`); err != nil {
		t.Fatalf("grant runtime: %v", err)
	}

	if _, err := pool.Exec(ctx, `grant pc_priv_grp to pc_set_login with inherit false, set true`); err != nil {
		t.Fatalf("grant priv group: %v", err)
	}
	runtime := loginPool(t, pool, "pc_set_login")

	err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime")
	if err == nil {
		t.Fatal("a runtime user that can SET ROLE into a privileged role must be rejected")
	}
	if !strings.Contains(err.Error(), "pc_priv_grp") {
		t.Errorf("error should name the reachable role, got: %v", err)
	}
	if !errors.Is(err, pg.ErrRuntimeInsecure) {
		t.Error("an over-privilege violation must be classified ErrRuntimeInsecure (dev-flag downgradable)")
	}
}

func TestVerifyRuntimeConnectionRejectsAdminOptionEscalation(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	createTestLogin(t, pool, "pc_admin_login", "")
	if _, err := pool.Exec(ctx, `
		do $$ begin
			if not exists (select 1 from pg_roles where rolname = 'pc_adminopt_grp') then
				create role pc_adminopt_grp nologin createdb;
			end if;
		end $$`); err != nil {
		t.Fatalf("create priv group: %v", err)
	}
	if _, err := pool.Exec(ctx, `grant portcullis_runtime to pc_admin_login`); err != nil {
		t.Fatalf("grant runtime: %v", err)
	}
	// ADMIN OPTION, but SET FALSE + INHERIT FALSE: no live privilege, not SET-reachable — the pre-fix SET-only scan would miss it.
	if _, err := pool.Exec(ctx, `grant pc_adminopt_grp to pc_admin_login with admin option, inherit false, set false`); err != nil {
		t.Fatalf("grant admin option: %v", err)
	}
	runtime := loginPool(t, pool, "pc_admin_login")

	err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime")
	if err == nil {
		t.Fatal("a runtime user holding a privileged role WITH ADMIN OPTION must be rejected")
	}
	if !strings.Contains(err.Error(), "pc_adminopt_grp") {
		t.Errorf("error should name the reachable role, got: %v", err)
	}
	if !errors.Is(err, pg.ErrRuntimeInsecure) {
		t.Error("an over-privilege violation must be classified ErrRuntimeInsecure")
	}
}

func TestVerifyRuntimeConnectionRejectsOwnerEscalation(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var owner string
	if err := pool.QueryRow(ctx, `select current_user`).Scan(&owner); err != nil {
		t.Fatalf("owner: %v", err)
	}
	createTestLogin(t, pool, "pc_owner_login", "")
	if _, err := pool.Exec(ctx, `grant portcullis_runtime to pc_owner_login`); err != nil {
		t.Fatalf("grant runtime: %v", err)
	}
	if _, err := pool.Exec(ctx, `grant `+owner+` to pc_owner_login with inherit false, set true`); err != nil {
		t.Fatalf("grant owner: %v", err)
	}
	runtime := loginPool(t, pool, "pc_owner_login")

	if err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime"); err == nil {
		t.Error("a runtime user that can become the schema/DB owner must be rejected")
	}
}

func TestVerifyRuntimeConnectionSeesThroughRoleMasquerade(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	createTestLogin(t, pool, "pc_mask_login", "createdb")
	if _, err := pool.Exec(ctx, `grant portcullis_runtime to pc_mask_login`); err != nil {
		t.Fatalf("grant runtime: %v", err)
	}

	if _, err := pool.Exec(ctx, `alter role pc_mask_login set role = 'portcullis_runtime'`); err != nil {
		t.Fatalf("set default role: %v", err)
	}
	runtime := loginPool(t, pool, "pc_mask_login")

	err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime")
	if err == nil {
		t.Fatal("a privileged session_user masked by a default SET ROLE must be rejected")
	}
	if !strings.Contains(err.Error(), "pc_mask_login") {
		t.Errorf("error should name the session user, got: %v", err)
	}
}

func TestVerifyRuntimeConnectionAllowsBenignSetFalseMembership(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	createTestLogin(t, pool, "pc_benign_login", "")
	if _, err := pool.Exec(ctx, `
		do $$ begin
			if not exists (select 1 from pg_roles where rolname = 'pc_inert_grp') then
				create role pc_inert_grp nologin;
			end if;
		end $$`); err != nil {
		t.Fatalf("create inert group: %v", err)
	}
	if _, err := pool.Exec(ctx, `grant portcullis_runtime to pc_benign_login`); err != nil {
		t.Fatalf("grant runtime: %v", err)
	}
	if _, err := pool.Exec(ctx, `grant pc_inert_grp to pc_benign_login with inherit false, set false`); err != nil {
		t.Fatalf("grant inert: %v", err)
	}
	runtime := loginPool(t, pool, "pc_benign_login")

	if err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime"); err != nil {
		t.Errorf("a SET FALSE, INHERIT FALSE membership is not an escalation path; must pass: %v", err)
	}
}

func TestVerifyRuntimeConnectionUnderPrivilegeIsFatal(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	createTestLogin(t, pool, "pc_bare_login", "")

	if _, err := pool.Exec(ctx, `
		do $$ begin
			execute format('grant connect on database %I to pc_bare_login', current_database());
		end $$`); err != nil {
		t.Fatalf("grant connect: %v", err)
	}
	runtime := loginPool(t, pool, "pc_bare_login")

	err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime")
	if err == nil {
		t.Fatal("an under-privileged runtime user must fail verification")
	}
	if errors.Is(err, pg.ErrRuntimeInsecure) {
		t.Error("under-privilege must be fatal, not dev-downgradable (ErrRuntimeInsecure)")
	}
}

func TestVerifyRuntimeConnectionNonMemberIsFatal(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	createTestLogin(t, pool, "pc_direct_login", "")
	if _, err := pool.Exec(ctx, `
		do $$ begin
			execute format('grant connect on database %I to pc_direct_login', current_database());
		end $$;
		grant usage on schema public to pc_direct_login;
		grant select, insert on public.audit_events to pc_direct_login`); err != nil {
		t.Fatalf("provision direct login: %v", err)
	}
	runtime := loginPool(t, pool, "pc_direct_login")

	err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime")
	if err == nil {
		t.Fatal("a non-member runtime user must fail verification")
	}
	if errors.Is(err, pg.ErrRuntimeInsecure) {
		t.Error("safe non-membership is fatal configuration drift, not dev-downgradable")
	}
}

func TestVerifyRuntimeConnectionNonMemberWithExcessIsFatal(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	createTestLogin(t, pool, "pc_drift_login", "")

	if _, err := pool.Exec(ctx, `
		do $$ begin
			execute format('grant connect on database %I to pc_drift_login', current_database());
		end $$;
		grant usage on schema public to pc_drift_login;
		grant select, insert on public.audit_events to pc_drift_login;
		grant create on schema public to pc_drift_login`); err != nil {
		t.Fatalf("provision drift login: %v", err)
	}
	runtime := loginPool(t, pool, "pc_drift_login")

	err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime")
	if err == nil {
		t.Fatal("a non-member runtime user with excess privilege must fail verification")
	}
	if errors.Is(err, pg.ErrRuntimeInsecure) {
		t.Error("non-membership is fatal drift even alongside excess privilege — the dev flag must never mask it")
	}
	if !strings.Contains(err.Error(), "not a member") {
		t.Errorf("error should name the non-membership, got: %v", err)
	}
}

func TestVerifyRuntimeConnectionRejectsObjectCreation(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	createTestLogin(t, pool, "pc_create_login", "")
	if _, err := pool.Exec(ctx, `grant portcullis_runtime to pc_create_login`); err != nil {
		t.Fatalf("grant runtime: %v", err)
	}

	if _, err := pool.Exec(ctx, `grant create on schema public to pc_create_login`); err != nil {
		t.Fatalf("grant create: %v", err)
	}
	runtime := loginPool(t, pool, "pc_create_login")

	err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime")
	if err == nil {
		t.Fatal("a runtime user with CREATE on public must be rejected")
	}
	if !errors.Is(err, pg.ErrRuntimeInsecure) {
		t.Error("object-creation capability is an over-privilege violation (ErrRuntimeInsecure)")
	}
}

func TestVerifyRuntimeConnectionWrongDatabase(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	err := pg.VerifyRuntimeConnection(ctx, pool, "portcullis_runtime")
	if err == nil {
		t.Fatal("verification against an unmigrated database must fail")
	}
	if !strings.Contains(err.Error(), "audit_events") {
		t.Errorf("error should point at the missing schema, got: %v", err)
	}
	// A wrong/unmigrated database is NOT an over-privilege violation, so the dev flag must never downgrade it (main.go gates on errors.Is).
	if errors.Is(err, pg.ErrRuntimeInsecure) {
		t.Error("wrong-database error must not be classified ErrRuntimeInsecure")
	}
}
