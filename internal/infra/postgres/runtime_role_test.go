package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// assertRuntimeRoleBoundary pins the permission boundary (ADR-0009, PRD:387) for
// the given runtime role: it can insert and read audit events but can neither
// mutate them nor reach the trigger/table definition (only the owner, which runs
// migrations, can).
func assertRuntimeRoleBoundary(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()
	ctx := context.Background()

	// Seed one event as the owner (via the normal path).
	store := pg.NewIdentityStore(pool)
	if _, err := store.BootstrapAdmin(ctx, "admin@example.com", "Admin", "phc-hash",
		testEvent(audit.ActionAuthBootstrap)); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}

	// All statements must run on ONE connection: SET ROLE is session state — and
	// must be RESET before the connection returns to the pool, or a later
	// checkout would silently run as the runtime role.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	defer func() { _, _ = conn.Exec(ctx, `reset role`) }()
	if _, err := conn.Exec(ctx, `set role `+role); err != nil {
		t.Fatalf("set role %s (migration should create it): %v", role, err)
	}

	// Allowed: read and append.
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from audit_events`).Scan(&n); err != nil {
		t.Errorf("runtime SELECT on audit_events should be allowed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into audit_events (organization_id, actor_type, action, target_type, outcome)
		select id, 'system', 'AUTH_LOGIN', 'user', 'FAILED' from organizations limit 1`); err != nil {
		t.Errorf("runtime INSERT on audit_events should be allowed: %v", err)
	}

	// Forbidden: mutating rows (privilege check fires before the trigger).
	if _, err := conn.Exec(ctx, `update audit_events set outcome = 'FAILED'`); err == nil {
		t.Error("runtime UPDATE on audit_events must be denied")
	}
	if _, err := conn.Exec(ctx, `delete from audit_events`); err == nil {
		t.Error("runtime DELETE on audit_events must be denied")
	}
	// Forbidden: reaching the table definition / triggers (owner-only).
	if _, err := conn.Exec(ctx, `alter table audit_events disable trigger audit_events_no_mutation`); err == nil {
		t.Error("runtime ALTER TABLE ... DISABLE TRIGGER must be denied")
	}
	if _, err := conn.Exec(ctx, `drop table audit_events`); err == nil {
		t.Error("runtime DROP TABLE must be denied")
	}

	// The runtime role still operates the app's other tables (smoke check).
	if _, err := conn.Exec(ctx, `update users set display_name = 'Renamed' where email = 'admin@example.com'`); err != nil {
		t.Errorf("runtime UPDATE on users should be allowed: %v", err)
	}

	// Migration history is owner-only: with any access, the runtime could delete
	// applied records (forcing re-runs) or pre-insert future versions (skipping
	// security migrations).
	for _, stmt := range []string{
		`select count(*) from schema_migrations`,
		`insert into schema_migrations (version) values ('9999_fake')`,
		`update schema_migrations set version = version`,
		`delete from schema_migrations`,
	} {
		if _, err := conn.Exec(ctx, stmt); err == nil {
			t.Errorf("runtime access to schema_migrations must be denied: %q succeeded", stmt)
		}
	}
}

func TestRuntimeRoleCannotMutateAuditEvents(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertRuntimeRoleBoundary(t, pool, "portcullis_runtime")
}

// Scenario: a shared cluster hosts several installs — each configures its OWN
// runtime role name, and CONNECT is no longer implicit via PUBLIC, so one
// install's runtime user gains nothing on another install's database.
func TestMigrateWithCustomRuntimeRoleIsolatesConnect(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool, pg.WithRuntimeRole("pc_custom_rt")); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// The custom-named role gets the exact same permission boundary.
	assertRuntimeRoleBoundary(t, pool, "pc_custom_rt")

	// The default-named role was never created in this install.
	var exists bool
	if err := pool.QueryRow(ctx,
		`select exists(select 1 from pg_roles where rolname = 'portcullis_runtime')
		 and exists(select 1 from information_schema.role_table_grants
		            where grantee = 'portcullis_runtime' and table_name = 'audit_events')`,
	).Scan(&exists); err != nil {
		t.Fatalf("check default role grants: %v", err)
	}
	if exists {
		t.Error("default role must not receive grants when a custom name is configured")
	}

	// CONNECT is explicit: the runtime role has it, an unrelated role does not
	// (PUBLIC's implicit CONNECT is revoked).
	if _, err := pool.Exec(ctx, `
		do $$ begin
			if not exists (select 1 from pg_roles where rolname = 'pc_bystander') then
				create role pc_bystander nologin;
			end if;
		end $$`); err != nil {
		t.Fatalf("create bystander role: %v", err)
	}
	var runtimeCan, bystanderCan bool
	if err := pool.QueryRow(ctx,
		`select has_database_privilege('pc_custom_rt', current_database(), 'CONNECT'),
		        has_database_privilege('pc_bystander', current_database(), 'CONNECT')`,
	).Scan(&runtimeCan, &bystanderCan); err != nil {
		t.Fatalf("check connect privileges: %v", err)
	}
	if !runtimeCan {
		t.Error("runtime role must hold CONNECT on the database")
	}
	if bystanderCan {
		t.Error("an unrelated role must NOT hold CONNECT (PUBLIC grant should be revoked)")
	}
}

