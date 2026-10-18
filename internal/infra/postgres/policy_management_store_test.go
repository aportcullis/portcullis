package postgres_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// policyServices injects the actual PostgreSQL transaction and audit adapters.
func policyServices(t *testing.T, fixture bindingManagementFixture) (*administration.AccountPolicyService, *administration.RolePolicyService) {
	t.Helper()
	store := pg.NewBindingManagementStore(fixture.pool)
	auditor := pg.NewAuditStore(fixture.pool)
	settings := administration.BindingConfig{Timeout: 5 * time.Second}
	accounts, err := administration.NewAccountPolicyService(store, auditor, settings)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := administration.NewRolePolicyService(store, auditor, administration.RolePolicyConfig{Timeout: settings.Timeout, MaxSubjects: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return accounts, roles
}

// grantPolicyRole seeds one immutable qualification grant at the requested scope.
func grantPolicyRole(t *testing.T, fixture bindingManagementFixture, user, role, scope string, conditional bool) string {
	t.Helper()
	grant := fixture.bindingSchemaFixture
	grant.user = user
	grant.role = role
	var connection *string
	if scope == "connection" {
		connection = &fixture.connection
	}
	var expression *string
	if conditional {
		expression = policyString(`request.time < timestamp("2000-01-01T00:00:00Z")`)
	}
	return requireSchemaBinding(t, grant, scope, connection, expression)
}

func TestPolicyAccountLifecycleWithoutLegacyRoleAssignment(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	accounts, _ := policyServices(t, fixture)
	ctx := context.Background()
	org := identity.OrganizationID(fixture.organization)
	actor := identity.UserID(fixture.user)
	// Only the disposable qualification schema retires the old authority column.
	if _, err := fixture.pool.Exec(ctx, `alter table public.organization_memberships drop column role_id`); err != nil {
		t.Fatal(err)
	}
	created, err := accounts.Create(ctx, org, actor, identity.NewPolicyAccount{Email: " NEW-ACCOUNT@example.com ", DisplayName: "New account"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Account.User.Email != "new-account@example.com" || created.SetupLink.Token == "" || created.Account.HasPassword {
		t.Fatalf("created: %+v", created)
	}
	if !created.SetupLink.ExpiresAt.Equal(created.Account.User.CreatedAt.Add(24 * time.Hour)) {
		t.Fatalf("setup expiry is not measured from the account mutation: %+v", created)
	}
	user := created.Account.User.ID
	var bindings int
	if err := fixture.pool.QueryRow(ctx, `select count(*) from public.role_bindings where user_id=$1`, string(user)).Scan(&bindings); err != nil || bindings != 0 {
		t.Fatalf("implicit authority: %d %v", bindings, err)
	}
	link, err := accounts.IssueSetupLink(ctx, org, actor, user)
	if err != nil || link.Token == created.SetupLink.Token {
		t.Fatalf("replacement: %+v %v", link, err)
	}
	disabled, err := accounts.Disable(ctx, org, actor, user)
	if err != nil || disabled.User.Status != identity.StatusDisabled {
		t.Fatalf("disable: %+v %v", disabled, err)
	}
	if _, err := accounts.IssueSetupLink(ctx, org, actor, user); !errors.Is(err, identity.ErrUserDisabled) {
		t.Fatalf("disabled setup: %v", err)
	}
	enabled, err := accounts.Enable(ctx, org, actor, user)
	if err != nil || enabled.User.Status != identity.StatusActive {
		t.Fatalf("enable: %+v %v", enabled, err)
	}
	var revision, events, openTokens int
	if err := fixture.pool.QueryRow(ctx, `select (select revision from public.organization_authorization_revisions where organization_id=$1),(select count(*) from public.audit_events where organization_id=$1 and target_id=$2 and outcome='SUCCEEDED'),(select count(*) from public.password_setup_tokens where user_id=$2::uuid and consumed_at is null and revoked_at is null)`, fixture.organization, string(user)).Scan(&revision, &events, &openTokens); err != nil {
		t.Fatal(err)
	}
	if revision != 5 || events != 4 || openTokens != 0 {
		t.Fatalf("atomic evidence: revision=%d events=%d tokens=%d", revision, events, openTokens)
	}
	// A repeated status is a checked no-op, including no revision or audit change.
	if _, err := accounts.Enable(ctx, org, actor, user); err != nil {
		t.Fatal(err)
	}
	var after int
	if err := fixture.pool.QueryRow(ctx, `select revision from public.organization_authorization_revisions where organization_id=$1`, fixture.organization).Scan(&after); err != nil || after != revision {
		t.Fatalf("no-op revision: %d %v", after, err)
	}
}

func TestPolicyAccountsPreserveGrantsAndRevokeSessions(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	accounts, _ := policyServices(t, fixture)
	ctx := context.Background()
	org := identity.OrganizationID(fixture.organization)
	actor := identity.UserID(fixture.user)
	target := identity.UserID(fixture.target)
	grantPolicyRole(t, fixture, fixture.target, fixture.readerRole, "connection", true)
	legacy := pg.NewIdentityStore(fixture.pool)
	before, err := legacy.PermissionsForUser(ctx, org, target)
	if err != nil {
		t.Fatal(err)
	}
	environment := administrationEnv{store: legacy}
	environment.openSession(t, target)
	var headsBefore int
	if err := fixture.pool.QueryRow(ctx, `select count(*) from public.role_binding_versions`).Scan(&headsBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.Disable(ctx, org, actor, target); err != nil {
		t.Fatal(err)
	}
	var sessions int
	if err := fixture.pool.QueryRow(ctx, `select count(*) from public.sessions where user_id=$1 and revoked_at is null`, fixture.target).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("sessions=%d %v", sessions, err)
	}
	if _, err := accounts.Enable(ctx, org, actor, target); err != nil {
		t.Fatal(err)
	}
	after, err := legacy.PermissionsForUser(ctx, org, target)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("legacy grants changed: %v %v %v", before, after, err)
	}
	var headsAfter int
	if err := fixture.pool.QueryRow(ctx, `select count(*) from public.role_binding_versions`).Scan(&headsAfter); err != nil || headsBefore != headsAfter {
		t.Fatalf("account mutation rewrote grants: %d %d %v", headsBefore, headsAfter, err)
	}
}

func TestPolicyAccountRefusesIncompleteDelegation(t *testing.T) {
	for _, scenario := range []string{"conditional management", "connection management", "missing target permission", "self", "foreign target", "disabled actor", "recovery absent", "setup password already set"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newBindingManagementFixture(t)
			accounts, _ := policyServices(t, fixture)
			ctx := context.Background()
			actor := fixture.user
			target := fixture.target
			expected := identity.ErrActorNotAuthorized
			switch scenario {
			case "conditional management", "connection management":
				scope := "organization"
				var connection *string
				expression := policyString(`true`)
				if scenario == "connection management" {
					scope = "connection"
					connection = &fixture.connection
					expression = nil
				}
				appendOwnerBindingVersion(t, fixture, fixture.actorBinding, fixture.managerRole, scope, connection, expression)
			case "missing target permission":
				role := insertBindingPolicyRole(t, fixture, "Account controller", []identity.Permission{identity.PermissionUsersDisable})
				appendOwnerBindingVersion(t, fixture, fixture.actorBinding, role, "organization", nil, nil)
				grantPolicyRole(t, fixture, target, fixture.readerRole, "connection", true)
				expected = identity.ErrPrivilegeEscalation
			case "self":
				target = actor
				expected = identity.ErrSelfAdministration
			case "foreign target":
				target = "00000000-0000-0000-0000-000000000001"
				expected = identity.ErrUserNotFound
			case "disabled actor":
				if _, err := fixture.pool.Exec(ctx, `update public.users set status='disabled' where id=$1`, actor); err != nil {
					t.Fatal(err)
				}
			case "recovery absent":
				role := insertBindingPolicyRole(t, fixture, "Not full recovery", []identity.Permission{identity.PermissionUsersDisable})
				appendOwnerBindingVersion(t, fixture, fixture.actorBinding, role, "organization", nil, nil)
				expected = identity.ErrLastAdministrator
			case "setup password already set":
				if _, err := fixture.pool.Exec(ctx, `insert into public.auth_methods (user_id,type,secret) values ($1,'password','unused')`, target); err != nil {
					t.Fatal(err)
				}
				expected = identity.ErrPasswordAlreadySet
			}
			before := readBindingPolicyCounts(t, fixture)
			var err error
			if scenario == "setup password already set" {
				_, err = accounts.IssueSetupLink(ctx, identity.OrganizationID(fixture.organization), identity.UserID(actor), identity.UserID(target))
			} else {
				_, err = accounts.Disable(ctx, identity.OrganizationID(fixture.organization), identity.UserID(actor), identity.UserID(target))
			}
			if !errors.Is(err, expected) {
				t.Fatalf("refusal: got %v want %v", err, expected)
			}
			after := readBindingPolicyCounts(t, fixture)
			// Refusal audits may append separately; policy versions and revision must remain unchanged.
			if before.heads != after.heads || before.versions != after.versions || before.revision != after.revision {
				t.Fatalf("refusal changed policy: %+v %+v", before, after)
			}
		})
	}
}

func TestPolicyRoleLifecycleAndBindingSessionFanout(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	_, roles := policyServices(t, fixture)
	ctx := context.Background()
	org := identity.OrganizationID(fixture.organization)
	actor := identity.UserID(fixture.user)
	role, err := roles.Create(ctx, org, actor, administration.RoleParams{Name: "Independent empty role"})
	if err != nil {
		t.Fatal(err)
	}
	role, err = roles.Update(ctx, org, actor, role.ID, 1, administration.RoleParams{Name: "Independent reader", Permissions: []identity.Permission{identity.PermissionConnectionsGet}})
	if err != nil {
		t.Fatal(err)
	}
	second := insertBindingPolicyMember(t, fixture, "role-second@example.com")
	firstBinding := grantPolicyRole(t, fixture, fixture.target, string(role.ID), "connection", true)
	secondBinding := grantPolicyRole(t, fixture, second, string(role.ID), "organization", false)
	legacy := pg.NewIdentityStore(fixture.pool)
	environment := administrationEnv{store: legacy}
	for _, user := range []string{fixture.target, second, fixture.user} {
		environment.openSession(t, identity.UserID(user))
	}
	// The legacy assignments intentionally reference a different role.
	var beforeLegacy string
	if err := fixture.pool.QueryRow(ctx, `select role_id::text from public.organization_memberships where user_id=$1`, fixture.target).Scan(&beforeLegacy); err != nil {
		t.Fatal(err)
	}
	role, err = roles.Update(ctx, org, actor, role.ID, 2, administration.RoleParams{Name: "Reader renamed", Permissions: []identity.Permission{identity.PermissionConnectionsList}})
	if err != nil {
		t.Fatal(err)
	}
	var subjectSessions, actorSessions int
	if err := fixture.pool.QueryRow(ctx, `select (select count(*) from public.sessions where user_id=any($1::uuid[]) and revoked_at is null),(select count(*) from public.sessions where user_id=$2 and revoked_at is null)`, []string{fixture.target, second}, fixture.user).Scan(&subjectSessions, &actorSessions); err != nil {
		t.Fatal(err)
	}
	if subjectSessions != 0 || actorSessions != 1 {
		t.Fatalf("fanout: subjects=%d actor=%d", subjectSessions, actorSessions)
	}
	if err := roles.Delete(ctx, org, actor, role.ID, 3); !errors.Is(err, identity.ErrRoleInUse) {
		t.Fatalf("live bindings deletion: %v", err)
	}
	var afterLegacy string
	if err := fixture.pool.QueryRow(ctx, `select role_id::text from public.organization_memberships where user_id=$1`, fixture.target).Scan(&afterLegacy); err != nil || afterLegacy != beforeLegacy {
		t.Fatalf("legacy assignment changed: %s %s %v", beforeLegacy, afterLegacy, err)
	}
	if role.BindingCount != 2 {
		t.Fatalf("binding usage count: %+v", role)
	}
	if _, err := fixture.pool.Exec(ctx, `alter table public.organization_memberships drop column role_id`); err != nil {
		t.Fatal(err)
	}
	for _, binding := range []string{firstBinding, secondBinding} {
		if _, err := fixture.service.Revoke(ctx, org, actor, identity.BindingID(binding), 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := roles.Delete(ctx, org, actor, role.ID, role.Version); err != nil {
		t.Fatalf("historical reference prevents delete: %v", err)
	}
	unused, err := roles.Create(ctx, org, actor, administration.RoleParams{Name: "Unused definition"})
	if err != nil {
		t.Fatal(err)
	}
	if err := roles.Delete(ctx, org, actor, unused.ID, 1); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyRoleRefusesEscalationAndProtectsRecovery(t *testing.T) {
	for _, scenario := range []string{"old permissions", "new permissions", "conditional authority", "connection authority", "stale version", "system role", "foreign role", "last recovery", "live expired binding"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newBindingManagementFixture(t)
			_, roles := policyServices(t, fixture)
			ctx := context.Background()
			role := fixture.readerRole
			params := administration.RoleParams{Name: "Updated definition"}
			expectedVersion := int64(1)
			expected := identity.ErrPrivilegeEscalation
			switch scenario {
			case "old permissions", "new permissions":
				controller := insertBindingPolicyRole(t, fixture, "Limited role manager", []identity.Permission{identity.PermissionRolesUpdate})
				appendOwnerBindingVersion(t, fixture, fixture.actorBinding, controller, "organization", nil, nil)
				if scenario == "new permissions" {
					role = insertBindingPolicyRole(t, fixture, "Empty original", nil)
					params.Permissions = []identity.Permission{identity.PermissionConnectionsList}
				}
			case "conditional authority":
				appendOwnerBindingVersion(t, fixture, fixture.actorBinding, fixture.managerRole, "organization", nil, policyString(`true`))
				expected = identity.ErrActorNotAuthorized
			case "connection authority":
				appendOwnerBindingVersion(t, fixture, fixture.actorBinding, fixture.managerRole, "connection", &fixture.connection, nil)
				expected = identity.ErrActorNotAuthorized
			case "stale version":
				expectedVersion = 2
				expected = identity.ErrRoleConflict
			case "system role":
				role = fixture.role
				expected = identity.ErrSystemRoleImmutable
			case "foreign role":
				role = "00000000-0000-0000-0000-000000000001"
				expected = identity.ErrRoleNotFound
			case "last recovery":
				controller := insertBindingPolicyRole(t, fixture, "Recovery piece controller", []identity.Permission{identity.PermissionRolesUpdate, identity.PermissionBindingsGet})
				appendOwnerBindingVersion(t, fixture, fixture.actorBinding, controller, "organization", nil, nil)
				role = insertBindingPolicyRole(t, fixture, "Recovery piece", []identity.Permission{identity.PermissionBindingsGet})

				if _, err := fixture.pool.Exec(ctx, `update public.role_permissions set deleted_at=clock_timestamp() where role_id=$1 and permission_key='bindings.get'`, fixture.managerRole); err != nil {
					t.Fatal(err)
				}

				grantPolicyRole(t, fixture, fixture.target, fixture.managerRole, "organization", false)
				grantPolicyRole(t, fixture, fixture.target, role, "organization", false)
				expected = identity.ErrLastAdministrator
			case "live expired binding":
				grantPolicyRole(t, fixture, fixture.target, role, "connection", true)
				expected = identity.ErrRoleInUse
			}
			var err error
			if scenario == "live expired binding" {
				err = roles.Delete(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.RoleID(role), expectedVersion)
			} else {
				_, err = roles.Update(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.RoleID(role), expectedVersion, params)
			}
			if !errors.Is(err, expected) {
				t.Fatalf("got %v want %v", err, expected)
			}
			var version int64
			if scenario == "last recovery" {
				if err := fixture.pool.QueryRow(ctx, `select version from public.roles where id=$1`, role).Scan(&version); err != nil || version != 1 {
					t.Fatalf("recovery rollback: %d %v", version, err)
				}
			}
		})
	}
}

// policyString makes optional condition source explicit in qualification grants.
func policyString(value string) *string { return &value }

func TestPolicyAccountCreationRefusesInvalidAndUnprivilegedRequests(t *testing.T) {
	for _, scenario := range []string{"email", "display name", "duplicate email", "foreign actor", "conditional actor", "no recovery", "incomplete catalog"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newBindingManagementFixture(t)
			accounts, _ := policyServices(t, fixture)
			ctx := context.Background()
			if _, err := fixture.pool.Exec(ctx, `alter table public.organization_memberships drop column role_id`); err != nil {
				t.Fatal(err)
			}
			definition := identity.NewPolicyAccount{Email: "created-policy@example.com", DisplayName: "Created account"}
			actor := fixture.user
			var expected error
			switch scenario {
			case "email":
				definition.Email = "invalid"
			case "display name":
				definition.DisplayName = ""
			case "duplicate email":
				if _, err := accounts.Create(ctx, identity.OrganizationID(fixture.organization), identity.UserID(actor), definition); err != nil {
					t.Fatal(err)
				}
				expected = identity.ErrEmailTaken
			case "foreign actor":
				actor = fixture.foreignUser
				expected = identity.ErrActorNotAuthorized
			case "conditional actor":
				appendOwnerBindingVersion(t, fixture, fixture.actorBinding, fixture.managerRole, "organization", nil, policyString(`true`))
				expected = identity.ErrActorNotAuthorized
			case "no recovery":
				controller := insertBindingPolicyRole(t, fixture, "Creation controller", []identity.Permission{identity.PermissionUsersCreate})
				appendOwnerBindingVersion(t, fixture, fixture.actorBinding, controller, "organization", nil, nil)
				expected = identity.ErrLastAdministrator
			case "incomplete catalog":
				if _, err := fixture.pool.Exec(ctx, `delete from public.role_permissions where permission_key='bindings.create'; delete from public.permissions where key='bindings.create'`); err != nil {
					t.Fatal(err)
				}
				expected = administration.ErrBindingManagementInactive
			}
			before := readBindingPolicyCounts(t, fixture)
			var accountsBefore int
			if err := fixture.pool.QueryRow(ctx, `select count(*) from public.users`).Scan(&accountsBefore); err != nil {
				t.Fatal(err)
			}
			created, err := accounts.Create(ctx, identity.OrganizationID(fixture.organization), identity.UserID(actor), definition)
			if err == nil || (expected != nil && !errors.Is(err, expected)) || created.SetupLink.Token != "" {
				t.Fatalf("creation refusal: %+v %v want %v", created, err, expected)
			}
			var accountsAfter int
			if err := fixture.pool.QueryRow(ctx, `select count(*) from public.users`).Scan(&accountsAfter); err != nil || accountsBefore != accountsAfter || before != readBindingPolicyCounts(t, fixture) {
				t.Fatalf("failed creation left state: before=%d after=%d %v", accountsBefore, accountsAfter, err)
			}
		})
	}
}

