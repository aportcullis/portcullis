package postgres_test

import (
	"context"
	"errors"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/migrations"
)

// policyFixture layers the policy store over the shared connection fixture.
type policyFixture struct {
	connFixture
	policies *pg.ConnectionPolicyStore
}

func newPolicyFixture(t *testing.T) policyFixture {
	t.Helper()
	f := newConnFixture(t)
	return policyFixture{connFixture: f, policies: pg.NewConnectionPolicyStore(f.pool)}
}

func validNextPolicy(t *testing.T, current connection.Policy, by identity.UserID) connection.Policy {
	t.Helper()
	next, err := connection.NewPolicy(
		current.ConnectionID, current.OrganizationID, current.Version+1,
		connection.ClassRule{Allowed: true, RequiredApprovals: 1},
		connection.ClassRule{Allowed: true, RequiredApprovals: 2},
		connection.ClassRule{Allowed: false, RequiredApprovals: 1},
		connection.Limits{QueryTimeoutSeconds: 60, MaxRows: 500, MaxResultBytes: 1 << 20},
		by, current.CreatedAt.Add(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

// Creating a connection leaves the v1 default policy row and the pointer in
// the SAME transaction — the deferred FK proves the pair at commit (ADR-0015).
func TestConnectionCreateInsertsDefaultPolicy(t *testing.T) {
	f := newPolicyFixture(t)
	ctx := context.Background()

	c := f.newConn(t, unique("Policied"))
	if err := f.store.Create(ctx, c, sealedStub(1), connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := f.policies.GetCurrent(ctx, f.org, c.ID)
	if err != nil {
		t.Fatalf("GetCurrent: %v", err)
	}
	want := connection.DefaultPolicy()
	if got.Version != 1 || got.Read != want.Read || got.Write != want.Write || got.DDL != want.DDL || got.Limits != want.Limits {
		t.Errorf("v1 policy = %+v, want the ADR-0015 defaults", got)
	}
	if got.CreatedBy != c.CreatedBy {
		t.Errorf("CreatedBy = %q, want the connection creator", got.CreatedBy)
	}

	var pointer int64
	if err := f.pool.QueryRow(ctx, `select current_policy_version from connections where id = $1`, string(c.ID)).Scan(&pointer); err != nil {
		t.Fatal(err)
	}
	if pointer != 1 {
		t.Errorf("current_policy_version = %d, want 1", pointer)
	}
}

// The descriptor round-trips environment and description (D8-0c).
func TestConnectionDescriptorEnvironmentRoundTrip(t *testing.T) {
	f := newPolicyFixture(t)
	ctx := context.Background()

	c := f.newConn(t, unique("EnvDesc"))
	c.Environment = connection.EnvironmentProduction
	c.Description = "primary OLTP\nhandle with care"
	if err := f.store.Create(ctx, c, sealedStub(1), connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := f.store.GetByID(ctx, f.org, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Environment != connection.EnvironmentProduction || got.Description != c.Description {
		t.Errorf("descriptor = %q/%q", got.Environment, got.Description)
	}
}

// UpdatePolicy bumps the pointer, appends the immutable version row, and
// commits the audit events — one transaction, observed via the trail.
func TestConnectionPolicyUpdateFlow(t *testing.T) {
	f := newPolicyFixture(t)
	ctx := context.Background()

	c := f.newConn(t, unique("PolicyUpd"))
	if err := f.store.Create(ctx, c, sealedStub(1), connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	current, err := f.policies.GetCurrent(ctx, f.org, c.ID)
	if err != nil {
		t.Fatal(err)
	}

	next := validNextPolicy(t, current, f.user)
	updated, err := f.policies.UpdatePolicy(ctx, next, current.Version,
		connEvent(audit.ActionConnectionPolicyUpdated, c.ID),
		connEvent(audit.ActionConnectionPolicyClassEnabled, c.ID))
	if err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("Version = %d, want 2", updated.Version)
	}

	round, err := f.policies.GetCurrent(ctx, f.org, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if round.Version != 2 || !round.Write.Allowed || round.Limits.MaxRows != 500 {
		t.Errorf("GetCurrent after update = %+v", round)
	}

	// Both events committed with the mutation; the v1 row is untouched.
	var events int
	if err := f.pool.QueryRow(ctx,
		`select count(*) from audit_events where target_id = $1 and action like 'CONNECTION_POLICY%'`,
		string(c.ID)).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 2 {
		t.Errorf("policy audit events = %d, want 2", events)
	}
	var versions int
	if err := f.pool.QueryRow(ctx,
		`select count(*) from connection_policy_versions where connection_id = $1`, string(c.ID)).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 2 {
		t.Errorf("version rows = %d, want 2 (append-only)", versions)
	}

	// Stale expected version → conflict, nothing changed.
	stale := validNextPolicy(t, current, f.user) // still says version 2
	if _, err := f.policies.UpdatePolicy(ctx, stale, current.Version); !errors.Is(err, connection.ErrPolicyConflict) {
		t.Fatalf("stale UpdatePolicy = %v, want ErrPolicyConflict", err)
	}

	// Unknown connection → not found.
	missing := validNextPolicy(t, current, f.user)
	missing.ConnectionID = connection.ConnectionID(uuid.NewString())
	if _, err := f.policies.UpdatePolicy(ctx, missing, 1); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("missing UpdatePolicy = %v, want ErrNotFound", err)
	}
}

// Archive freezes the policy: Get keeps answering (historical snapshot),
// Update fails with ErrArchived.
func TestConnectionPolicyOnArchivedConnection(t *testing.T) {
	f := newPolicyFixture(t)
	ctx := context.Background()

	c := f.newConn(t, unique("PolicyArch"))
	if err := f.store.Create(ctx, c, sealedStub(1), connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Archive(ctx, f.org, c.ID, connEvent(audit.ActionConnectionArchived, c.ID)); err != nil {
		t.Fatal(err)
	}

	current, err := f.policies.GetCurrent(ctx, f.org, c.ID)
	if err != nil {
		t.Fatalf("GetCurrent on archived = %v, want the historical snapshot", err)
	}
	next := validNextPolicy(t, current, f.user)
	if _, err := f.policies.UpdatePolicy(ctx, next, current.Version); !errors.Is(err, connection.ErrArchived) {
		t.Fatalf("UpdatePolicy on archived = %v, want ErrArchived", err)
	}
}

// Cross-org isolation: a caller scoped to another organization cannot read the
// policy (ADR-0004 repository enforcement).
func TestConnectionPolicyCrossOrgIsolation(t *testing.T) {
	f := newPolicyFixture(t)
	ctx := context.Background()

	c := f.newConn(t, unique("PolicyOrg"))
	if err := f.store.Create(ctx, c, sealedStub(1), connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
		t.Fatal(err)
	}
	otherOrg := identity.OrganizationID(uuid.NewString())
	if _, err := f.policies.GetCurrent(ctx, otherOrg, c.ID); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("cross-org GetCurrent = %v, want ErrNotFound", err)
	}
}

// The runtime role is structurally unable to rewrite a policy snapshot: 0011
// revokes UPDATE (DELETE was already gone via 0010) — append-only in the
// schema, not just in code (ADR-0015).
func TestConnectionPolicyVersionsAppendOnlyForRuntime(t *testing.T) {
	f := newPolicyFixture(t)
	ctx := context.Background()

	c := f.newConn(t, unique("PolicyRT"))
	if err := f.store.Create(ctx, c, sealedStub(1), connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
		t.Fatal(err)
	}

	conn, err := f.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `set role portcullis_runtime`); err != nil {
		t.Fatalf("set role: %v", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), `reset role`) //nolint:errcheck // best-effort restore

	_, err = conn.Exec(ctx, `update connection_policy_versions set max_rows = 1 where connection_id = $1`, string(c.ID))
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" { // insufficient_privilege
		t.Fatalf("runtime UPDATE on policy versions = %v, want insufficient_privilege", err)
	}
}

// Migration 0011 backfills a v1 default policy for connections that existed
// BEFORE it — archived included — and pins current_policy_version = 1. The
// pre-0011 state is built by applying 0001..0010 manually (recorded in
// schema_migrations exactly as Migrate would), inserting old-shape rows, then
// running the real Migrate, which applies only 0011.
func TestMigration0011BackfillsExistingConnections(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()

	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Mirror Migrate's session setup: unqualified names resolve to public and
	// the migrations read the runtime role from the session GUC.
	for _, stmt := range []string{
		`select set_config('search_path', 'public', false)`,
		`select set_config('portcullis.runtime_role', 'portcullis_runtime', false)`,
		`create table if not exists public.schema_migrations (
			version text primary key, applied_at timestamptz not null default now())`,
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		if version >= "0011" {
			break
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := conn.Exec(ctx, `insert into public.schema_migrations (version) values ($1)`, version); err != nil {
			t.Fatal(err)
		}
	}

	// Pre-0011 data: the default org, a creator, one active and one archived
	// connection in the old column shape (no environment/description/policy).
	var orgID, userID string
	if err := conn.QueryRow(ctx, `select id::text from organizations limit 1`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx,
		`insert into users (email, display_name) values ($1, 'Backfill Admin') returning id::text`,
		unique("backfill")+"@example.com").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	var activeID, archivedID string
	if err := conn.QueryRow(ctx, `insert into connections
		(id, organization_id, db_type, display_name, host, port, database_name, tls_mode, target_fingerprint,
		 credential_key_version, credential_wrapped_dek, credential_nonce, credential_ciphertext, created_by)
		values (gen_random_uuid(), $1, 'postgresql', $2, 'h', 5432, 'd', 'verify-full', 'fp',
		 1, 'x'::bytea, 'y'::bytea, 'z'::bytea, $3) returning id::text`,
		orgID, unique("pre11-active"), userID).Scan(&activeID); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `insert into connections
		(id, organization_id, db_type, display_name, host, port, database_name, tls_mode, target_fingerprint,
		 created_by, archived_at)
		values (gen_random_uuid(), $1, 'postgresql', $2, 'h', 5432, 'd', 'verify-full', 'fp', $3, now()) returning id::text`,
		orgID, unique("pre11-archived"), userID).Scan(&archivedID); err != nil {
		t.Fatal(err)
	}
	conn.Release()

	// The real migration path applies 0011 (and only 0011 — history says the
	// rest are done).
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, id := range []string{activeID, archivedID} {
		var pointer int64
		if err := pool.QueryRow(ctx, `select current_policy_version from connections where id = $1`, id).Scan(&pointer); err != nil {
			t.Fatal(err)
		}
		if pointer != 1 {
			t.Errorf("current_policy_version(%s) = %d, want 1", id, pointer)
		}
		var readAllowed, writeAllowed bool
		var approvals int
		if err := pool.QueryRow(ctx,
			`select read_allowed, write_allowed, read_required_approvals
			 from connection_policy_versions where connection_id = $1 and version = 1`, id,
		).Scan(&readAllowed, &writeAllowed, &approvals); err != nil {
			t.Fatalf("backfilled policy row missing for %s: %v", id, err)
		}
		if !readAllowed || writeAllowed || approvals != 1 {
			t.Errorf("backfilled policy = read %t / write %t / approvals %d, want defaults", readAllowed, writeAllowed, approvals)
		}
		var env, desc string
		if err := pool.QueryRow(ctx, `select environment, description from connections where id = $1`, id).Scan(&env, &desc); err != nil {
			t.Fatal(err)
		}
		if env != "development" || desc != "" {
			t.Errorf("backfilled descriptor = %q/%q, want development/empty", env, desc)
		}
	}

	// Re-running the whole chain is idempotent: no duplicate policy rows.
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate rerun: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from connection_policy_versions where connection_id = $1`, activeID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("policy rows after rerun = %d, want 1", count)
	}
}
