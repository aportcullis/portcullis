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

// runtimeRolePattern is the shape a runtime role name must have: it is spliced into migration SQL as an identifier, so anything else is rejected outright (PostgreSQL identifiers are also capped at 63 bytes).
var runtimeRolePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// WithRuntimeRole overrides the name of the least-privilege runtime role the migrations create and grant (default "portcullis_runtime"). Give each install on a SHARED PostgreSQL cluster its own name: roles are cluster-wide, so two installs using the same name would merge their privileges (ADR-0009).
func WithRuntimeRole(name string) MigrateOption {
	return func(c *migrateConfig) { c.runtimeRole = name }
}

// Migrate applies unrecorded files in filename order, one transaction each, on a dedicated advisory-lock connection to serialize startup.
func Migrate(ctx context.Context, pool *pgxpool.Pool, opts ...MigrateOption) error {
	cfg := migrateConfig{runtimeRole: defaultRuntimeRole}
	for _, o := range opts {
		o(&cfg)
	}
	// The role name is spliced into SQL as an identifier — validate even though config.Load already did (defense in depth for other callers).
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
		// Explicit unlock — a session lock is not dropped when the connection returns to the pool. WithoutCancel so a cancelled ctx still releases the lock.
		_, _ = conn.Exec(context.WithoutCancel(ctx), `select pg_advisory_unlock($1, $2)`, lockClassMigrate, 0)
	}()

	// Drop temporary objects and pin migration search_path to public so role settings or shadow relations cannot redirect metadata operations.
	if _, err := conn.Exec(ctx, `discard temp`); err != nil {
		return fmt.Errorf("clear migration temporary schema: %w", err)
	}
	if _, err := conn.Exec(ctx, `select set_config('search_path', 'public', false)`); err != nil {
		return fmt.Errorf("set migration search path: %w", err)
	}

	// Preflight: a PRE-EXISTING runtime role must be attribute-safe BEFORE any migration grants it CONNECT + DML — otherwise a role holding e.g. CREATEDB would end up privileged even though boot then fails.
	if err := checkRuntimeRoleAttributes(ctx, conn, cfg.runtimeRole); err != nil {
		return err
	}

	// Migrate as the schema owner so revokes take effect and default privileges bind to the role creating future tables (ADR-0009).
	owner, err := resolveMigrationOwner(ctx, conn)
	if err != nil {
		return err
	}

	// A legacy schema_migrations created by a previous member-migrator must be re-owned BEFORE SET ROLE (the login can: it owns the table and can SET into the new owner), or the owner-run version reads/inserts below would be denied on it.
	if err := normalizeHistoryOwnership(ctx, conn, owner); err != nil {
		return err
	}

	if !owner.isCurrentUser {
		// set_config('role', ...) is SET ROLE with the target as a VALUE (no identifier splicing for an arbitrary owner name).
		if _, err := conn.Exec(ctx, `select set_config('role', $1, false)`, owner.name); err != nil {
			return safeErrorf("assume schema owner role %q for migrations: %v (ADR-0009)", owner.name, err)
		}
		defer func() {
			// pgxpool does not reset session state on Release: a failed RESET ROLE would leak an owner-privileged session back into the pool, so the connection is destroyed instead. Registered after the unlock defer, so it runs first (LIFO) while the session is still usable.
			rctx := context.WithoutCancel(ctx)
			if _, err := conn.Exec(rctx, `reset role`); err != nil {
				_ = conn.Conn().Close(rctx)
			}
		}()
	}

	// Use the same dedicated connection for locking and migration work to prevent pool starvation; create history after SET ROLE.
	if _, err := conn.Exec(ctx, `create table if not exists public.schema_migrations (
		version text primary key,
		applied_at timestamptz not null default now())`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	// Pass the runtime role through a session GUC and quote it with format(%I), avoiding text substitution and cross-install grants.
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

	// Verify effective runtime privileges on every boot to detect role renames and grant drift (ADR-0009).
	return verifyRuntimeRole(ctx, conn, cfg.runtimeRole)
}

// migrationOwner is the resolved role migrations must run as.
type migrationOwner struct {
	name string // role name, used as a set_config VALUE
	// ident is the pre-quoted identifier (quote_ident) for the one statement that needs it spliced (ALTER TABLE ... OWNER TO).
	ident         string
	isCurrentUser bool
}

