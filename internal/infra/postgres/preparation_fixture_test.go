package postgres_test

import (
	"context"
	"crypto/sha256"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"io/fs"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migratePreparationFixture keeps superseded/preparation scenarios on their historical schema; it is never a runtime migration option.
func migratePreparationFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `create table if not exists public.schema_migrations (version text primary key,applied_at timestamptz not null default now(),checksum bytea)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `alter table public.schema_migrations add column if not exists checksum bytea`); err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") || name > "0024_binding_result_ancestry_index.sql" {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(body)
		var recorded bool
		if err := pool.QueryRow(ctx, `select exists(select 1 from public.schema_migrations where version=$1)`, strings.TrimSuffix(name, ".sql")).Scan(&recorded); err != nil {
			t.Fatal(err)
		}
		if recorded {
			if _, err := pool.Exec(ctx, `update public.schema_migrations set checksum=$2 where version=$1 and checksum is null`, strings.TrimSuffix(name, ".sql"), digest[:]); err != nil {
				t.Fatal(err)
			}
			continue
		}
		transaction, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = transaction.Exec(ctx, string(body)); err != nil {
			_ = transaction.Rollback(ctx)
			t.Fatalf("preparation migration %s: %v", name, err)
		}
		if _, err = transaction.Exec(ctx, `insert into public.schema_migrations(version,checksum) values($1,$2)`, strings.TrimSuffix(name, ".sql"), digest[:]); err != nil {
			_ = transaction.Rollback(ctx)
			t.Fatal(err)
		}
		if err = transaction.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// preparationPermissions reads only owner-created historical fixtures, never active application authority.
func preparationPermissions(ctx context.Context, pool *pgxpool.Pool, organization identity.OrganizationID, user identity.UserID) ([]identity.Permission, error) {
	rows, err := pool.Query(ctx, `select distinct permission.permission_key from public.organization_memberships member join public.roles role on role.id=member.role_id and role.deleted_at is null join public.role_permissions permission on permission.role_id=role.id and permission.deleted_at is null where member.organization_id=$1 and member.user_id=$2 order by permission.permission_key`, organization, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var permissions []identity.Permission
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return nil, err
		}
		permissions = append(permissions, identity.Permission(permission))
	}
	return permissions, rows.Err()
}
