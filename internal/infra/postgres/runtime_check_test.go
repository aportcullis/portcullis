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

// testLoginPassword is the shared password for the throwaway login roles the
// runtime-connection scenarios create.
const testLoginPassword = "pc-test-pw"

// createTestLogin idempotently provisions a LOGIN role authenticated with
// testLoginPassword (roles are cluster-wide, so existence is guarded); attrs
// appends extra role attributes (e.g. "createdb").
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

// loginPool opens a second pool to the SAME database as pool, authenticated as
// the given login user (created by the caller).
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

	// A user that is NOT a member of the configured runtime role must be caught
	// even if it happens to hold similar privileges.
	if _, err := pool.Exec(ctx, `revoke portcullis_runtime from pc_runtime_login`); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}
	if err := pg.VerifyRuntimeConnection(ctx, runtime, "portcullis_runtime"); err == nil {
		t.Error("a non-member runtime user must fail verification")
	}
}

// A login user that can SET ROLE into a privileged role — even with INHERIT
// FALSE, so it holds no inherited privileges right now — must be rejected: it can
// escalate at will after connecting (ADR-0009).
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
	// SET TRUE (default), INHERIT FALSE: reachable via SET ROLE, privileges NOT
	// inherited — so attribute/privilege snapshots of the login user look clean.
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

// A login user granted a privileged role WITH ADMIN OPTION but SET FALSE holds no
// inherited privilege and cannot SET ROLE right now — yet it can grant ITSELF the
// SET option (admin option permits it) and then escalate. Verification must reject
// it (ADR-0009): admin option is an escalation path, not just SET-reachability.
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
	// ADMIN OPTION, but SET FALSE + INHERIT FALSE: no live privilege, not
	// SET-reachable — the pre-fix SET-only scan would miss it.
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

// A login user that can SET ROLE into the database/schema owner (which can DROP
// the audit table) must be rejected even though it does not own it directly.
func TestVerifyRuntimeConnectionRejectsOwnerEscalation(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// The database owner (the connection's own superuser here) owns public/tables.
	// A login user that can SET ROLE into it can DROP SCHEMA public CASCADE.
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

// Scenario: ALTER ROLE ... SET role makes current_user the runtime role at
// connect while session_user stays the privileged login — and SET ROLE NONE
// restores it. Verification must judge the SESSION user, not the mask.
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
	// The mask: every new session starts with current_user = portcullis_runtime.
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

// Scenario: a SET FALSE, INHERIT FALSE membership conveys nothing usable — it
// must NOT block boot (only SET-reachable roles are escalation paths).
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

// Scenario: an under-privileged runtime DSN (can connect, cannot write audit)
// must be FATAL — never downgradable by the dev flag, which exists for
// OVER-privileged single-role dev, not for a server that cannot function.
func TestVerifyRuntimeConnectionUnderPrivilegeIsFatal(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	createTestLogin(t, pool, "pc_bare_login", "")
	// CONNECT only (PUBLIC's grant was revoked by 0003) — no runtime membership,
	// no table privileges.
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

// A principal with the right direct privileges but no membership is still a
// configuration error, not the owner-style over-privilege the dev flag permits.
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

// Scenario: a non-member that ALSO holds an over-privilege (here CREATE on public)
// must stay FATAL, never dev-downgradable — the excess must not reclassify the
// wrong-principal drift as the owner-style over-privilege the flag permits. Guards
// the check ordering: membership is judged before the excess-privilege classes.
func TestVerifyRuntimeConnectionNonMemberWithExcessIsFatal(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	createTestLogin(t, pool, "pc_drift_login", "")
	// Floor privileges to pass the functional checks, PLUS a stray CREATE on public
	// (an over-privilege) — but NO runtime-role membership.
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

// Scenario: a login user that can CREATE objects (schema-level CREATE on public,
// or database-level CREATE for new schemas) can craft a relation that shadows
// the audit table via name resolution. Boot verification must reject it.
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
	// A stray CREATE on the public schema — enough to plant a shadowing table.
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
	// A wrong/unmigrated database is NOT an over-privilege violation, so the dev
	// flag must never downgrade it (main.go gates on errors.Is).
	if errors.Is(err, pg.ErrRuntimeInsecure) {
		t.Error("wrong-database error must not be classified ErrRuntimeInsecure")
	}
}