// resolveMigrationOwner resolves pg_database_owner to the database owner and requires SET ROLE capability plus database revoke rights (ADR-0009).
func resolveMigrationOwner(ctx context.Context, q rowQuerier) (migrationOwner, error) {
	var (
		user, name, ident               string
		isOwner, canSet, dbOK, schemaOK bool
	)
	if err := q.QueryRow(ctx, `
		select current_user,
		       o.rolname,
		       quote_ident(o.rolname),
		       current_user = o.rolname,
		       pg_has_role(current_user, o.oid, 'SET'),
		       (o.oid = d.datdba or pg_has_role(o.oid, d.datdba, 'USAGE')),
		       (n.nspowner = 'pg_database_owner'::regrole
		        or o.oid = n.nspowner
		        or pg_has_role(o.oid, n.nspowner, 'USAGE'))
		from pg_database d
		cross join pg_namespace n
		join pg_roles o
		  on o.oid = case when n.nspowner = 'pg_database_owner'::regrole
		                  then d.datdba else n.nspowner end
		where d.datname = current_database() and n.nspname = 'public'`,
	).Scan(&user, &name, &ident, &isOwner, &canSet, &dbOK, &schemaOK); err != nil {
		return migrationOwner{}, fmt.Errorf("resolve migration owner: %w", err)
	}
	if !isOwner && !canSet {
		return migrationOwner{}, safeErrorf("migration user %q is not schema owner %q and cannot SET ROLE into it — run migrations as the owner, or GRANT %s TO %s WITH SET TRUE; migrations must run AS the owner so REVOKEs apply and default privileges bind to it (ADR-0009)", user, name, name, user)
	}
	if !dbOK || !schemaOK {
		return migrationOwner{}, safeErrorf("schema owner %q cannot act for the database owner (database=%t, schema=%t) — the boundary migrations REVOKE database- and schema-level privileges, which would silently no-op (ADR-0009); align the database and schema public owners", name, dbOK, schemaOK)
	}
	return migrationOwner{name: name, ident: ident, isCurrentUser: isOwner}, nil
}

// normalizeHistoryOwnership re-owns a legacy schema_migrations created by a previous non-owner migrator. It runs as the LOGIN user (before SET ROLE): that user either owns the table (the legacy scenario, and it can SET into the new owner — just verified) or the table already belongs to the owner.
func normalizeHistoryOwnership(ctx context.Context, conn *pgxpool.Conn, owner migrationOwner) error {
	// Pure catalog read (pg_class/pg_namespace) rather than to_regclass: name resolution would demand schema USAGE, which a non-inheriting member login does not hold once 0003 revoked PUBLIC's.
	var needsChown bool
	if err := conn.QueryRow(ctx, `
		select coalesce(
		    (select c.relowner <> $1::regrole
		     from pg_class c
		     join pg_namespace n on n.oid = c.relnamespace
		     where n.nspname = 'public' and c.relname = 'schema_migrations'),
		    false)`, owner.name,
	).Scan(&needsChown); err != nil {
		return fmt.Errorf("inspect schema_migrations ownership: %w", err)
	}
	if !needsChown {
		return nil
	}
	if _, err := conn.Exec(ctx, `alter table public.schema_migrations owner to `+owner.ident); err != nil {
		return safeErrorf("schema_migrations is owned by a previous migrator and could not be re-owned to %q: %v — run ALTER TABLE public.schema_migrations OWNER TO %s as its current owner (ADR-0009)", owner.name, err, owner.ident)
	}
	return nil
}

// checkRuntimeRoleAttributes fails if the role exists with cluster attributes a least-privilege runtime role must never hold: members can SET ROLE into the role (allowed by default), which would hand them those powers. LOGIN is deliberately allowed — a deployment may use the role directly as its login user.
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

// verifyRuntimeRole checks role attributes, required grants, append-only audit access, and owner-only migration history. Every post-migration violation is fatal.
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
	if err := m.verdict(fmt.Sprintf("runtime role %q", role),
		" — was PORTCULLIS_RUNTIME_ROLE changed after the first migration? Follow the rotation procedure in ADR-0009"); err != nil {
		return err
	}
	missing, forbidden, err := verifyTablePrivileges(ctx, conn, role, fmt.Sprintf("runtime role %q", role))
	if err != nil {
		return fmt.Errorf("verify runtime role %q table privileges: %w", role, err)
	}
	if missing != nil {
		return missing
	}
	return forbidden
}

// isUndefinedObject reports whether err is PostgreSQL's undefined_object (42704) — has_*_privilege raises it for a role name that does not exist.
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
