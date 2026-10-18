package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestBindingActivationPreservesCurrentAssignmentsAndRetiresLegacyAuthority(t *testing.T) {
	for _, scenario := range []string{"seeded administrator", "custom member", "disabled member", "assignment changed after preparation"} {
		t.Run(scenario, func(t *testing.T) {
			environment := newAdministrationEnv(t)
			ctx := context.Background()
			userID := environment.admin.ID
			if scenario != "seeded administrator" {
				userID = environment.createMemberWithRole(t, "activation@example.com", environment.roles["requester"]).User.ID
			}
			if scenario == "disabled member" {
				if _, err := environment.pool.Exec(ctx, `update public.users set status='disabled' where id=$1`, userID); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "assignment changed after preparation" {
				if _, err := environment.pool.Exec(ctx, `update public.organization_memberships set role_id=$2 where user_id=$1`, userID, environment.roles["approver"]); err != nil {
					t.Fatal(err)
				}
			}
			var assigned string
			if err := environment.pool.QueryRow(ctx, `select role_id::text from public.organization_memberships where organization_id=$1 and user_id=$2`, environment.org, userID).Scan(&assigned); err != nil {
				t.Fatal(err)
			}
			if err := pg.Migrate(ctx, environment.pool); err != nil {
				t.Fatal(err)
			}
			var retired bool
			if err := environment.pool.QueryRow(ctx, `select not exists(select 1 from public.organization_memberships where role_id is not null)`).Scan(&retired); err != nil || !retired {
				t.Fatalf("legacy authority remains: %v %v", retired, err)
			}
			var bindings int
			if err := environment.pool.QueryRow(ctx, `select count(*) from public.role_bindings binding join public.role_binding_versions version on version.binding_id=binding.id and version.version=binding.current_version where binding.organization_id=$1 and binding.user_id=$2 and version.role_id=$3 and version.scope_type='organization' and version.condition_expression is null and version.revoked_at is null`, environment.org, userID, assigned).Scan(&bindings); err != nil || bindings != 1 {
				t.Fatalf("backfill=%d %v", bindings, err)
			}
			if _, err := environment.pool.Exec(ctx, `update public.organization_memberships set role_id=$3 where organization_id=$1 and user_id=$2`, environment.org, userID, assigned); err == nil {
				t.Fatal("retired role assignment accepted a write")
			}
		})
	}
}

func TestBindingActivationRefusesMixedOrUnrecoverablePreparationAtomically(t *testing.T) {
	for _, scenario := range []string{"disabled recovery administrator", "deleted recovery role", "custom role cannot acquire new management rights", "nonempty prepared bindings"} {
		t.Run(scenario, func(t *testing.T) {
			environment := newAdministrationEnv(t)
			ctx := context.Background()
			switch scenario {
			case "disabled recovery administrator":
				if _, err := environment.pool.Exec(ctx, `update public.users set status='disabled' where id=$1`, environment.admin.ID); err != nil {
					t.Fatal(err)
				}
			case "deleted recovery role":
				if _, err := environment.pool.Exec(ctx, `update public.roles set deleted_at=clock_timestamp() where id=$1`, environment.roles["admin"]); err != nil {
					t.Fatal(err)
				}
			case "custom role cannot acquire new management rights":
				var role string
				if err := environment.pool.QueryRow(ctx, `insert into public.roles(organization_id,name) values($1,'Custom recovery') returning id`, environment.org).Scan(&role); err != nil {
					t.Fatal(err)
				}
				if _, err := environment.pool.Exec(ctx, `insert into public.role_permissions(role_id,permission_key) select $1,key from public.permissions`, role); err != nil {
					t.Fatal(err)
				}
				if _, err := environment.pool.Exec(ctx, `update public.organization_memberships set role_id=$1 where user_id=$2`, role, environment.admin.ID); err != nil {
					t.Fatal(err)
				}
			case "nonempty prepared bindings":
				if _, err := environment.pool.Exec(ctx, `insert into public.organization_authorization_revisions(organization_id,revision) values($1,1)`, environment.org); err != nil {
					t.Fatal(err)
				}
			}
			if err := pg.Migrate(ctx, environment.pool); err == nil {
				t.Fatal("unsafe activation accepted")
			}
			var legacy bool
			if err := environment.pool.QueryRow(ctx, `select exists(select 1 from information_schema.columns where table_schema='public' and table_name='organization_memberships' and column_name='role_id')`).Scan(&legacy); err != nil || !legacy {
				t.Fatalf("failed activation did not roll back: %v %v", legacy, err)
			}
			var added int
			if err := environment.pool.QueryRow(ctx, `select count(*) from public.permissions where key like 'bindings.%'`).Scan(&added); err != nil || added != 0 {
				t.Fatalf("partial catalog=%d %v", added, err)
			}
		})
	}
}

func TestBindingActivationAllowsAnEmptyInstallationForAtomicBootstrap(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := pg.NewIdentityStore(pool)
	catalog, err := store.ListPermissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, permission := range identity.BindingManagementPermissions() {
		found := false
		for _, key := range catalog {
			if key == permission {
				found = true
			}
		}
		if !found {
			t.Fatalf("activation catalog lacks %s", permission)
		}
	}
}

func TestBindingActivationBootstrapCreatesIndependentMembershipAndRecoveryGrant(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := pg.NewIdentityStore(pool)
	user, err := store.BootstrapAdmin(ctx, "activated-admin@example.com", "Activated", "phc", nil, testEvent(audit.ActionAuthBootstrap))
	if err != nil {
		t.Fatal(err)
	}
	var count int
	var retired bool
	if err := pool.QueryRow(ctx, `select (select count(*) from public.role_bindings binding join public.role_binding_versions version on version.binding_id=binding.id and version.version=binding.current_version where binding.user_id=$1 and version.scope_type='organization' and version.condition_expression is null and version.revoked_at is null),(select role_id is null from public.organization_memberships where user_id=$1)`, user.ID).Scan(&count, &retired); err != nil || count != 1 || !retired {
		t.Fatalf("bootstrap grants=%d retired=%v %v", count, retired, err)
	}
	org, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	permissions, err := store.PermissionsForUser(ctx, org, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range identity.SelectRecoveryPermissions(identity.EnforcedPermissions()) {
		found := false
		for _, permission := range permissions {
			if permission == required {
				found = true
			}
		}
		if !found {
			t.Fatalf("bootstrap lacks %s", required)
		}
	}
}

func TestBindingActivationSessionAdmissionRequiresCurrentIndependentMembership(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := pg.NewIdentityStore(pool)
	user, err := store.CreateUser(ctx, "outside@example.com", "Outside")
	if err != nil {
		t.Fatal(err)
	}
	session := identity.NewSession("", user.ID, time.Now(), time.Hour, 2*time.Hour)
	if _, err := store.RotateSession(ctx, user.ID, session, make([]byte, 32), testEvent(audit.ActionAuthLogin)); err == nil {
		t.Fatal("session issued without membership")
	}
}
