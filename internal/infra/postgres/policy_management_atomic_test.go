package postgres_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestPolicyManagementRollsBackMutationSessionsRevisionAndAudit(t *testing.T) {
	for _, failure := range []struct{ name, table, operation, when string }{
		{"audit", "audit_events", "insert", "new.outcome='SUCCEEDED'"},
		{"revision", "organization_authorization_revisions", "update", "true"},
		{"session", "sessions", "update", "true"},
	} {
		for _, operation := range []string{"account", "role", "create"} {
			t.Run(failure.name+"/"+operation, func(t *testing.T) {
				fixture := newBindingManagementFixture(t)
				accounts, roles := policyServices(t, fixture)
				ctx := context.Background()
				grantPolicyRole(t, fixture, fixture.target, fixture.readerRole, "connection", true)
				(administrationEnv{store: pg.NewIdentityStore(fixture.pool)}).openSession(t, identity.UserID(fixture.target))
				if operation == "create" {
					if _, err := fixture.pool.Exec(ctx, `alter table public.organization_memberships drop column role_id`); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := fixture.pool.Exec(ctx, `create function public.refuse_policy_write() returns trigger language plpgsql as $$begin if `+failure.when+` then raise exception 'private storage detail'; end if; return new; end$$; create trigger refuse_policy_write before `+failure.operation+` on public.`+failure.table+` for each row execute function public.refuse_policy_write()`); err != nil {
					t.Fatal(err)
				}
				before := readBindingPolicyCounts(t, fixture)
				var err error
				switch operation {
				case "account":
					_, err = accounts.Disable(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.UserID(fixture.target))
				case "role":
					_, err = roles.Update(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.RoleID(fixture.readerRole), 1, administration.RoleParams{Name: "Rollback", Permissions: []identity.Permission{identity.PermissionConnectionsList}})
				case "create":
					// Creation has no visible sessions; inject its setup-token failure at that boundary instead.
					if failure.name == "session" {
						if _, setupErr := fixture.pool.Exec(ctx, `create trigger refuse_policy_setup before insert on public.password_setup_tokens for each row execute function public.refuse_policy_write()`); setupErr != nil {
							t.Fatal(setupErr)
						}
					}
					_, err = accounts.Create(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.NewPolicyAccount{Email: "rollback-account@example.com", DisplayName: "Rollback"})
				}
				if !errors.Is(err, administration.ErrBindingManagementUnavailable) || err.Error() != administration.ErrBindingManagementUnavailable.Error() || !reflect.DeepEqual(before, readBindingPolicyCounts(t, fixture)) {
					t.Fatalf("partial commit or leaked error: %v", err)
				}
				var status string
				var version, created int
				if err := fixture.pool.QueryRow(ctx, `select (select status from public.users where id=$1),(select version from public.roles where id=$2),(select count(*) from public.users where email='rollback-account@example.com')`, fixture.target, fixture.readerRole).Scan(&status, &version, &created); err != nil || status != "active" || version != 1 || created != 0 {
					t.Fatalf("mutation survived rollback: status=%s version=%d created=%d %v", status, version, created, err)
				}
			})
		}
	}
}

func TestPolicyRoleFanoutLimitRefusesBeforeAnyMutation(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	ctx := context.Background()
	other := insertBindingPolicyMember(t, fixture, "fanout-other@example.com")
	grantPolicyRole(t, fixture, fixture.target, fixture.readerRole, "connection", true)
	grantPolicyRole(t, fixture, other, fixture.readerRole, "organization", false)
	service, err := administration.NewRolePolicyService(pg.NewBindingManagementStore(fixture.pool), pg.NewAuditStore(fixture.pool), administration.RolePolicyConfig{Timeout: 5 * time.Second, MaxSubjects: 1})
	if err != nil {
		t.Fatal(err)
	}
	before := readBindingPolicyCounts(t, fixture)
	_, err = service.Update(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.RoleID(fixture.readerRole), 1, administration.RoleParams{Name: "Too many subjects"})
	if !errors.Is(err, administration.ErrBindingManagementUnavailable) || before != readBindingPolicyCounts(t, fixture) {
		t.Fatalf("fanout overflow: %v", err)
	}
}

func TestPolicyManagementKeepsRuntimeInactive(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	ctx := context.Background()
	settings := fixture.pool.Config().Copy()
	settings.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, `set role portcullis_runtime`)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository, auditor := pg.NewBindingManagementStore(pool), pg.NewAuditStore(pool)
	accounts, err := administration.NewAccountPolicyService(repository, auditor, administration.DefaultBindingConfig())
	if err != nil {
		t.Fatal(err)
	}
	roles, err := administration.NewRolePolicyService(repository, auditor, administration.DefaultRolePolicyConfig())
	if err != nil {
		t.Fatal(err)
	}
	before := readBindingPolicyCounts(t, fixture)
	if _, err := accounts.Disable(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.UserID(fixture.target)); !errors.Is(err, administration.ErrBindingManagementUnavailable) {
		t.Fatalf("runtime account writer: %v", err)
	}
	if _, err := roles.Update(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.RoleID(fixture.readerRole), 1, administration.RoleParams{Name: "Forbidden"}); !errors.Is(err, administration.ErrBindingManagementUnavailable) {
		t.Fatalf("runtime role writer: %v", err)
	}
	if before != readBindingPolicyCounts(t, fixture) {
		t.Fatal("runtime changed prepared policy")
	}
	if err := pg.Migrate(ctx, fixture.pool); err != nil {
		t.Fatal(err)
	}
}
