package postgres_test

import (
	"context"
	"errors"
	"hash/fnv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestPolicyAccountDisableSerializesWithLoginSessionInsertion(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	settings := fixture.pool.Config().Copy()
	applicationName := "account-login-" + fixture.organization
	settings.ConnConfig.RuntimeParams["application_name"] = applicationName
	pool, err := pgxpool.NewWithConfig(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	accounts, err := administration.NewAccountPolicyService(pg.NewBindingManagementStore(pool), pg.NewAuditStore(pool), administration.DefaultBindingConfig())
	if err != nil {
		t.Fatal(err)
	}
	login, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = login.Rollback(ctx) }()
	// Hold the same class-2 coordinator used by login before inserting its session.
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(fixture.target))
	lockObject := int64(hash.Sum32())
	if lockObject > 2147483647 {
		lockObject -= 4294967296
	}
	if _, err := login.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock(2,$1::integer)`, lockObject); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 1)
	go func() {
		_, err := accounts.Disable(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.UserID(fixture.target))
		results <- err
	}()
	waitBindingPolicyLock(t, ctx, fixture.pool, applicationName)
	if _, err := login.Exec(ctx, `set local lock_timeout='250ms'`); err != nil {
		t.Fatal(err)
	}
	if _, err := login.Exec(ctx, `insert into public.sessions (user_id,token_hash,idle_expires_at,absolute_expires_at) values ($1,decode(repeat('12',32),'hex'),clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 day')`, fixture.target); err != nil {
		t.Fatalf("account reference lock prevents an admitted login from finishing: %v", err)
	}
	if err := login.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-results:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var status string
	var activeSessions int
	if err := fixture.pool.QueryRow(ctx, `select (select status from public.users where id=$1),(select count(*) from public.sessions where user_id=$1 and revoked_at is null)`, fixture.target).Scan(&status, &activeSessions); err != nil || status != "disabled" || activeSessions != 0 {
		t.Fatalf("login session escaped revocation: %s %d %v", status, activeSessions, err)
	}
}

func TestPolicyRoleAndBindingWritersSerializeRecoveryProtection(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	_, roles := policyServices(t, fixture)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	catalog, err := pg.NewIdentityStore(fixture.pool).ListPermissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var rest []identity.Permission
	for _, permission := range identity.SelectRecoveryPermissions(catalog) {
		if permission != identity.PermissionBindingsGet {
			rest = append(rest, permission)
		}
	}
	controller := insertBindingPolicyRole(t, fixture, "Mixed writer controller", []identity.Permission{identity.PermissionRolesUpdate, identity.PermissionBindingsDelete, identity.PermissionBindingsGet})
	appendOwnerBindingVersion(t, fixture, fixture.actorBinding, controller, "organization", nil, nil)
	piece := insertBindingPolicyRole(t, fixture, "Role recovery piece", []identity.Permission{identity.PermissionBindingsGet})
	otherPiece := insertBindingPolicyRole(t, fixture, "Binding recovery piece", []identity.Permission{identity.PermissionBindingsGet})
	restRole := insertBindingPolicyRole(t, fixture, "Mixed recovery rest", rest)
	otherTarget := insertBindingPolicyMember(t, fixture, "mixed-recovery@example.com")
	grantPolicyRole(t, fixture, fixture.target, piece, "organization", false)
	grantPolicyRole(t, fixture, fixture.target, restRole, "organization", false)
	otherBinding := grantPolicyRole(t, fixture, otherTarget, otherPiece, "organization", false)
	grantPolicyRole(t, fixture, otherTarget, restRole, "organization", false)
	start, results := make(chan struct{}), make(chan error, 2)
	go func() {
		<-start
		_, err := roles.Update(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.RoleID(piece), 1, administration.RoleParams{Name: "Removed recovery piece"})
		results <- err
	}()
	go func() {
		<-start
		_, err := fixture.service.Revoke(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.BindingID(otherBinding), 1)
		results <- err
	}()
	close(start)
	var succeeded, protected int
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				succeeded++
			} else if errors.Is(err, identity.ErrLastAdministrator) {
				protected++
			} else {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var revision int
	if err := fixture.pool.QueryRow(ctx, `select revision from public.organization_authorization_revisions where organization_id=$1`, fixture.organization).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || protected != 1 || revision != 2 {
		t.Fatalf("mixed writer recovery: succeeded=%d protected=%d revision=%d", succeeded, protected, revision)
	}
}

