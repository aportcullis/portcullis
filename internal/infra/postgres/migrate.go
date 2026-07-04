package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/migrations"
)

// runtimeRolePattern is the shape a runtime role name must have: it is spliced
// into migration SQL as an identifier, so anything else is rejected outright
// (PostgreSQL identifiers are also capped at 63 bytes).
var runtimeRolePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// WithRuntimeRole overrides the name of the least-privilege runtime role the
// migrations create and grant (default "portcullis_runtime"). Give each install
// on a SHARED PostgreSQL cluster its own name: roles are cluster-wide, so two
// installs using the same name would merge their privileges (ADR-0009).
func WithRuntimeRole(name string) MigrateOption {
	return func(c *migrateConfig) { c.runtimeRole = name }
}

// Migrate applies every embedded migration not yet recorded, each in its own
// transaction, in lexical filename order. It is safe to call on every startup.
//
// A session-level advisory lock on a dedicated connection serializes concurrent
// instance startups, so two processes booting together can't both run the same
// (unrecorded) migration and have one fail — the second waits, then finds every
// migration already applied.
func Migrate(ctx context.Context, pool *pgxpool.Pool, opts ...MigrateOption) error {
	cfg := migrateConfig{runtimeRole: defaultRuntimeRole}
	for _, o := range opts {
		o(&cfg)
	}
	// The role name is spliced into SQL as an identifier — validate even though
	// config.Load already did (defense in depth for other callers).
	if !runtimeRolePattern.MatchString(cfg.runtimeRole) {
		return fmt.Errorf("invalid runtime role name %q", cfg.runtimeRole)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration lock connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `select pg_advisory_lock($1, $2)`, lockClassMigrate, 0); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		// Explicit unlock — a session lock is not dropped when the connection returns
		// to the pool. WithoutCancel so a cancelled ctx still releases the lock.
		_, _ = conn.Exec(context.WithoutCancel(ctx), `select pg_advisory_unlock($1, $2)`, lockClassMigrate, 0)
	}()

	// Migration files intentionally use concise, mostly unqualified object names.
	// Never inherit an ALTER ROLE / DSN search_path: it could create or inspect the
	// metadata schema somewhere other than public. Drop any temp objects left on a
	// reused owner connection too; pg_temp is otherwise searched ahead of the
	// configured path and can shadow an unqualified relation. With only public in
	// the explicit path, PostgreSQL implicitly searches pg_catalog first while
	// keeping public as the target for unqualified CREATE statements.
	if _, err := conn.Exec(ctx, `discard temp`); err != nil {
		return fmt.Errorf("clear migration temporary schema: %w", err)
	}
	if _, err := conn.Exec(ctx, `select set_config('search_path', 'public', false)`); err != nil {
		return fmt.Errorf("set migration search path: %w", err)
	}

	// Every step runs on the lock-holding connection, so a flood of concurrent
	// callers can't starve the pool: each caller holds exactly one connection
	// (blocked on the lock) and the winner does all its work on that same one.
	if _, err := conn.Exec(ctx, `create table if not exists public.schema_migrations (
		version text primary key,
		applied_at timestamptz not null default now())`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	// Preflight: a PRE-EXISTING runtime role must be attribute-safe BEFORE any
	// migration grants it CONNECT + DML — otherwise a role holding e.g. CREATEDB
	// would end up privileged even though boot then fails.
	if err := checkRuntimeRoleAttributes(ctx, conn, cfg.runtimeRole); err != nil {
		return err
	}

	// Preflight: the migration principal must be able to act as the owner of the
	// database AND schema public. The boundary migrations REVOKE PUBLIC's privileges
	// as the owner; a non-owner makes those REVOKEs silent no-ops (PostgreSQL warns
	// but commits), the migration records as applied, and the post-flight
	// verifyRuntimeRole then fails EVERY boot blaming the runtime role — with no
	// migration left to self-repair. Fail fast here with an accurate message.
	if err := checkMigratorOwnership(ctx, conn); err != nil {
		return err
	}

	// Publish the runtime role name as a session GUC on the migration connection.
	// Migrations read it via current_setting and splice it with format(%I) — no
	// blind text substitution (which could rewrite an unrelated substring in a
	// future migration, or leak the dev-membership grant to a custom-name install).
	if _, err := conn.Exec(ctx, `select set_config('portcullis.runtime_role', $1, false)`, cfg.runtimeRole); err != nil {
		return fmt.Errorf("set runtime role setting: %w", err)
	}

	names, err := migrationNames()
	if err != nil {
		return err
	}

	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		var applied bool
		if err := conn.QueryRow(ctx,
			`select exists(select 1 from public.schema_migrations where version = $1)`, version,
		).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}
		if err := applyOne(ctx, conn, version, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", version, err)
		}
	}

	// Postflight: verify the runtime role's EFFECTIVE state on every boot. This
	// catches a renamed PORTCULLIS_RUNTIME_ROLE (0003 is version-recorded, so a
	// new name never receives grants) and later drift (attributes added, grants
	// changed) — failing fast instead of leaving the old role privileged and the
	// new one powerless (ADR-0009; rotation procedure documented there).
	return verifyRuntimeRole(ctx, conn, cfg.runtimeRole)
}

