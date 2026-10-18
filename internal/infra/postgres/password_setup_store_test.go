package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"hash/fnv"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type passwordSetupFixture struct {
	pool     *pgxpool.Pool
	store    *pg.IdentityStore
	accounts *administration.AccountPolicyService
	org      identity.OrganizationID
	admin    identity.UserID
	member   identity.UserID
	token    string
	digest   []byte
}

// newPasswordSetupFixture issues a genuine management link on the active schema.
func newPasswordSetupFixture(t *testing.T) passwordSetupFixture {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.FreshPostgres(t)
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := pg.NewIdentityStore(pool)
	admin, err := store.BootstrapAdmin(ctx, "owner@example.com", "Owner", "owner-phc", nil, testEvent(audit.ActionAuthBootstrap))
	if err != nil {
		t.Fatal(err)
	}
	org, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := administration.NewAccountPolicyService(pg.NewBindingManagementStore(pool), pg.NewAuditStore(pool), administration.DefaultBindingConfig())
	if err != nil {
		t.Fatal(err)
	}
	created, err := accounts.Create(ctx, org, admin.ID, identity.NewPolicyAccount{Email: "member@example.com", DisplayName: "Member"})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(created.SetupLink.Token))
	return passwordSetupFixture{pool, store, accounts, org, admin.ID, created.Account.User.ID, created.SetupLink.Token, digest[:]}
}

func TestPasswordSetupCompletionPreservesMembershipAndAuthority(t *testing.T) {
	for _, scenario := range []string{"grantless member", "bound member", "existing OIDC method", "revoked sessions"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newPasswordSetupFixture(t)
			ctx := context.Background()
			if scenario == "bound member" {
				var role string
				if err := fixture.pool.QueryRow(ctx, `select id::text from public.roles where organization_id=$1 and name='requester'`, fixture.org).Scan(&role); err != nil {
					t.Fatal(err)
				}
				seedActiveFixtureRole(t, fixture.pool, fixture.org, fixture.member, identity.RoleID(role))
			}
			if scenario == "existing OIDC method" {
				if _, err := fixture.pool.Exec(ctx, `insert into public.oidc_identities(user_id,issuer,subject,email) values($1,'https://accounts.google.com','setup-member','member@example.com')`, fixture.member); err != nil {
					t.Fatal(err)
				}
			}
			(administrationEnv{store: fixture.store}).openSession(t, fixture.member)
			before, err := fixture.store.PermissionsForUser(ctx, fixture.org, fixture.member)
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.store.CompletePasswordSetup(ctx, fixture.digest, "new-phc", testEvent(audit.ActionUserPasswordSet)); err != nil {
				t.Fatal(err)
			}
			after, err := fixture.store.PermissionsForUser(ctx, fixture.org, fixture.member)
			if err != nil || !slices.Equal(before, after) {
				t.Fatal("authority changed", err)
			}
			var password string
			if scenario == "existing OIDC method" {
				var retained bool
				if err := fixture.pool.QueryRow(ctx, `select exists(select 1 from public.oidc_identities where user_id=$1 and issuer='https://accounts.google.com' and subject='setup-member')`, fixture.member).Scan(&retained); err != nil || !retained {
					t.Fatalf("OIDC identity changed: %v", err)
				}
			}
			var consumed bool
			var sessions, events int
			if err := fixture.pool.QueryRow(ctx, `select (select secret from public.auth_methods where user_id=$1 and type='password'),(select consumed_at is not null from public.password_setup_tokens where token_hash=$2),(select count(*) from public.sessions where user_id=$1 and revoked_at is null),(select count(*) from public.audit_events where target_id=$1::text and action='USER_PASSWORD_SET' and outcome='SUCCEEDED')`, fixture.member, fixture.digest).Scan(&password, &consumed, &sessions, &events); err != nil || password != "new-phc" || !consumed || sessions != 0 || events != 1 {
				t.Fatalf("completion %q %v %d %d: %v", password, consumed, sessions, events, err)
			}
		})
	}
}

func TestPasswordSetupRefusesInvalidOrIneligibleTokens(t *testing.T) {
	for _, scenario := range []string{"unknown", "expired", "reused", "revoked", "disabled", "already has password", "membership missing", "replaced", "foreign organization"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newPasswordSetupFixture(t)
			ctx := context.Background()
			switch scenario {
			case "foreign organization":
				if _, err := fixture.pool.Exec(ctx, `with organization as(insert into public.organizations(name,slug) values('Foreign setup','foreign-setup') returning id), member as(insert into public.organization_memberships(organization_id,user_id) select id,$1 from organization returning organization_id) update public.password_setup_tokens set organization_id=member.organization_id from member where user_id=$1`, fixture.member); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				fixture.digest = make([]byte, 32)
			case "expired":
				_, err := fixture.pool.Exec(ctx, `update public.password_setup_tokens set created_at=clock_timestamp()-interval '2 days',expires_at=clock_timestamp()-interval '1 day' where user_id=$1`, fixture.member)
				if err != nil {
					t.Fatal(err)
				}
			case "reused":
				if err := fixture.store.CompletePasswordSetup(ctx, fixture.digest, "first-phc", testEvent(audit.ActionUserPasswordSet)); err != nil {
					t.Fatal(err)
				}
			case "revoked":
				if _, err := fixture.pool.Exec(ctx, `update public.password_setup_tokens set revoked_at=clock_timestamp() where user_id=$1`, fixture.member); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				if _, err := fixture.accounts.Disable(ctx, fixture.org, fixture.admin, fixture.member); err != nil {
					t.Fatal(err)
				}
			case "already has password":
				if err := fixture.store.SetPassword(ctx, fixture.member, "existing-phc"); err != nil {
					t.Fatal(err)
				}
			case "membership missing":
				if _, err := fixture.pool.Exec(ctx, `delete from public.organization_memberships where user_id=$1`, fixture.member); err != nil {
					t.Fatal(err)
				}
			case "replaced":
				if _, err := fixture.accounts.IssueSetupLink(ctx, fixture.org, fixture.admin, fixture.member); err != nil {
					t.Fatal(err)
				}
			}
			if err := fixture.store.CompletePasswordSetup(ctx, fixture.digest, "must-not-win", testEvent(audit.ActionUserPasswordSet)); !errors.Is(err, identity.ErrPasswordSetupInvalid) {
				t.Fatalf("refusal=%v", err)
			}
			var overwritten bool
			if err := fixture.pool.QueryRow(ctx, `select exists(select 1 from public.auth_methods where user_id=$1 and secret='must-not-win')`, fixture.member).Scan(&overwritten); err != nil || overwritten {
				t.Fatal("refusal changed password", err)
			}
		})
	}
}

