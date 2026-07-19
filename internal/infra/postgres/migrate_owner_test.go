package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// The boot-time per-table matrix (ADR-0009 amendment): a migration that leaves
// the runtime role without its policy DML — or with a forbidden verb — must
// fail the migration postflight, naming the table and the verb, instead of
// booting and dying on the first RPC.

func TestMigrateMatrixDetectsMissingTableGrant(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := pool.Exec(ctx, `revoke select on public.connections from portcullis_runtime`); err != nil {
		t.Fatalf("induce drift: %v", err)
	}
	err := pg.Migrate(ctx, pool)
	if err == nil {
		t.Fatal("Migrate should fail when the runtime role lacks required table DML")
	}
	if !strings.Contains(err.Error(), "connections") || !strings.Contains(err.Error(), "SELECT") {
		t.Errorf("error should name the table and verb, got: %v", err)
	}

	if _, err := pool.Exec(ctx, `grant select on public.connections to portcullis_runtime`); err != nil {
		t.Fatalf("restore grant: %v", err)
	}
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Errorf("restored grants should verify again: %v", err)
	}
}

func TestMigrateMatrixDetectsForbiddenTableGrant(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// DELETE on connections is the 0007 sensitive-table revoke — re-granting it
	// breaches the no-hard-delete boundary (ADR-0014) and must fail the boot.
	if _, err := pool.Exec(ctx, `grant delete on public.connections to portcullis_runtime`); err != nil {
		t.Fatalf("induce drift: %v", err)
	}
	err := pg.Migrate(ctx, pool)
	if err == nil {
		t.Fatal("Migrate should fail when the runtime role holds a forbidden verb")
	}
	if !strings.Contains(err.Error(), "connections") || !strings.Contains(err.Error(), "DELETE") {
		t.Errorf("error should name the table and verb, got: %v", err)
	}

	if _, err := pool.Exec(ctx, `revoke delete on public.connections from portcullis_runtime`); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Errorf("restored boundary should verify again: %v", err)
	}
}

// A table the policy map has never heard of gets the DEFAULT full-DML policy,
// so a future migration that creates a table without runtime grants is caught
// on the first boot — the exact member-migrator failure class.
func TestMigrateMatrixCoversUnknownNewTable(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Created by the owner, the table inherits the default-privilege grants;
	// revoking one simulates a table created outside the owner's defaults.
	if _, err := pool.Exec(ctx, `create table if not exists public.pc_orphan (id int)`); err != nil {
		t.Fatalf("create orphan table: %v", err)
	}
	if _, err := pool.Exec(ctx, `revoke update on public.pc_orphan from portcullis_runtime`); err != nil {
		t.Fatalf("induce drift: %v", err)
	}
	err := pg.Migrate(ctx, pool)
	if err == nil {
		t.Fatal("Migrate should fail for an unknown table missing default-policy DML")
	}
	if !strings.Contains(err.Error(), "pc_orphan") || !strings.Contains(err.Error(), "UPDATE") {
		t.Errorf("error should name the unknown table and verb, got: %v", err)
	}
}