// checkMigratorOwnership fails unless the migration principal can act as the owner
// of BOTH the current database and schema public — i.e. it is that owner or a role
// that inherits/can SET into it (a superuser satisfies this implicitly). Without it,
// the boundary migrations' `revoke ... from public` statements silently no-op, which
// later fails verifyRuntimeRole on every boot with a misleading runtime-role error.
func checkMigratorOwnership(ctx context.Context, q rowQuerier) error {
	var user string
	var ownsDB, ownsSchema bool
	if err := q.QueryRow(ctx, `
		select current_user,
		       pg_has_role(current_user, (select datdba from pg_database where datname = current_database()), 'USAGE')
		         or pg_has_role(current_user, (select datdba from pg_database where datname = current_database()), 'SET'),
		       coalesce((select pg_has_role(current_user, nspowner, 'USAGE') or pg_has_role(current_user, nspowner, 'SET')
		                 from pg_namespace where nspname = 'public'), false)`,
	).Scan(&user, &ownsDB, &ownsSchema); err != nil {
		return fmt.Errorf("check migration ownership: %w", err)
	}
	if !ownsDB || !ownsSchema {
		return safeErrorf("migration user %q cannot act as the owner of the database and schema public (owns database=%t, owns schema public=%t) — the runtime-boundary migrations REVOKE PUBLIC privileges AS the owner, which silently no-op otherwise and then fail boot verification (ADR-0009); run migrations as the schema owner or a member of it", user, ownsDB, ownsSchema)
	}
	return nil
}

// checkRuntimeRoleAttributes fails if the role exists with cluster attributes a
// least-privilege runtime role must never hold: members can SET ROLE into the
// role (allowed by default), which would hand them those powers. LOGIN is
// deliberately allowed — a deployment may use the role directly as its login user.
func checkRuntimeRoleAttributes(ctx context.Context, q rowQuerier, role string) error {
	var unsafe bool
	err := q.QueryRow(ctx,
		`select `+unsafeRoleAttrsSQL+` from pg_roles where rolname = $1`, role,
	).Scan(&unsafe)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // not created yet — migration 0003 will create it safely
	}
	if err != nil {
		return fmt.Errorf("inspect runtime role %q: %w", role, err)
	}
	if unsafe {
		return safeErrorf("runtime role %q holds a dangerous attribute (%s); refusing to grant it privileges (ADR-0009)", role, unsafeRoleAttrsList)
	}
	return nil
}

// verifyRuntimeRole asserts the role exists, is attribute-safe, and holds
// exactly the intended privileges: CONNECT, append-only audit access
// (SELECT+INSERT, no mutation), and no access at all to the migration history.
func verifyRuntimeRole(ctx context.Context, conn *pgxpool.Conn, role string) error {
	if err := checkRuntimeRoleAttributes(ctx, conn, role); err != nil {
		return err
	}
	m, err := queryPrivilegeMatrix(ctx, conn, role)
	if errors.Is(err, pgx.ErrNoRows) || isUndefinedObject(err) {
		return safeErrorf("runtime role %q does not exist — was PORTCULLIS_RUNTIME_ROLE changed after the first migration? Follow the rotation procedure in ADR-0009", role)
	}
	if err != nil {
		return fmt.Errorf("verify runtime role %q: %w", role, err)
	}
	return m.verdict(fmt.Sprintf("runtime role %q", role),
		" — was PORTCULLIS_RUNTIME_ROLE changed after the first migration? Follow the rotation procedure in ADR-0009")
}

// isUndefinedObject reports whether err is PostgreSQL's undefined_object (42704)
// — has_*_privilege raises it for a role name that does not exist.
func isUndefinedObject(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42704"
}

func migrationNames() ([]string, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func applyOne(ctx context.Context, conn *pgxpool.Conn, version, body string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, body); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into public.schema_migrations (version) values ($1)`, version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