func TestPasswordSetupConcurrentConsumptionAdmitsExactlyOnce(t *testing.T) {
	fixture := newPasswordSetupFixture(t)
	ctx := context.Background()
	var workers sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		workers.Go(func() {
			results <- fixture.store.CompletePasswordSetup(ctx, fixture.digest, "concurrent-phc", testEvent(audit.ActionUserPasswordSet))
		})
	}
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, identity.ErrPasswordSetupInvalid) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("successes=%d", successes)
	}
}

func TestPasswordSetupAuditFailureLeavesTokenRetryable(t *testing.T) {
	fixture := newPasswordSetupFixture(t)
	ctx := context.Background()
	(administrationEnv{store: fixture.store}).openSession(t, fixture.member)
	if _, err := fixture.pool.Exec(ctx, `create function public.refuse_setup_audit() returns trigger language plpgsql as $$begin raise exception 'setup audit unavailable'; end$$; create trigger refuse_setup_audit before insert on public.audit_events for each row execute function public.refuse_setup_audit()`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.CompletePasswordSetup(ctx, fixture.digest, "rolled-back-phc", testEvent(audit.ActionUserPasswordSet)); err == nil {
		t.Fatal("missing audit accepted")
	}
	var untouched bool
	if err := fixture.pool.QueryRow(ctx, `select not exists(select 1 from public.auth_methods where user_id=$1 and type='password') and exists(select 1 from public.password_setup_tokens where user_id=$1 and consumed_at is null and revoked_at is null) and (select count(*) from public.sessions where user_id=$1 and revoked_at is null)=1`, fixture.member).Scan(&untouched); err != nil || !untouched {
		t.Fatalf("failed audit changed credentials, token or sessions: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `drop trigger refuse_setup_audit on public.audit_events`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.CompletePasswordSetup(ctx, fixture.digest, "retry-phc", testEvent(audit.ActionUserPasswordSet)); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordSetupConsumptionUsesTimeAfterPolicyLock(t *testing.T) {
	for _, scenario := range []string{"valid after wait", "expires during wait"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newPasswordSetupFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if scenario == "expires during wait" {
				if _, err := fixture.pool.Exec(ctx, `update public.password_setup_tokens set expires_at=clock_timestamp()+interval '1 second' where user_id=$1`, fixture.member); err != nil {
					t.Fatal(err)
				}
			}
			blocker, err := fixture.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = blocker.Rollback(context.Background()) }()
			coordinator := fnv.New32a()
			_, _ = coordinator.Write([]byte(fixture.org))
			object := int64(coordinator.Sum32())
			if object > 2147483647 {
				object -= 4294967296
			}
			if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock(5,$1::integer)`, object); err != nil {
				t.Fatal(err)
			}
			configuration := fixture.pool.Config().Copy()
			configuration.ConnConfig.RuntimeParams["application_name"] = "password-setup-lock-wait"
			waiting, err := pgxpool.NewWithConfig(ctx, configuration)
			if err != nil {
				t.Fatal(err)
			}
			defer waiting.Close()
			results := make(chan error, 1)
			go func() {
				results <- pg.NewIdentityStore(waiting).CompletePasswordSetup(ctx, fixture.digest, "post-lock-phc", testEvent(audit.ActionUserPasswordSet))
			}()
			waitBindingPolicyLock(t, ctx, fixture.pool, "password-setup-lock-wait")
			if scenario == "expires during wait" {
				if _, err := blocker.Exec(ctx, `select pg_catalog.pg_sleep(1.1)`); err != nil {
					t.Fatal(err)
				}
			}
			var released time.Time
			if err := blocker.QueryRow(ctx, `select clock_timestamp()`).Scan(&released); err != nil {
				t.Fatal(err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			err = <-results
			if scenario == "expires during wait" {
				if !errors.Is(err, identity.ErrPasswordSetupInvalid) {
					t.Fatalf("expired token admitted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var consistent bool
			if err := fixture.pool.QueryRow(ctx, `select token.consumed_at >= $2 and method.created_at=token.consumed_at and method.updated_at=token.consumed_at and event.occurred_at=token.consumed_at from public.password_setup_tokens token join public.auth_methods method on method.user_id=token.user_id and method.type='password' join public.audit_events event on event.target_id=token.user_id::text and event.action='USER_PASSWORD_SET' where token.user_id=$1`, fixture.member, released).Scan(&consistent); err != nil || !consistent {
				t.Fatalf("pre-lock credential or audit timestamp: %v %v", consistent, err)
			}
		})
	}
}
