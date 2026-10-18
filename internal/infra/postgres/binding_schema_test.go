package postgres_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// bindingSchemaFixture holds organization-pinned references for persistence scenarios.
type bindingSchemaFixture struct {
	pool                     *pgxpool.Pool
	organization, user, role string
	connection               string
	foreignOrganization      string
	foreignUser, foreignRole string
	foreignConnection        string
}

// newBindingSchemaFixture prepares two isolated organizations and archived connection descriptors.
func newBindingSchemaFixture(t *testing.T) bindingSchemaFixture {
	t.Helper()
	environment := newAdministrationEnv(t)
	fixture := bindingSchemaFixture{pool: environment.pool, organization: string(environment.org), user: string(environment.admin.ID), role: string(environment.roles["admin"])}
	ctx := context.Background()
	if err := fixture.pool.QueryRow(ctx, `insert into public.organizations (name, slug) values ('Other', 'other') returning id`).Scan(&fixture.foreignOrganization); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `insert into public.users (email) values ('other@example.com') returning id`).Scan(&fixture.foreignUser); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `insert into public.roles (organization_id, name) values ($1, 'Other role') returning id`, fixture.foreignOrganization).Scan(&fixture.foreignRole); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `insert into public.organization_memberships (organization_id, user_id, role_id) values ($1, $2, $3)`, fixture.foreignOrganization, fixture.foreignUser, fixture.foreignRole); err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range []struct {
		organization, user string
		out                *string
	}{
		{fixture.organization, fixture.user, &fixture.connection},
		{fixture.foreignOrganization, fixture.foreignUser, &fixture.foreignConnection},
	} {
		if err := fixture.pool.QueryRow(ctx, `with descriptor as (
			insert into public.connections
			(id, organization_id, db_type, display_name, host, port, database_name, tls_mode, target_fingerprint, created_by, archived_at, current_policy_version)
			values (gen_random_uuid(), $1, 'postgresql', 'Archived target', 'localhost', 5432, 'test', 'disable', 'test', $2, clock_timestamp(), 1)
			returning id, organization_id, created_by)
			insert into public.connection_policy_versions
			(connection_id, organization_id, version, read_allowed, write_allowed, ddl_allowed, read_required_approvals, write_required_approvals, ddl_required_approvals, query_timeout_seconds, max_rows, max_result_bytes, created_by)
			select id, organization_id, 1, true, false, false, 1, 1, 1, 30, 10000, 4096, created_by from descriptor returning connection_id`, descriptor.organization, descriptor.user).Scan(descriptor.out); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

// insertSchemaBinding inserts a binding and its first immutable version in one transaction.
func insertSchemaBinding(ctx context.Context, transaction pgx.Tx, fixture bindingSchemaFixture, scope string, connection *string, expression *string) (string, error) {
	var binding string
	if err := transaction.QueryRow(ctx, `insert into public.role_bindings (organization_id, user_id, current_version, created_at)
		values ($1, $2, 1, clock_timestamp()) returning id`, fixture.organization, fixture.user).Scan(&binding); err != nil {
		return "", err
	}
	_, err := transaction.Exec(ctx, `insert into public.role_binding_versions
		(binding_id, organization_id, user_id, version, role_id, scope_type, connection_id, condition_title, condition_expression, condition_language_version, condition_environment_version, created_at)
		values ($1, $2, $7, 1, $3, $4, $5,
			case when $6::text is null then null else 'Temporary access' end, $6,
			case when $6::text is null then null else 'cel-v1' end,
			case when $6::text is null then null else 'portcullis-v1' end, clock_timestamp())`, binding, fixture.organization, fixture.role, scope, connection, expression, fixture.user)
	return binding, err
}

// requireSchemaBinding commits a complete owner-created binding fixture.
func requireSchemaBinding(t *testing.T, fixture bindingSchemaFixture, scope string, connection, expression *string) string {
	t.Helper()
	ctx := context.Background()
	transaction, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	binding, err := insertSchemaBinding(ctx, transaction, fixture, scope, connection, expression)
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestBindingSchemaPreparationPreservesLegacyGrants(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.FreshPostgres(t)
	applyLegacyHistoryBefore(t, pool, "0023")
	store := pg.NewIdentityStore(pool)
	admin, err := store.BootstrapAdmin(ctx, "admin@example.com", "Admin", "phc", nil, testEvent(audit.ActionAuthBootstrap))
	if err != nil {
		t.Fatal(err)
	}
	organization, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.PermissionsForUser(ctx, organization, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeated preparation: %v", err)
	}
	after, err := store.PermissionsForUser(ctx, organization, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(before)
	slices.Sort(after)
	if !slices.Equal(before, after) {
		t.Fatalf("schema preparation changed effective grants: before=%v after=%v", before, after)
	}
	var bindings, revisions int
	if err := pool.QueryRow(ctx, `select (select count(*) from public.role_bindings), (select count(*) from public.organization_authorization_revisions)`).Scan(&bindings, &revisions); err != nil {
		t.Fatal(err)
	}
	if bindings != 0 || revisions != 0 {
		t.Fatalf("preparation prematurely activated grants or revisions: bindings=%d revisions=%d", bindings, revisions)
	}
	requireRecordedChecksums(t, pool)
}

func TestBindingSchemaStoresIndependentScopesAndConditionHistory(t *testing.T) {
	fixture := newBindingSchemaFixture(t)
	ctx := context.Background()
	t.Run("organization grant has an explicitly absent condition", func(t *testing.T) {
		binding := requireSchemaBinding(t, fixture, "organization", nil, nil)
		var unconditional bool
		if err := fixture.pool.QueryRow(ctx, `select scope_type = 'organization' and connection_id is null and condition_expression is null
			from public.role_binding_versions where organization_id = $1 and binding_id = $2`, fixture.organization, binding).Scan(&unconditional); err != nil || !unconditional {
			t.Fatalf("organization binding: unconditional=%t error=%v", unconditional, err)
		}
	})
	t.Run("one member may hold multiple connection grants", func(t *testing.T) {
		first := requireSchemaBinding(t, fixture, "connection", &fixture.connection, nil)
		second := requireSchemaBinding(t, fixture, "connection", &fixture.connection, nil)
		if first == second {
			t.Fatal("separate bindings reused an identity")
		}
	})
	t.Run("condition source and versions survive a later unconditional revision", func(t *testing.T) {
		expression := `request.time < timestamp("2026-12-01T00:00:00Z")`
		binding := requireSchemaBinding(t, fixture, "connection", &fixture.connection, &expression)
		transaction, err := fixture.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = transaction.Rollback(ctx) }()
		if _, err := transaction.Exec(ctx, `insert into public.role_binding_versions (binding_id, organization_id, user_id, version, role_id, scope_type, created_at)
			values ($1, $2, $4, 2, $3, 'organization', clock_timestamp());`, binding, fixture.organization, fixture.role, fixture.user); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(ctx, `update public.role_bindings set current_version = 2 where organization_id = $1 and id = $2 and current_version = 1`, fixture.organization, binding); err != nil {
			t.Fatal(err)
		}
		if err := transaction.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		var source, language, environment string
		if err := fixture.pool.QueryRow(ctx, `select condition_expression, condition_language_version, condition_environment_version
			from public.role_binding_versions where organization_id = $1 and binding_id = $2 and version = 1`, fixture.organization, binding).Scan(&source, &language, &environment); err != nil {
			t.Fatal(err)
		}
		if source != expression || language != "cel-v1" || environment != "portcullis-v1" {
			t.Fatalf("original condition changed: source=%q language=%q environment=%q", source, language, environment)
		}
	})
	t.Run("revocation appends a version and retains prior grants", func(t *testing.T) {
		binding := requireSchemaBinding(t, fixture, "organization", nil, nil)
		transaction, err := fixture.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = transaction.Rollback(ctx) }()
		if _, err := transaction.Exec(ctx, `insert into public.role_binding_versions (binding_id, organization_id, user_id, version, role_id, scope_type, created_at, revoked_at)
			select binding_id, organization_id, user_id, 2, role_id, scope_type, clock_timestamp(), clock_timestamp()
			from public.role_binding_versions where organization_id = $1 and binding_id = $2 and version = 1`, fixture.organization, binding); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(ctx, `update public.role_bindings set current_version = 2 where organization_id = $1 and id = $2`, fixture.organization, binding); err != nil {
			t.Fatal(err)
		}
		if err := transaction.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		var retained bool
		if err := fixture.pool.QueryRow(ctx, `select count(*) = 2 and count(revoked_at) = 1 from public.role_binding_versions
			where organization_id = $1 and binding_id = $2`, fixture.organization, binding).Scan(&retained); err != nil || !retained {
			t.Fatalf("revocation history: retained=%t error=%v", retained, err)
		}
	})
}

func TestBindingSchemaRejectsInvalidReferencesAndShapes(t *testing.T) {
	fixture := newBindingSchemaFixture(t)
	ctx := context.Background()
	for _, scenario := range []struct {
		name  string
		alter func(*bindingSchemaFixture, *string, **string, **string)
		code  string
	}{
		{"foreign membership", func(value *bindingSchemaFixture, _ *string, _ **string, _ **string) { value.user = value.foreignUser }, "23503"},
		{"foreign role", func(value *bindingSchemaFixture, _ *string, _ **string, _ **string) { value.role = value.foreignRole }, "23503"},
		{"foreign connection", func(value *bindingSchemaFixture, _ *string, connection **string, _ **string) {
			*connection = &value.foreignConnection
		}, "23503"},
		{"unknown scope", func(_ *bindingSchemaFixture, scope *string, _ **string, _ **string) { *scope = "database" }, "23514"},
		{"connection scope without connection", func(_ *bindingSchemaFixture, _ *string, connection **string, _ **string) { *connection = nil }, "23514"},
		{"organization scope with connection", func(_ *bindingSchemaFixture, scope *string, _ **string, _ **string) { *scope = "organization" }, "23514"},
		{"empty condition is not unconditional", func(_ *bindingSchemaFixture, _ *string, _ **string, expression **string) {
			source := "  "
			*expression = &source
		}, "23514"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			candidate := fixture
			scope := "connection"
			connection := &candidate.connection
			var expression *string
			scenario.alter(&candidate, &scope, &connection, &expression)
			transaction, err := fixture.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = transaction.Rollback(ctx) }()
			_, err = insertSchemaBinding(ctx, transaction, candidate, scope, connection, expression)
			if err == nil {
				err = transaction.Commit(ctx)
			}
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != scenario.code {
				t.Fatalf("invalid binding: error=%v, want SQLSTATE %s", err, scenario.code)
			}
		})
	}
	t.Run("head cannot commit without its exact immutable version", func(t *testing.T) {
		_, err := fixture.pool.Exec(ctx, `insert into public.role_bindings (organization_id, user_id, current_version, created_at)
			values ($1, $2, 1, clock_timestamp())`, fixture.organization, fixture.user)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
			t.Fatalf("orphan head: %v", err)
		}
	})
	t.Run("condition metadata cannot survive without its expression", func(t *testing.T) {
		binding := requireSchemaBinding(t, fixture, "organization", nil, nil)
		_, err := fixture.pool.Exec(ctx, `insert into public.role_binding_versions
			(binding_id, organization_id, user_id, version, role_id, scope_type, condition_title, created_at)
			values ($1, $2, $4, 2, $3, 'organization', 'Incomplete condition', clock_timestamp())`, binding, fixture.organization, fixture.role, fixture.user)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
			t.Fatalf("partial condition: %v", err)
		}
	})
	t.Run("version identity cannot be reused to replace condition history", func(t *testing.T) {
		binding := requireSchemaBinding(t, fixture, "organization", nil, nil)
		_, err := fixture.pool.Exec(ctx, `insert into public.role_binding_versions
			(binding_id, organization_id, user_id, version, role_id, scope_type, created_at)
			values ($1, $2, $4, 1, $3, 'organization', clock_timestamp())`, binding, fixture.organization, fixture.role, fixture.user)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23505" {
			t.Fatalf("duplicate binding version: %v", err)
		}
	})
	t.Run("reassigning a head cannot rewrite the subject of its history", func(t *testing.T) {
		binding := requireSchemaBinding(t, fixture, "organization", nil, nil)
		if _, err := fixture.pool.Exec(ctx, `insert into public.organization_memberships (organization_id, user_id, role_id)
			values ($1, $2, $3)`, fixture.organization, fixture.foreignUser, fixture.role); err != nil {
			t.Fatal(err)
		}
		_, err := fixture.pool.Exec(ctx, `update public.role_bindings set user_id = $1 where organization_id = $2 and id = $3`, fixture.foreignUser, fixture.organization, binding)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
			t.Fatalf("historical subject reassignment: %v", err)
		}
	})
	t.Run("authorization revision must be positive", func(t *testing.T) {
		_, err := fixture.pool.Exec(ctx, `insert into public.organization_authorization_revisions (organization_id, revision) values ($1, 0)`, fixture.organization)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
			t.Fatalf("zero authorization revision: %v", err)
		}
	})
}