// The runtime role holds hard DELETE on NO table: every entity is soft-delete
// (data.md), no runtime query issues DELETE, and 0010 revokes the 0003-era
// blanket grant for current tables and the default privileges for future ones
// (external review; OWASP least privilege). A table that genuinely needs
// DELETE — e.g. a future result-cache TTL eviction (ADR-0011) — must grant it
// in its own migration and register a tablePolicies exception.
func TestMigrateRevokesRuntimeDelete(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows, err := pool.Query(ctx, `
		select c.relname, has_table_privilege('portcullis_runtime', c.oid, 'DELETE')
		from pg_class c join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public' and c.relkind in ('r', 'p')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table string
		var canDelete bool
		if err := rows.Scan(&table, &canDelete); err != nil {
			t.Fatal(err)
		}
		if canDelete {
			t.Errorf("runtime role holds DELETE on %s — hard delete is soft-delete-only territory (data.md)", table)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	// A regressed DELETE grant on an entity table is boundary drift: boot fails
	// until it is revoked (default policy forbids DELETE).
	if _, err := pool.Exec(ctx, `grant delete on public.users to portcullis_runtime`); err != nil {
		t.Fatalf("induce drift: %v", err)
	}
	err = pg.Migrate(ctx, pool)
	if err == nil {
		t.Fatal("Migrate should fail when the runtime role regains DELETE")
	}
	if !strings.Contains(err.Error(), "users") || !strings.Contains(err.Error(), "DELETE") {
		t.Errorf("error should name the table and verb, got: %v", err)
	}
	if _, err := pool.Exec(ctx, `revoke delete on public.users from portcullis_runtime`); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Errorf("restored boundary should verify again: %v", err)
	}

	// The default-privilege revoke covers tables FUTURE migrations create: a
	// new owner-created table must arrive without DELETE.
	for _, stmt := range []string{
		`create table public.pc_no_delete (id int)`,
		`grant select, insert, update on public.pc_no_delete to portcullis_runtime`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	var futureDelete bool
	if err := pool.QueryRow(ctx,
		`select has_table_privilege('portcullis_runtime', 'public.pc_no_delete', 'DELETE')`,
	).Scan(&futureDelete); err != nil {
		t.Fatal(err)
	}
	if futureDelete {
		t.Error("default privileges still grant DELETE — a future migration's table would regain hard delete")
	}
}

// TestRuntimeRoleRotationRunbook executes the ADR-0009 role-rotation runbook
// verbatim (keep the statement lists in sync with the ADR by hand) against a
// database that HAS a sequence, then boot-verifies the new role and fully
// decommissions the rotated-away role. Without the runbook's sequence
// statements the new role lacks sequence USAGE (boot fails), and the old
// role's default-privilege entries block DROP ROLE (external review). The
// runbook's `grant <new> to <login user>` step is omitted — Migrate verifies
// the group role itself, and no login user exists in this test.
func TestRuntimeRoleRotationRunbook(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// A real sequence, created by the owner as a future migration would: the
	// rotated role's sequence USAGE requirement must be exercised, not vacuous.
	if _, err := pool.Exec(ctx, `create sequence public.pc_rot_seq`); err != nil {
		t.Fatalf("create sequence: %v", err)
	}

	var db string
	if err := pool.QueryRow(ctx, `select current_database()`).Scan(&db); err != nil {
		t.Fatalf("current database: %v", err)
	}
	ident := pgx.Identifier{db}.Sanitize()

	// New-role block of the runbook (rotating portcullis_runtime → pc_rot_new;
	// cluster-wide role names are test-unique, the shared default role is not
	// touched).
	for _, stmt := range []string{
		`create role pc_rot_new nologin`,
		`revoke temporary on database ` + ident + ` from public`,
		`grant connect on database ` + ident + ` to pc_rot_new`,
		`grant usage on schema public to pc_rot_new`,
		`grant select, insert, update on all tables in schema public to pc_rot_new`,
		`grant usage on all sequences in schema public to pc_rot_new`,
		`revoke update on public.audit_events from pc_rot_new`,
		`revoke update on public.connection_policy_versions from pc_rot_new`,
		`revoke all on public.schema_migrations from pc_rot_new`,
		`alter default privileges in schema public grant select, insert, update on tables to pc_rot_new`,
		`alter default privileges in schema public grant usage on sequences to pc_rot_new`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	// The rotated configuration must pass the full boot verification —
	// including sequence USAGE on pc_rot_seq.
	if err := pg.Migrate(ctx, pool, pg.WithRuntimeRole("pc_rot_new")); err != nil {
		t.Fatalf("Migrate with rotated role: %v", err)
	}

	// Old-role decommission block, applied to the just-created role (same
	// statements the runbook runs against <old>): without the default-privilege
	// revokes — sequences included — DROP ROLE fails with a dependency error.
	for _, stmt := range []string{
		`alter default privileges in schema public revoke select, insert, update on tables from pc_rot_new`,
		`alter default privileges in schema public revoke usage on sequences from pc_rot_new`,
		`revoke all on all tables in schema public from pc_rot_new`,
		`revoke usage on all sequences in schema public from pc_rot_new`,
		`revoke usage on schema public from pc_rot_new`,
		`revoke connect on database ` + ident + ` from pc_rot_new`,
		`drop role pc_rot_new`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

// clusterRoles creates the shared owner/member login roles used by the
// member-migrator scenarios. Roles are cluster-wide: fixed names, if-not-exists
// guards, never dropped (the throwaway container dies per test binary); an
// external PORTCULLIS_TEST_DATABASE_URL run without role privileges skips.
func clusterRoles(t *testing.T, pool *pgxpool.Pool, owner, member, memberPassword, setOption string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		do $$ begin
			if not exists (select 1 from pg_roles where rolname = '`+owner+`') then
				create role `+owner+` nologin;
			end if;
			if not exists (select 1 from pg_roles where rolname = '`+member+`') then
				create role `+member+` login password '`+memberPassword+`';
			end if;
		end $$`); err != nil {
		t.Skipf("cannot create cluster roles (external test DB without role privileges?): %v", err)
	}
	// Re-granting updates the membership options if the roles pre-existed.
	if _, err := pool.Exec(ctx, `grant `+owner+` to `+member+` with `+setOption); err != nil {
		t.Skipf("cannot grant membership: %v", err)
	}
}