// Scenario: 0003 is version-recorded, so changing PORTCULLIS_RUNTIME_ROLE later
// can never re-apply the grants — boot must fail loudly with the new name in the
// error instead of leaving the old role privileged and the new one powerless.
func TestMigrateDetectsRuntimeRoleRename(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil { // first boot: default role
		t.Fatalf("migrate: %v", err)
	}
	err := pg.Migrate(ctx, pool, pg.WithRuntimeRole("pc_renamed_b"))
	if err == nil {
		t.Fatal("Migrate with a renamed runtime role should fail (grants were never applied to it)")
	}
	if !strings.Contains(err.Error(), "pc_renamed_b") {
		t.Errorf("error should name the misconfigured role, got: %v", err)
	}
}

// Scenario: privilege DRIFT after install — extra grants (audit DELETE) or
// missing grants (audit SELECT) on the runtime role must fail the next boot,
// with the violated privilege named. has_table_privilege is effective-privilege
// based, so grants inherited via membership are caught the same way.
func TestMigrateDetectsPrivilegeDrift(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Extra mutation privilege sneaks in → boot must fail naming it.
	if _, err := pool.Exec(ctx, `grant delete on audit_events to portcullis_runtime`); err != nil {
		t.Fatalf("grant delete: %v", err)
	}
	err := pg.Migrate(ctx, pool)
	if err == nil {
		t.Fatal("Migrate must fail while the runtime role can DELETE audit_events")
	}
	if !strings.Contains(err.Error(), "DELETE") {
		t.Errorf("error should name the violated privilege, got: %v", err)
	}
	if _, err := pool.Exec(ctx, `revoke delete on audit_events from portcullis_runtime`); err != nil {
		t.Fatalf("revoke delete: %v", err)
	}

	// TRIGGER privilege would let the runtime CREATE TRIGGER on the audit table
	// (blocking every insert = silent audit DoS) → boot must fail naming it.
	if _, err := pool.Exec(ctx, `grant trigger on audit_events to portcullis_runtime`); err != nil {
		t.Fatalf("grant trigger: %v", err)
	}
	err = pg.Migrate(ctx, pool)
	if err == nil {
		t.Fatal("Migrate must fail while the runtime role holds TRIGGER on audit_events")
	}
	if !strings.Contains(err.Error(), "TRIGGER") {
		t.Errorf("error should name the violated privilege, got: %v", err)
	}
	if _, err := pool.Exec(ctx, `revoke trigger on audit_events from portcullis_runtime`); err != nil {
		t.Fatalf("revoke trigger: %v", err)
	}

	// A required privilege goes missing → boot must fail naming it.
	if _, err := pool.Exec(ctx, `revoke select on audit_events from portcullis_runtime`); err != nil {
		t.Fatalf("revoke select: %v", err)
	}
	err = pg.Migrate(ctx, pool)
	if err == nil {
		t.Fatal("Migrate must fail while the runtime role cannot SELECT audit_events")
	}
	if !strings.Contains(err.Error(), "SELECT") {
		t.Errorf("error should name the missing privilege, got: %v", err)
	}
	if _, err := pool.Exec(ctx, `grant select on audit_events to portcullis_runtime`); err != nil {
		t.Fatalf("restore select: %v", err)
	}
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Errorf("restored grants should boot again: %v", err)
	}
}

// Scenario: a deployment uses the login user ITSELF as the runtime role
// (PORTCULLIS_RUNTIME_ROLE=portcullis_app). The dev-membership grant would then
// be a forbidden self-grant — the migration must skip it and still verify.
func TestMigrateAllowsLoginUserAsRuntimeRole(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		do $$ begin
			if not exists (select 1 from pg_roles where rolname = 'portcullis_app') then
				create role portcullis_app nologin;
			end if;
		end $$`); err != nil {
		t.Fatalf("provision portcullis_app: %v", err)
	}
	if err := pg.Migrate(ctx, pool, pg.WithRuntimeRole("portcullis_app")); err != nil {
		t.Fatalf("Migrate with the login user as runtime role must not self-grant: %v", err)
	}
	assertRuntimeRoleBoundary(t, pool, "portcullis_app")
}

// Scenario: a pre-existing role with dangerous cluster attributes must be
// rejected, not silently granted CONNECT + DML (members could SET ROLE into it).
func TestMigrateRejectsUnsafeRuntimeRole(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		do $$ begin
			if not exists (select 1 from pg_roles where rolname = 'pc_evil_rt') then
				create role pc_evil_rt nologin createdb;
			end if;
		end $$`); err != nil {
		t.Fatalf("create unsafe role: %v", err)
	}
	err := pg.Migrate(ctx, pool, pg.WithRuntimeRole("pc_evil_rt"))
	if err == nil {
		t.Fatal("Migrate must reject a runtime role holding CREATEDB")
	}
	if !strings.Contains(err.Error(), "pc_evil_rt") {
		t.Errorf("error should name the unsafe role, got: %v", err)
	}
}