func TestPolicyManagementReauthorizesAndCancelsAfterOrganizationLockWait(t *testing.T) {
	for _, operation := range []string{"account", "role", "cancel account", "cancel role"} {
		t.Run(operation, func(t *testing.T) {
			fixture := newBindingManagementFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			settings := fixture.pool.Config().Copy()
			applicationName := "policy-wait-" + fixture.organization
			settings.ConnConfig.RuntimeParams["application_name"] = applicationName
			pool, err := pgxpool.NewWithConfig(ctx, settings)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			store, auditor := pg.NewBindingManagementStore(pool), pg.NewAuditStore(pool)
			accounts, err := administration.NewAccountPolicyService(store, auditor, administration.DefaultBindingConfig())
			if err != nil {
				t.Fatal(err)
			}
			roles, err := administration.NewRolePolicyService(store, auditor, administration.DefaultRolePolicyConfig())
			if err != nil {
				t.Fatal(err)
			}
			results := make(chan error, 1)
			waiting, cancelWaiting := context.WithCancel(ctx)
			defer cancelWaiting()
			err = pg.NewBindingManagementStore(fixture.pool).WithBindingTransaction(ctx, identity.OrganizationID(fixture.organization), func(administration.BindingTransaction) error {
				go func() {
					var err error
					if operation == "account" || operation == "cancel account" {
						_, err = accounts.Disable(waiting, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.UserID(fixture.target))
					} else {
						_, err = roles.Update(waiting, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.RoleID(fixture.readerRole), 1, administration.RoleParams{Name: "Waiting change"})
					}
					results <- err
				}()
				waitBindingPolicyLock(t, ctx, fixture.pool, applicationName)
				if operation == "cancel account" || operation == "cancel role" {
					cancelWaiting()
				} else {
					appendOwnerBindingVersion(t, fixture, fixture.actorBinding, fixture.managerRole, "organization", nil, policyString(`true`))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-results:
				expected := identity.ErrActorNotAuthorized
				if operation == "cancel account" || operation == "cancel role" {
					expected = context.Canceled
				}
				if !errors.Is(err, expected) {
					t.Fatalf("post-wait admission: %v want %v", err, expected)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var status string
			var version, revision int
			if err := fixture.pool.QueryRow(ctx, `select (select status from public.users where id=$1),(select version from public.roles where id=$2),(select revision from public.organization_authorization_revisions where organization_id=$3)`, fixture.target, fixture.readerRole, fixture.organization).Scan(&status, &version, &revision); err != nil || status != "active" || version != 1 || revision != 1 {
				t.Fatalf("refused waiter changed state: %s %d %d %v", status, version, revision, err)
			}
		})
	}
}

func TestPolicyRoleObservesTimeAfterEverySubjectSessionLock(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	grantPolicyRole(t, fixture, fixture.target, fixture.readerRole, "connection", true)
	(administrationEnv{store: pg.NewIdentityStore(fixture.pool)}).openSession(t, identity.UserID(fixture.target))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	settings := fixture.pool.Config().Copy()
	applicationName := "role-session-clock-" + fixture.organization
	settings.ConnConfig.RuntimeParams["application_name"] = applicationName
	pool, err := pgxpool.NewWithConfig(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	roles, err := administration.NewRolePolicyService(pg.NewBindingManagementStore(pool), pg.NewAuditStore(pool), administration.DefaultRolePolicyConfig())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Rollback(ctx) }()
	if _, err := owner.Exec(ctx, `select id from public.sessions where user_id=$1 and revoked_at is null for update`, fixture.target); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 1)
	go func() {
		_, err := roles.Update(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.RoleID(fixture.readerRole), 1, administration.RoleParams{Name: "Fresh timestamp"})
		results <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := fixture.pool.QueryRow(ctx, `select exists(select 1 from pg_stat_activity where application_name=$1 and wait_event_type='Lock' and wait_event in ('transactionid','tuple'))`, applicationName).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var heldAt time.Time
	if err := owner.QueryRow(ctx, `select clock_timestamp()`).Scan(&heldAt); err != nil {
		t.Fatal(err)
	}
	if err := owner.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-results:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var revokedAt, auditedAt time.Time
	if err := fixture.pool.QueryRow(ctx, `select (select revoked_at from public.sessions where user_id=$1 order by revoked_at desc limit 1),(select occurred_at from public.audit_events where target_id=$2 and action='ROLE_UPDATED' and outcome='SUCCEEDED')`, fixture.target, fixture.readerRole).Scan(&revokedAt, &auditedAt); err != nil || !revokedAt.After(heldAt) || !auditedAt.Equal(revokedAt) {
		t.Fatalf("pre-lock evidence: held=%v revoked=%v audited=%v %v", heldAt, revokedAt, auditedAt, err)
	}
}
