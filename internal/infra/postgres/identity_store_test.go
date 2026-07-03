package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// testEvent is a minimal valid audit event for exercising the transactional ops
// in tests whose subject is not the audit trail itself.
func testEvent(action audit.Action) audit.Event {
	return audit.Event{ActorType: audit.ActorUser, Action: action, TargetType: audit.TargetTypeUser, Outcome: audit.OutcomeSucceeded}
}

func TestIdentityStore(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	// Permission catalog and system roles are seeded.
	perms, err := store.ListPermissions(ctx)
	if err != nil {
		t.Fatalf("ListPermissions: %v", err)
	}
	if len(perms) == 0 {
		t.Fatal("permission catalog should be seeded")
	}

	org, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("DefaultOrganizationID: %v", err)
	}
	roleID, err := store.BootstrapRoleID(ctx, org)
	if err != nil {
		t.Fatalf("BootstrapRoleID: %v", err)
	}

	// Create the first user with a password and the bootstrap (admin) role.
	const email = "admin@example.com"
	u, err := store.CreateUser(ctx, email, "Admin")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.SetPassword(ctx, u.ID, "phc-hash"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if err := store.AddMembership(ctx, org, u.ID, roleID); err != nil {
		t.Fatalf("AddMembership: %v", err)
	}

	// The bootstrap (admin) role grants the full catalog.
	userPerms, err := store.PermissionsForUser(ctx, org, u.ID)
	if err != nil {
		t.Fatalf("PermissionsForUser: %v", err)
	}
	if len(userPerms) != len(perms) {
		t.Errorf("admin permissions = %d, want full catalog %d", len(userPerms), len(perms))
	}

	if h, err := store.GetPasswordHash(ctx, u.ID); err != nil || h != "phc-hash" {
		t.Errorf("GetPasswordHash = %q, %v", h, err)
	}
	if got, err := store.GetUserByEmail(ctx, email); err != nil || got.ID != u.ID {
		t.Errorf("GetUserByEmail mismatch: %v", err)
	}
	if _, err := store.GetUserByEmail(ctx, "nobody@example.com"); !errors.Is(err, identity.ErrUserNotFound) {
		t.Errorf("want ErrUserNotFound, got %v", err)
	}

	// Sessions round-trip and revoke.
	now := time.Now()
	sess := identity.NewSession("", u.ID, now, 12*time.Hour, 7*24*time.Hour)
	tokenHash := sha256.Sum256([]byte("raw-token"))
	created, err := store.CreateSession(ctx, sess, tokenHash[:])
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if got, err := store.GetSessionByTokenHash(ctx, tokenHash[:]); err != nil || got.ID != created.ID {
		t.Errorf("GetSessionByTokenHash mismatch: %v", err)
	}
	if err := store.RevokeSession(ctx, created.ID, testEvent(audit.ActionAuthLogout)); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	// OIDC link + lookup.
	if err := store.LinkIdentity(ctx, identity.OIDCIdentity{
		UserID: u.ID, Issuer: "https://accounts.google.com", Subject: "sub-123", Email: email,
	}); err != nil {
		t.Fatalf("LinkIdentity: %v", err)
	}
	if got, err := store.FindUserBySubject(ctx, "https://accounts.google.com", "sub-123"); err != nil || got.ID != u.ID {
		t.Errorf("FindUserBySubject mismatch: %v", err)
	}
	if _, err := store.FindUserBySubject(ctx, "https://accounts.google.com", "nope"); !errors.Is(err, identity.ErrNoLinkedAccount) {
		t.Errorf("want ErrNoLinkedAccount, got %v", err)
	}
}

// Concurrent first-run bootstraps must serialize (advisory lock) so exactly one
// admin is created — the rest see ErrAlreadyBootstrapped, never a duplicate or a
// half-created user. Runs on an isolated database since it asserts on the whole
// users table.
func TestBootstrapAdminSerializesConcurrent(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = store.BootstrapAdmin(ctx, "admin@example.com", "Admin", "phc-hash", testEvent(audit.ActionAuthBootstrap))
		}(i)
	}
	wg.Wait()

	var ok, already int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, identity.ErrAlreadyBootstrapped):
			already++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 || already != n-1 {
		t.Errorf("want 1 success + %d already-bootstrapped, got %d + %d", n-1, ok, already)
	}
	if c, err := store.CountUsers(ctx); err != nil || c != 1 {
		t.Errorf("want exactly 1 user, got %d (%v)", c, err)
	}
}

