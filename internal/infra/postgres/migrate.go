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

	// Every step runs on the lock-holding connection, so a flood of concurrent
	// callers can't starve the pool: each caller holds exactly one connection
	// (blocked on the lock) and the winner does all its work on that same one.
	if _, err := conn.Exec(ctx, `create table if not exists schema_migrations (
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

	names, err := migrationNames()
	if err != nil {
		return err
	}

	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		var applied bool
		if err := conn.QueryRow(ctx,
			`select exists(select 1 from schema_migrations where version = $1)`, version,
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
		// The SQL is written against the default role name so each file stays
		// readable and runnable standalone; a configured name is substituted here.
		sql := string(body)
		if cfg.runtimeRole != defaultRuntimeRole {
			sql = strings.ReplaceAll(sql, defaultRuntimeRole, cfg.runtimeRole)
		}
		if err := applyOne(ctx, conn, version, sql); err != nil {
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

// checkRuntimeRoleAttributes fails if the role exists with cluster attributes a
// least-privilege runtime role must never hold: members can SET ROLE into the
// role (allowed by default), which would hand them those powers. LOGIN is
// deliberately allowed — a deployment may use the role directly as its login user.
func checkRuntimeRoleAttributes(ctx context.Context, q rowQuerier, role string) error {
	var unsafe bool
	err := q.QueryRow(ctx, `
		select rolsuper or rolcreaterole or rolcreatedb or rolbypassrls or rolreplication
		from pg_roles where rolname = $1`, role,
	).Scan(&unsafe)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // not created yet — migration 0003 will create it safely
	}
	if err != nil {
		return fmt.Errorf("inspect runtime role %q: %w", role, err)
	}
	if unsafe {
		return fmt.Errorf("runtime role %q holds a dangerous attribute (SUPERUSER/CREATEROLE/CREATEDB/BYPASSRLS/REPLICATION); refusing to grant it privileges (ADR-0009)", role)
	}
	return nil
}

// queryPrivilegeMatrix snapshots the EFFECTIVE privileges (direct + membership +
// PUBLIC; implicit owner and superuser rights evaluate true) of a role or login
// user against the audit boundary. The forbidden lists include TRIGGER (with it,
// a non-owner could CREATE TRIGGER — e.g. one that blocks every audit insert),
// plus REFERENCES and MAINTAIN, so "append-only" and "owner-only" mean ALL other
// table privileges.
func queryPrivilegeMatrix(ctx context.Context, q rowQuerier, name string) (privMatrix, error) {
	var m privMatrix
	err := q.QueryRow(ctx, `
		select has_database_privilege($1, current_database(), 'CONNECT'),
		       has_table_privilege($1, 'audit_events', 'SELECT'),
		       has_table_privilege($1, 'audit_events', 'INSERT'),
		       has_table_privilege($1, 'audit_events', 'UPDATE, DELETE, TRUNCATE, TRIGGER, REFERENCES, MAINTAIN'),
		       has_table_privilege($1, 'schema_migrations', 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, TRIGGER, REFERENCES, MAINTAIN')`, name,
	).Scan(&m.connect, &m.auditRead, &m.auditAppend, &m.auditMutate, &m.historyAny)
	return m, err
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
		return fmt.Errorf("runtime role %q does not exist — was PORTCULLIS_RUNTIME_ROLE changed after the first migration? Follow the rotation procedure in ADR-0009", role)
	}
	if err != nil {
		return fmt.Errorf("verify runtime role %q: %w", role, err)
	}
	switch {
	case !m.connect || !m.auditRead || !m.auditAppend:
		return fmt.Errorf("runtime role %q lacks its grants (CONNECT=%t, audit SELECT=%t, audit INSERT=%t) — was PORTCULLIS_RUNTIME_ROLE changed after the first migration? Follow the rotation procedure in ADR-0009", role, m.connect, m.auditRead, m.auditAppend)
	case m.auditMutate:
		return fmt.Errorf("runtime role %q holds UPDATE, DELETE, TRUNCATE, TRIGGER, REFERENCES, or MAINTAIN on audit_events; the append-only boundary is broken (ADR-0009)", role)
	case m.historyAny:
		return fmt.Errorf("runtime role %q has access to schema_migrations; migration history must be owner-only (ADR-0009)", role)
	}
	return nil
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
	if _, err := tx.Exec(ctx, `insert into schema_migrations (version) values ($1)`, version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
