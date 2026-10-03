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

	// DELETE on connections is the 0007 sensitive-table revoke — re-granting it breaches the no-hard-delete boundary (ADR-0014) and must fail the boot.
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

func TestMigrateMatrixCoversUnknownNewTable(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

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

	deleteAllowed := map[string]bool{"settings": true}
	for rows.Next() {
		var table string
		var canDelete bool
		if err := rows.Scan(&table, &canDelete); err != nil {
			t.Fatal(err)
		}
		if canDelete != deleteAllowed[table] {
			t.Errorf("runtime role DELETE on %s = %t, want %t (data.md: soft-delete-only, settings excepted per ADR-0017)", table, canDelete, deleteAllowed[table])
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

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

	// The default-privilege revoke covers tables FUTURE migrations create: a new owner-created table must arrive without DELETE.
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

func TestRuntimeRoleRotationRunbook(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// A real sequence, created by the owner as a future migration would: the rotated role's sequence USAGE requirement must be exercised, not vacuous.
	if _, err := pool.Exec(ctx, `create sequence public.pc_rot_seq`); err != nil {
		t.Fatalf("create sequence: %v", err)
	}

	var db string
	if err := pool.QueryRow(ctx, `select current_database()`).Scan(&db); err != nil {
		t.Fatalf("current database: %v", err)
	}
	ident := pgx.Identifier{db}.Sanitize()

	for _, stmt := range []string{
		`create role pc_rot_new nologin`,
		`revoke temporary on database ` + ident + ` from public`,
		`grant connect on database ` + ident + ` to pc_rot_new`,
		`grant usage on schema public to pc_rot_new`,
		`grant usage on schema result_cache to pc_rot_new`,
		`grant select, insert, update, delete on all tables in schema result_cache to pc_rot_new`,
		`grant select, insert, update on all tables in schema public to pc_rot_new`,
		`grant usage on all sequences in schema public to pc_rot_new`,
		`grant delete on public.settings to pc_rot_new`,
		`revoke update on public.audit_events from pc_rot_new`,
		`revoke update on public.connection_policy_versions from pc_rot_new`,
		`revoke update on public.approvals from pc_rot_new`,
		`revoke all on public.schema_migrations from pc_rot_new`,
		`alter default privileges in schema public grant select, insert, update on tables to pc_rot_new`,
		`alter default privileges in schema public grant usage on sequences to pc_rot_new`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	// The rotated configuration must pass the full boot verification — including sequence USAGE on pc_rot_seq.
	if err := pg.Migrate(ctx, pool, pg.WithRuntimeRole("pc_rot_new")); err != nil {
		t.Fatalf("Migrate with rotated role: %v", err)
	}

	for _, stmt := range []string{
		`alter default privileges in schema public revoke select, insert, update on tables from pc_rot_new`,
		`alter default privileges in schema public revoke usage on sequences from pc_rot_new`,
		`revoke all on all tables in schema public from pc_rot_new`,
		`revoke usage on all sequences in schema public from pc_rot_new`,
		`revoke usage on schema public from pc_rot_new`,
		`revoke all on all tables in schema result_cache from pc_rot_new`,
		`revoke usage on schema result_cache from pc_rot_new`,
		`revoke connect on database ` + ident + ` from pc_rot_new`,
		`drop role pc_rot_new`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

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

	if _, err := pool.Exec(ctx, `grant `+owner+` to `+member+` with `+setOption); err != nil {
		t.Skipf("cannot grant membership: %v", err)
	}
}

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

func TestMigrateAsOwnerMemberAppliesDefaultPrivileges(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	clusterRoles(t, pool, "pc_mig_owner", "pc_mig_member", "pc-mig-member", "set true, inherit false")
	giveDatabaseTo(t, pool, "pc_mig_owner", "pc_mig_member")

	mp := memberPool(t, pool, "pc_mig_member", "pc-mig-member")
	if err := pg.Migrate(ctx, mp); err != nil {
		t.Fatalf("Migrate as owner member: %v", err)
	}

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

	// THE fix under test: default privileges are bound to the owner, so a table a FUTURE migration would create (as the owner) gets runtime grants.
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

	if err := pg.Migrate(ctx, mp); err != nil {
		t.Errorf("second Migrate as member: %v", err)
	}
}

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