func TestPolicySharedRoleEditRevokesActorSessionWithoutChangingLegacyGrants(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	_, roles := policyServices(t, fixture)
	ctx := context.Background()
	org, actor := identity.OrganizationID(fixture.organization), identity.UserID(fixture.user)
	legacy := pg.NewIdentityStore(fixture.pool)
	before, err := legacy.PermissionsForUser(ctx, org, actor)
	if err != nil {
		t.Fatal(err)
	}
	(administrationEnv{store: legacy}).openSession(t, actor)
	catalog, err := legacy.ListPermissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var remaining []identity.Permission
	for _, permission := range catalog {
		if permission != identity.PermissionConnectionsGet {
			remaining = append(remaining, permission)
		}
	}
	updated, err := roles.Update(ctx, org, actor, identity.RoleID(fixture.managerRole), 1, administration.RoleParams{Name: "Shared manager edited", Permissions: remaining})
	if err != nil || updated.Version != 2 {
		t.Fatalf("shared edit: %+v %v", updated, err)
	}
	var sessions int
	if err := fixture.pool.QueryRow(ctx, `select count(*) from public.sessions where user_id=$1 and revoked_at is null`, fixture.user).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("actor session survived: %d %v", sessions, err)
	}
	after, err := legacy.PermissionsForUser(ctx, org, actor)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("legacy grant changed: %v %v %v", before, after, err)
	}
}