// Concurrent logins (RotateSession) must serialize so exactly one session stays
// active — the per-user advisory lock makes revoke+insert atomic.
func TestRotateSessionLeavesOneActive(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	u, err := store.BootstrapAdmin(ctx, "admin@example.com", "Admin", "phc-hash", testEvent(audit.ActionAuthBootstrap))
	if err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			now := time.Now()
			sess := identity.NewSession("", u.ID, now, 12*time.Hour, 7*24*time.Hour)
			h := sha256.Sum256([]byte{byte(i)})
			if _, err := store.RotateSession(ctx, u.ID, sess, h[:], testEvent(audit.ActionAuthLogin)); err != nil {
				t.Errorf("RotateSession: %v", err)
			}
		}(i)
	}
	wg.Wait()

	var active int
	if err := pool.QueryRow(ctx,
		`select count(*) from sessions where user_id = $1::uuid and revoked_at is null`,
		string(u.ID)).Scan(&active); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if active != 1 {
		t.Errorf("active sessions = %d, want exactly 1", active)
	}
}

// A soft-deleted role must stop granting its permissions even though the
// membership still references it (FKs are RESTRICT, so the row lingers).
func TestPermissionsExcludeSoftDeletedRole(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	org, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("DefaultOrganizationID: %v", err)
	}

	// A throwaway custom role granting exactly one permission.
	var roleID string
	if err := pool.QueryRow(ctx,
		`insert into roles (organization_id, name) values ($1::uuid, 'tmp-role') returning id::text`,
		string(org)).Scan(&roleID); err != nil {
		t.Fatalf("insert role: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`insert into role_permissions (role_id, permission_key) values ($1::uuid, 'users.list')`,
		roleID); err != nil {
		t.Fatalf("grant permission: %v", err)
	}

	u, err := store.CreateUser(ctx, "perm@example.com", "Perm")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.AddMembership(ctx, org, u.ID, identity.RoleID(roleID)); err != nil {
		t.Fatalf("AddMembership: %v", err)
	}

	if perms, err := store.PermissionsForUser(ctx, org, u.ID); err != nil || len(perms) != 1 {
		t.Fatalf("active role should grant 1 permission, got %d (%v)", len(perms), err)
	}

	// Soft-delete the role; the membership row stays but grants nothing.
	if _, err := pool.Exec(ctx, `update roles set deleted_at = now() where id = $1::uuid`, roleID); err != nil {
		t.Fatalf("soft-delete role: %v", err)
	}
	if after, err := store.PermissionsForUser(ctx, org, u.ID); err != nil || len(after) != 0 {
		t.Errorf("soft-deleted role should grant nothing, got %d (%v)", len(after), err)
	}
}

// insertRole creates a role in org granting exactly one permission, returning its id.
func insertRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool, org, name, perm string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`insert into roles (organization_id, name) values ($1::uuid, $2) returning id::text`,
		org, name).Scan(&id); err != nil {
		t.Fatalf("insert role %q: %v", name, err)
	}
	if _, err := pool.Exec(ctx,
		`insert into role_permissions (role_id, permission_key) values ($1::uuid, $2)`, id, perm); err != nil {
		t.Fatalf("grant %q: %v", perm, err)
	}
	return id
}

// Permission resolution is per-organization: a user with different roles in two
// orgs gets only the queried org's permissions, never the cross-org union (ADR-0004).
func TestPermissionsAreOrgScoped(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	orgA, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("DefaultOrganizationID: %v", err)
	}
	var orgB string
	if err := pool.QueryRow(ctx,
		`insert into organizations (slug, name) values ('org-b', 'Org B') returning id::text`).Scan(&orgB); err != nil {
		t.Fatalf("insert org: %v", err)
	}

	roleA := insertRole(t, ctx, pool, string(orgA), "scoped-a", "users.list")
	roleB := insertRole(t, ctx, pool, orgB, "scoped-b", "roles.delete")

	u, err := store.CreateUser(ctx, "multi@example.com", "Multi")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.AddMembership(ctx, orgA, u.ID, identity.RoleID(roleA)); err != nil {
		t.Fatalf("AddMembership A: %v", err)
	}
	if err := store.AddMembership(ctx, identity.OrganizationID(orgB), u.ID, identity.RoleID(roleB)); err != nil {
		t.Fatalf("AddMembership B: %v", err)
	}

	if a, err := store.PermissionsForUser(ctx, orgA, u.ID); err != nil || len(a) != 1 || a[0] != "users.list" {
		t.Errorf("org A perms = %v (%v), want [users.list]", a, err)
	}
	if b, err := store.PermissionsForUser(ctx, identity.OrganizationID(orgB), u.ID); err != nil || len(b) != 1 || b[0] != "roles.delete" {
		t.Errorf("org B perms = %v (%v), want [roles.delete]", b, err)
	}
}