// giveDatabaseTo hands the current test database to the owner role and lets
// the member connect (0003 revokes PUBLIC CONNECT; the explicit grant keeps
// later pool connections working).
func giveDatabaseTo(t *testing.T, pool *pgxpool.Pool, owner, member string) {
	t.Helper()
	ctx := context.Background()
	var db string
	if err := pool.QueryRow(ctx, `select current_database()`).Scan(&db); err != nil {
		t.Fatalf("current database: %v", err)
	}
	ident := pgx.Identifier{db}.Sanitize()
	if _, err := pool.Exec(ctx, `alter database `+ident+` owner to `+owner); err != nil {
		t.Skipf("cannot reassign database owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `grant connect on database `+ident+` to `+member); err != nil {
		t.Fatalf("grant connect: %v", err)
	}
}

// memberPool opens a second pool to the same database as the member login.
func memberPool(t *testing.T, pool *pgxpool.Pool, user, password string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(pool.Config().ConnString())
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	cfg.ConnConfig.User = user
	cfg.ConnConfig.Password = password
	mp, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open member pool: %v", err)
	}
	t.Cleanup(mp.Close)
	return mp
}

// The member-migrator scenario end-to-end (ADR-0009 amendment): a login that is
// only a SET-capable member of the schema owner migrates a fresh database. The
// SET ROLE inside Migrate must make every object owner-owned and — the actual
// bug under test — bind the default privileges to the OWNER, so tables created
// by future migrations still get runtime grants.
func TestMigrateAsOwnerMemberAppliesDefaultPrivileges(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	clusterRoles(t, pool, "pc_mig_owner", "pc_mig_member", "pc-mig-member", "set true, inherit false")
	giveDatabaseTo(t, pool, "pc_mig_owner", "pc_mig_member")

	mp := memberPool(t, pool, "pc_mig_member", "pc-mig-member")
	if err := pg.Migrate(ctx, mp); err != nil {
		t.Fatalf("Migrate as owner member: %v", err)
	}

	// Objects belong to the owner, not the member login.
	for _, table := range []string{"connections", "schema_migrations"} {
		var relowner string
		if err := pool.QueryRow(ctx,
			`select relowner::regrole::text from pg_class where oid = to_regclass('public.'||$1)`, table,
		).Scan(&relowner); err != nil {
			t.Fatalf("relowner of %s: %v", table, err)
		}
		if relowner != "pc_mig_owner" {
			t.Errorf("relowner(%s) = %q, want pc_mig_owner", table, relowner)
		}
	}

	// The runtime boundary holds exactly as in an owner-run migration.
	var canSelect, canDelete bool
	if err := pool.QueryRow(ctx, `
		select has_table_privilege('portcullis_runtime', 'public.connections', 'SELECT'),
		       has_table_privilege('portcullis_runtime', 'public.connections', 'DELETE')`,
	).Scan(&canSelect, &canDelete); err != nil {
		t.Fatal(err)
	}
	if !canSelect || canDelete {
		t.Errorf("runtime on connections: SELECT=%t DELETE=%t, want true/false", canSelect, canDelete)
	}

	// THE fix under test: default privileges are bound to the owner, so a table
	// a FUTURE migration would create (as the owner) gets runtime grants.
	for _, stmt := range []string{
		`set role pc_mig_owner`,
		`create table public.pc_future (id int)`,
		`reset role`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	var futureSelect bool
	if err := pool.QueryRow(ctx,
		`select has_table_privilege('portcullis_runtime', 'public.pc_future', 'SELECT')`,
	).Scan(&futureSelect); err != nil {
		t.Fatal(err)
	}
	if !futureSelect {
		t.Error("default privileges are not bound to the owner — a future migration's table would have no runtime grants")
	}

	// Idempotent re-run through the same member login.
	if err := pg.Migrate(ctx, mp); err != nil {
		t.Errorf("second Migrate as member: %v", err)
	}
}

// INHERIT-only membership (no SET option) can run REVOKEs but can never fix
// the default-privilege binding, so the tightened preflight refuses it with
// actionable guidance.
func TestMigrateRejectsMemberWithoutSetOption(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	clusterRoles(t, pool, "pc_mig_owner2", "pc_mig_member2", "pc-mig-member2", "set false, inherit true")
	giveDatabaseTo(t, pool, "pc_mig_owner2", "pc_mig_member2")

	mp := memberPool(t, pool, "pc_mig_member2", "pc-mig-member2")
	err := pg.Migrate(ctx, mp)
	if err == nil {
		t.Fatal("Migrate should refuse a migrator that cannot SET ROLE into the owner")
	}
	if !strings.Contains(err.Error(), "SET") || !strings.Contains(err.Error(), "pc_mig_owner2") {
		t.Errorf("error should carry the SET-membership guidance, got: %v", err)
	}
}