func TestBindingSchemaPreparationKeepsRuntimeReadOnly(t *testing.T) {
	fixture := newBindingSchemaFixture(t)
	ctx := context.Background()
	binding := requireSchemaBinding(t, fixture, "organization", nil, nil)
	connection, err := fixture.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	defer func() { _, _ = connection.Exec(ctx, `reset role`) }()
	if _, err := connection.Exec(ctx, `set role portcullis_runtime`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"role_bindings", "role_binding_versions", "organization_authorization_revisions"} {
		if _, err := connection.Exec(ctx, `select * from public.`+table+` where organization_id = $1`, fixture.organization); err != nil {
			t.Errorf("runtime read %s: %v", table, err)
		}
		for _, statement := range []string{
			`delete from public.` + table + ` where organization_id = $1`,
			`update public.` + table + ` set organization_id = organization_id where organization_id = $1`,
			`insert into public.` + table + ` (organization_id) values ($1)`,
		} {
			_, err := connection.Exec(ctx, statement, fixture.organization)
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != "42501" {
				t.Errorf("runtime must not write prepared schema %s: %v", table, err)
			}
		}
	}
	if _, err := connection.Exec(ctx, `reset role`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `grant update on public.role_binding_versions to portcullis_runtime`); err != nil {
		t.Fatal(err)
	}
	err = pg.Migrate(ctx, fixture.pool)
	if err == nil || !strings.Contains(err.Error(), "role_binding_versions") {
		t.Fatalf("boot must refuse immutable history privilege drift for binding %s: %v", binding, err)
	}
}
