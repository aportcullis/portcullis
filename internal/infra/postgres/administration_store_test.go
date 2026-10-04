package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// administrationEnv is a freshly migrated database with a bootstrap administrator.
type administrationEnv struct {
	pool  *pgxpool.Pool
	store *pg.IdentityStore
	org   identity.OrganizationID
	admin identity.User
	roles map[string]identity.RoleID
}

func newAdministrationEnv(t *testing.T) administrationEnv {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.FreshPostgres(t)
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)
	admin, err := store.BootstrapAdmin(ctx, "admin@example.com", "Admin", "phc", nil, testEvent(audit.ActionAuthBootstrap))
	if err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	org, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := store.ListRoles(ctx, org)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	byName := map[string]identity.RoleID{}
	for _, role := range roles {
		byName[role.Name] = role.ID
	}
	return administrationEnv{pool: pool, store: store, org: org, admin: admin, roles: byName}
}

func administrationEvent(actor identity.UserID, action audit.Action, targetType string) audit.Event {
	return audit.Event{ActorType: audit.ActorUser, ActorUserID: &actor, Action: action, TargetType: targetType, Outcome: audit.OutcomeSucceeded}
}

func setupIssue(token string) identity.PasswordSetupIssue {
	digest := sha256.Sum256([]byte(token))
	return identity.PasswordSetupIssue{TokenHash: digest[:], Validity: identity.PasswordSetupValidity}
}

func (e administrationEnv) createMember(t *testing.T, email, systemRole string) identity.Member {
	t.Helper()
	return e.createMemberWithRole(t, email, e.roles[systemRole])
}

func (e administrationEnv) createMemberWithRole(t *testing.T, email string, role identity.RoleID) identity.Member {
	t.Helper()
	member, _, err := e.store.CreateMember(context.Background(), e.org, identity.Delegation{Actor: e.admin.ID, Gate: identity.PermissionUsersCreate}, identity.NewMember{Email: email, DisplayName: email, RoleID: role}, setupIssue(email), administrationEvent(e.admin.ID, audit.ActionUserCreated, audit.TargetTypeUser))
	if err != nil {
		t.Fatalf("CreateMember(%s): %v", email, err)
	}
	return member
}

func (e administrationEnv) openSession(t *testing.T, user identity.UserID) {
	t.Helper()
	digest := sha256.Sum256([]byte(string(user) + time.Now().String()))
	if _, err := e.store.CreateSession(context.Background(), identity.NewSession("", user, time.Now(), time.Hour, 2*time.Hour), digest[:]); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
}

func (e administrationEnv) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var total int
	if err := e.pool.QueryRow(context.Background(), query, args...).Scan(&total); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return total
}

func (e administrationEnv) activeSessions(t *testing.T, user identity.UserID) int {
	return e.count(t, `select count(*) from sessions where user_id = $1::uuid and revoked_at is null`, string(user))
}

func (e administrationEnv) auditCount(t *testing.T, action audit.Action) int {
	return e.count(t, `select count(*) from audit_events where action = $1`, string(action))
}

func (e administrationEnv) foreignOrganization(t *testing.T) (identity.OrganizationID, identity.RoleID, identity.UserID) {
	t.Helper()
	ctx := context.Background()
	var org string
	if err := e.pool.QueryRow(ctx, `insert into organizations (slug, name) values ('foreign', 'Foreign') returning id::text`).Scan(&org); err != nil {
		t.Fatal(err)
	}
	role := insertRole(t, ctx, e.pool, org, "foreign-role", "users.list")
	user, err := e.store.CreateUser(ctx, "foreigner@example.com", "Foreigner")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.AddMembership(ctx, identity.OrganizationID(org), user.ID, identity.RoleID(role)); err != nil {
		t.Fatal(err)
	}
	return identity.OrganizationID(org), identity.RoleID(role), user.ID
}

func TestCreateMemberPersistsAccountMembershipSetupLinkAndAudit(t *testing.T) {
	env := newAdministrationEnv(t)
	ctx := context.Background()
	before := time.Now()
	member, setup, err := env.store.CreateMember(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersCreate}, identity.NewMember{Email: "reviewer@example.com", DisplayName: "Reviewer", RoleID: env.roles["approver"]}, setupIssue("reviewer-token"), administrationEvent(env.admin.ID, audit.ActionUserCreated, audit.TargetTypeUser))
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if member.User.Email != "reviewer@example.com" || member.RoleName != "approver" || member.HasPassword || !member.User.Active() {
		t.Errorf("member = %+v", member)
	}
	if setup.UserID != member.User.ID || setup.ExpiresAt.Before(before.Add(identity.PasswordSetupValidity-time.Minute)) || setup.ExpiresAt.After(time.Now().Add(identity.PasswordSetupValidity+time.Minute)) {
		t.Errorf("setup = %+v, want a 24-hour link for the member", setup)
	}
	digest := sha256.Sum256([]byte("reviewer-token"))
	if got := env.count(t, `select count(*) from password_setup_tokens where user_id = $1::uuid and token_hash = $2 and consumed_at is null and revoked_at is null`, string(member.User.ID), digest[:]); got != 1 {
		t.Errorf("open setup rows with the digest = %d, want 1", got)
	}
	if got := env.count(t, `select count(*) from audit_events where action = 'USER_CREATED' and target_id = $1 and actor_user_id = $2::uuid`, string(member.User.ID), string(env.admin.ID)); got != 1 {
		t.Errorf("USER_CREATED events for the member = %d, want 1", got)
	}
	second := env.createMember(t, "approver@example.com", "requester")
	members, err := env.store.ListMembers(ctx, env.org)
	if err != nil {
		t.Fatal(err)
	}
	emails := make([]string, 0, len(members))
	for _, listed := range members {
		emails = append(emails, listed.User.Email)
	}
	if !slices.Equal(emails, []string{"admin@example.com", "approver@example.com", "reviewer@example.com"}) {
		t.Errorf("ListMembers emails = %v", emails)
	}
	got, err := env.store.GetMember(ctx, env.org, second.User.ID)
	if err != nil || got.RoleName != "requester" {
		t.Errorf("GetMember = %+v, %v", got, err)
	}
	adminView, err := env.store.GetMember(ctx, env.org, env.admin.ID)
	if err != nil || !adminView.HasPassword || adminView.RoleName != "admin" {
		t.Errorf("admin member = %+v, %v; want a password and the admin role", adminView, err)
	}
}

func TestCreateMemberRefusesTakenEmailForeignAndDeletedRoles(t *testing.T) {
	env := newAdministrationEnv(t)
	ctx := context.Background()
	_, foreignRole, _ := env.foreignOrganization(t)
	deleted := env.createCustomRole(t, "short lived", identity.PermissionAuditList)
	if err := env.store.DeleteRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionRolesDelete}, deleted.ID, deleted.Version, administrationEvent(env.admin.ID, audit.ActionRoleDeleted, audit.TargetTypeRole)); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	cases := []struct {
		name  string
		email string
		role  identity.RoleID
		want  error
	}{
		{"case-insensitively taken email", "ADMIN@example.com", env.roles["requester"], identity.ErrEmailTaken},
		{"role of another organization", "new@example.com", foreignRole, identity.ErrRoleNotFound},
		{"soft-deleted role", "new@example.com", deleted.ID, identity.ErrRoleNotFound},
		{"malformed role id", "new@example.com", "not-a-uuid", identity.ErrRoleNotFound},
	}
	for _, tc := range cases {
		_, _, err := env.store.CreateMember(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersCreate}, identity.NewMember{Email: tc.email, DisplayName: "X", RoleID: tc.role}, setupIssue(tc.name), administrationEvent(env.admin.ID, audit.ActionUserCreated, audit.TargetTypeUser))
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: CreateMember = %v, want %v", tc.name, err, tc.want)
		}
	}
	if got := env.count(t, `select count(*) from users`); got != 2 {
		t.Errorf("users = %d, want the admin and the foreigner only", got)
	}
	if got := env.auditCount(t, audit.ActionUserCreated); got != 0 {
		t.Errorf("refused creates committed %d events", got)
	}
}

func TestIssuePasswordSetupKeepsOneOpenLink(t *testing.T) {
	env := newAdministrationEnv(t)
	ctx := context.Background()
	pending := env.createMember(t, "pending@example.com", "requester")
	for _, token := range []string{"second-token", "third-token"} {
		if _, err := env.store.IssuePasswordSetup(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersUpdate}, pending.User.ID, setupIssue(token), administrationEvent(env.admin.ID, audit.ActionUserSetupLinkIssued, audit.TargetTypeUser)); err != nil {
			t.Fatalf("IssuePasswordSetup(%s): %v", token, err)
		}
	}
	if got := env.count(t, `select count(*) from password_setup_tokens where user_id = $1::uuid and consumed_at is null and revoked_at is null`, string(pending.User.ID)); got != 1 {
		t.Errorf("open links = %d, want 1", got)
	}
	if got := env.count(t, `select count(*) from password_setup_tokens where user_id = $1::uuid and revoked_at is not null`, string(pending.User.ID)); got != 2 {
		t.Errorf("revoked links = %d, want the two replaced ones", got)
	}
	if got := env.auditCount(t, audit.ActionUserSetupLinkIssued); got != 2 {
		t.Errorf("USER_SETUP_LINK_ISSUED = %d, want 2", got)
	}

	_, _, foreignUser := env.foreignOrganization(t)
	disabled := env.createMember(t, "disabled@example.com", "requester")
	if _, err := env.store.SetMemberStatus(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersDisable}, disabled.User.ID, identity.StatusDisabled, administrationEvent(env.admin.ID, audit.ActionUserDisabled, audit.TargetTypeUser)); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		user identity.UserID
		want error
	}{
		{"user with a password", env.admin.ID, identity.ErrPasswordAlreadySet},
		{"disabled user", disabled.User.ID, identity.ErrUserDisabled},
		{"user of another organization", foreignUser, identity.ErrUserNotFound},
		{"malformed id", "nope", identity.ErrUserNotFound},
	}
	for _, tc := range cases {
		if _, err := env.store.IssuePasswordSetup(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersUpdate}, tc.user, setupIssue(tc.name), administrationEvent(env.admin.ID, audit.ActionUserSetupLinkIssued, audit.TargetTypeUser)); !errors.Is(err, tc.want) {
			t.Errorf("%s: IssuePasswordSetup = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestSetMemberStatusRevokesSessionsAndGuardsTheLastAdministrator(t *testing.T) {
	env := newAdministrationEnv(t)
	ctx := context.Background()
	member := env.createMember(t, "member@example.com", "requester")
	env.openSession(t, member.User.ID)
	env.openSession(t, env.admin.ID)

	disabled, err := env.store.SetMemberStatus(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersDisable}, member.User.ID, identity.StatusDisabled, administrationEvent(env.admin.ID, audit.ActionUserDisabled, audit.TargetTypeUser))
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if disabled.User.Status != identity.StatusDisabled || env.activeSessions(t, member.User.ID) != 0 {
		t.Errorf("disabled member = %+v with %d sessions", disabled, env.activeSessions(t, member.User.ID))
	}
	if got := env.count(t, `select count(*) from password_setup_tokens where user_id = $1::uuid and revoked_at is null and consumed_at is null`, string(member.User.ID)); got != 0 {
		t.Errorf("open links after disable = %d, want 0", got)
	}
	if got := env.count(t, `select count(*) from audit_events where action = 'USER_DISABLED' and (metadata->>'revoked_sessions')::int = 1`); got != 1 {
		t.Errorf("USER_DISABLED with revoked_sessions=1 = %d, want 1", got)
	}
	enabled, err := env.store.SetMemberStatus(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersDisable}, member.User.ID, identity.StatusActive, administrationEvent(env.admin.ID, audit.ActionUserEnabled, audit.TargetTypeUser))
	if err != nil || !enabled.User.Active() {
		t.Fatalf("enable = %+v, %v", enabled, err)
	}

	if _, err := env.store.SetMemberStatus(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersDisable}, env.admin.ID, identity.StatusDisabled, administrationEvent(env.admin.ID, audit.ActionUserDisabled, audit.TargetTypeUser)); !errors.Is(err, identity.ErrLastAdministrator) {
		t.Fatalf("disable last admin = %v, want ErrLastAdministrator", err)
	}
	adminView, err := env.store.GetMember(ctx, env.org, env.admin.ID)
	if err != nil || !adminView.User.Active() || env.activeSessions(t, env.admin.ID) != 1 {
		t.Errorf("after refused disable: admin %+v with %d sessions", adminView, env.activeSessions(t, env.admin.ID))
	}
	if got := env.auditCount(t, audit.ActionUserDisabled); got != 1 {
		t.Errorf("USER_DISABLED events = %d, want only the member's", got)
	}
	_, _, foreignUser := env.foreignOrganization(t)
	if _, err := env.store.SetMemberStatus(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersDisable}, foreignUser, identity.StatusDisabled, administrationEvent(env.admin.ID, audit.ActionUserDisabled, audit.TargetTypeUser)); !errors.Is(err, identity.ErrUserNotFound) {
		t.Errorf("disable foreign user = %v, want ErrUserNotFound", err)
	}
}

func TestConcurrentAdministratorsCannotDisableEachOther(t *testing.T) {
	env := newAdministrationEnv(t)
	ctx := context.Background()
	second := env.createMember(t, "second-admin@example.com", "admin")
	type mutualDisable struct{ actor, target identity.UserID }
	pairs := []mutualDisable{{actor: env.admin.ID, target: second.User.ID}, {actor: second.User.ID, target: env.admin.ID}}
	for round := range 20 {
		if _, err := env.pool.Exec(ctx, `update users set status = 'active'`); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var group sync.WaitGroup
		results := make([]error, len(pairs))
		for idx, pair := range pairs {
			group.Add(1)
			go func() {
				defer group.Done()
				callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				<-start
				_, results[idx] = env.store.SetMemberStatus(callCtx, env.org, identity.Delegation{Actor: pair.actor, Gate: identity.PermissionUsersDisable}, pair.target, identity.StatusDisabled, administrationEvent(pair.actor, audit.ActionUserDisabled, audit.TargetTypeUser))
			}()
		}
		close(start)
		group.Wait()
		succeeded, refused := 0, 0
		for _, err := range results {
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, identity.ErrActorNotAuthorized):
				refused++
			default:
				t.Fatalf("round %d: unexpected error: %v", round, err)
			}
		}
		if succeeded != 1 || refused != 1 {
			t.Fatalf("round %d: succeeded %d refused %d, want exactly one of each", round, succeeded, refused)
		}
		if got := env.count(t, `select count(*) from users where status = 'active'`); got != 1 {
			t.Fatalf("round %d: active users = %d, want 1", round, got)
		}
	}
}

func TestAssignMemberRoleRevokesSessionsAndGuardsTheLastAdministrator(t *testing.T) {
	env := newAdministrationEnv(t)
	ctx := context.Background()
	member := env.createMember(t, "member@example.com", "requester")
	env.openSession(t, member.User.ID)
	updated, err := env.store.AssignMemberRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersUpdate}, member.User.ID, env.roles["approver"], administrationEvent(env.admin.ID, audit.ActionUserRoleAssigned, audit.TargetTypeUser))
	if err != nil || updated.RoleName != "approver" {
		t.Fatalf("AssignMemberRole = %+v, %v", updated, err)
	}
	if env.activeSessions(t, member.User.ID) != 0 {
		t.Error("role change must revoke the member's sessions")
	}
	permissions, err := env.store.PermissionsForUser(ctx, env.org, member.User.ID)
	if err != nil || !slices.Contains(permissions, identity.PermissionRequestsApprove) {
		t.Errorf("permissions after assignment = %v, %v", permissions, err)
	}
	deleted := env.createCustomRole(t, "gone", identity.PermissionAuditList)
	if err := env.store.DeleteRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionRolesDelete}, deleted.ID, deleted.Version, administrationEvent(env.admin.ID, audit.ActionRoleDeleted, audit.TargetTypeRole)); err != nil {
		t.Fatal(err)
	}
	if _, err := env.store.AssignMemberRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersUpdate}, member.User.ID, env.roles["admin"], administrationEvent(env.admin.ID, audit.ActionUserRoleAssigned, audit.TargetTypeUser)); err != nil {
		t.Fatalf("promote to admin: %v", err)
	}
	if _, err := env.store.AssignMemberRole(ctx, env.org, identity.Delegation{Actor: member.User.ID, Gate: identity.PermissionUsersUpdate}, env.admin.ID, env.roles["requester"], administrationEvent(member.User.ID, audit.ActionUserRoleAssigned, audit.TargetTypeUser)); err != nil {
		t.Fatalf("demote original admin while another remains: %v", err)
	}

	_, foreignRole, foreignUser := env.foreignOrganization(t)
	cases := []struct {
		name string
		user identity.UserID
		role identity.RoleID
		want error
	}{
		{"demote the last administrator", member.User.ID, env.roles["requester"], identity.ErrLastAdministrator},
		{"role of another organization", env.admin.ID, foreignRole, identity.ErrRoleNotFound},
		{"soft-deleted role", env.admin.ID, deleted.ID, identity.ErrRoleNotFound},
		{"user of another organization", foreignUser, env.roles["requester"], identity.ErrUserNotFound},
	}
	for _, tc := range cases {
		if _, err := env.store.AssignMemberRole(ctx, env.org, identity.Delegation{Actor: member.User.ID, Gate: identity.PermissionUsersUpdate}, tc.user, tc.role, administrationEvent(member.User.ID, audit.ActionUserRoleAssigned, audit.TargetTypeUser)); !errors.Is(err, tc.want) {
			t.Errorf("%s: AssignMemberRole = %v, want %v", tc.name, err, tc.want)
		}
	}
	if view, err := env.store.GetMember(ctx, env.org, member.User.ID); err != nil || view.RoleName != "admin" {
		t.Errorf("refused demotion left %+v, %v", view, err)
	}
}

func (e administrationEnv) createCustomRole(t *testing.T, name string, permissions ...identity.Permission) identity.Role {
	t.Helper()
	role, err := e.store.CreateRole(context.Background(), e.org, identity.Delegation{Actor: e.admin.ID, Gate: identity.PermissionRolesCreate}, identity.RoleDefinition{Name: name, Permissions: permissions}, administrationEvent(e.admin.ID, audit.ActionRoleCreated, audit.TargetTypeRole))
	if err != nil {
		t.Fatalf("CreateRole(%s): %v", name, err)
	}
	return role
}

func TestRoleLifecycleReplacesPermissionsAndSoftDeletes(t *testing.T) {
	env := newAdministrationEnv(t)
	ctx := context.Background()
	auditor := env.createCustomRole(t, "auditor", identity.PermissionAuditList, identity.PermissionAuditGet)
	if auditor.IsSystem || auditor.Version != 1 || !slices.Equal(auditor.Permissions, []identity.Permission{identity.PermissionAuditGet, identity.PermissionAuditList}) || auditor.MemberCount != 0 {
		t.Errorf("created role = %+v", auditor)
	}
	member := env.createMember(t, "auditor@example.com", "requester")
	if _, err := env.store.AssignMemberRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersUpdate}, member.User.ID, auditor.ID, administrationEvent(env.admin.ID, audit.ActionUserRoleAssigned, audit.TargetTypeUser)); err != nil {
		t.Fatal(err)
	}
	env.openSession(t, member.User.ID)

	updated, err := env.store.UpdateRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionRolesUpdate}, auditor.ID, 1, identity.RoleDefinition{Name: "senior auditor", Permissions: []identity.Permission{identity.PermissionAuditGet, identity.PermissionUsersList}}, administrationEvent(env.admin.ID, audit.ActionRoleUpdated, audit.TargetTypeRole))
	if err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if updated.Version != 2 || updated.Name != "senior auditor" || !slices.Equal(updated.Permissions, []identity.Permission{identity.PermissionAuditGet, identity.PermissionUsersList}) || updated.MemberCount != 1 {
		t.Errorf("updated role = %+v", updated)
	}
	if env.activeSessions(t, member.User.ID) != 0 {
		t.Error("a role update must revoke its members' sessions")
	}
	permissions, err := env.store.PermissionsForUser(ctx, env.org, member.User.ID)
	if err != nil || slices.Contains(permissions, identity.PermissionAuditList) || !slices.Contains(permissions, identity.PermissionUsersList) {
		t.Errorf("member permissions after update = %v, %v", permissions, err)
	}
	// The removed permission is soft-deleted, never erased (ADR-0053).
	if got := env.count(t, `select count(*) from role_permissions where role_id = $1::uuid and permission_key = 'audit.list' and deleted_at is not null`, string(auditor.ID)); got != 1 {
		t.Errorf("removed permission rows soft-deleted = %d, want 1", got)
	}
	restored, err := env.store.UpdateRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionRolesUpdate}, auditor.ID, 2, identity.RoleDefinition{Name: "senior auditor", Permissions: []identity.Permission{identity.PermissionAuditGet, identity.PermissionAuditList, identity.PermissionUsersList}}, administrationEvent(env.admin.ID, audit.ActionRoleUpdated, audit.TargetTypeRole))
	if err != nil || restored.Version != 3 || len(restored.Permissions) != 3 {
		t.Fatalf("re-adding a removed permission = %+v, %v", restored, err)
	}
	if got := env.count(t, `select count(*) from role_permissions where role_id = $1::uuid`, string(auditor.ID)); got != 3 {
		t.Errorf("role permission rows after re-adding = %d, want the revived row reused (3)", got)
	}
	if got := env.count(t, `select count(*) from role_permissions where role_id = $1::uuid and deleted_at is not null`, string(auditor.ID)); got != 0 {
		t.Errorf("deleted role permission rows after re-adding = %d, want 0", got)
	}
	if permissions, err := env.store.PermissionsForUser(ctx, env.org, member.User.ID); err != nil || !slices.Contains(permissions, identity.PermissionAuditList) {
		t.Errorf("re-added permission not granted: %v, %v", permissions, err)
	}
	emptied := env.createCustomRole(t, "emptied", identity.PermissionAuditList, identity.PermissionAuditGet)
	cleared, err := env.store.UpdateRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionRolesUpdate}, emptied.ID, 1, identity.RoleDefinition{Name: "emptied"}, administrationEvent(env.admin.ID, audit.ActionRoleUpdated, audit.TargetTypeRole))
	if err != nil || len(cleared.Permissions) != 0 {
		t.Errorf("clearing every permission = %+v, %v", cleared, err)
	}
	if got := env.count(t, `select count(*) from role_permissions where role_id = $1::uuid and deleted_at is not null`, string(emptied.ID)); got != 2 {
		t.Errorf("cleared role soft-deleted rows = %d, want 2", got)
	}

	unused := env.createCustomRole(t, "unused")
	if err := env.store.DeleteRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionRolesDelete}, unused.ID, 1, administrationEvent(env.admin.ID, audit.ActionRoleDeleted, audit.TargetTypeRole)); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	if _, err := env.store.GetRole(ctx, env.org, unused.ID); !errors.Is(err, identity.ErrRoleNotFound) {
		t.Errorf("GetRole(deleted) = %v, want ErrRoleNotFound", err)
	}
	if got := env.count(t, `select count(*) from roles where id = $1::uuid and deleted_at is not null`, string(unused.ID)); got != 1 {
		t.Error("the role row must remain, soft-deleted")
	}
	reused := env.createCustomRole(t, "unused")
	if reused.ID == unused.ID {
		t.Error("a reused name must create a new role")
	}
	for _, action := range []audit.Action{audit.ActionRoleCreated, audit.ActionRoleUpdated, audit.ActionRoleDeleted} {
		if env.auditCount(t, action) == 0 {
			t.Errorf("no %s event committed", action)
		}
	}
}

func TestRoleMutationsRefuseNamesSystemStaleAssignedForeignAndLastAdministrator(t *testing.T) {
	env := newAdministrationEnv(t)
	ctx := context.Background()
	auditor := env.createCustomRole(t, "auditor", identity.PermissionAuditList)
	assigned := env.createCustomRole(t, "assigned", identity.PermissionAuditList)
	env.createMemberWithRole(t, "assigned@example.com", assigned.ID)
	_, foreignRole, _ := env.foreignOrganization(t)
	owner := env.createCustomRole(t, "owner", identity.EnforcedPermissions()...)
	if _, err := env.store.AssignMemberRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersUpdate}, env.admin.ID, owner.ID, administrationEvent(env.admin.ID, audit.ActionUserRoleAssigned, audit.TargetTypeUser)); err != nil {
		t.Fatal(err)
	}
	event := administrationEvent(env.admin.ID, audit.ActionRoleUpdated, audit.TargetTypeRole)

	if _, err := env.store.CreateRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionRolesCreate}, identity.RoleDefinition{Name: "auditor"}, event); !errors.Is(err, identity.ErrRoleNameTaken) {
		t.Errorf("duplicate CreateRole = %v, want ErrRoleNameTaken", err)
	}
	updates := []struct {
		name    string
		role    identity.RoleID
		version int64
		roleDef identity.RoleDefinition
		want    error
	}{
		{"rename onto a live role", auditor.ID, 1, identity.RoleDefinition{Name: "assigned"}, identity.ErrRoleNameTaken},
		{"system role", env.roles["approver"], 1, identity.RoleDefinition{Name: "approver"}, identity.ErrSystemRoleImmutable},
		{"stale version", auditor.ID, 9, identity.RoleDefinition{Name: "auditor"}, identity.ErrRoleConflict},
		{"role of another organization", foreignRole, 1, identity.RoleDefinition{Name: "x"}, identity.ErrRoleNotFound},
		{"last administrator role loses users.disable", owner.ID, 1, identity.RoleDefinition{Name: "owner", Permissions: []identity.Permission{identity.PermissionUsersUpdate}}, identity.ErrLastAdministrator},
	}
	for _, tc := range updates {
		if _, err := env.store.UpdateRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionRolesUpdate}, tc.role, tc.version, tc.roleDef, event); !errors.Is(err, tc.want) {
			t.Errorf("%s: UpdateRole = %v, want %v", tc.name, err, tc.want)
		}
	}
	ownerAfter, err := env.store.GetRole(ctx, env.org, owner.ID)
	if err != nil || ownerAfter.Version != 1 || len(ownerAfter.Permissions) != len(identity.EnforcedPermissions()) {
		t.Errorf("refused owner update left %+v, %v", ownerAfter, err)
	}
	deletes := []struct {
		name    string
		role    identity.RoleID
		version int64
		want    error
	}{
		{"assigned role", assigned.ID, 1, identity.ErrRoleInUse},
		{"system role", env.roles["requester"], 1, identity.ErrSystemRoleImmutable},
		{"stale version", auditor.ID, 3, identity.ErrRoleConflict},
		{"role of another organization", foreignRole, 1, identity.ErrRoleNotFound},
	}
	for _, tc := range deletes {
		if err := env.store.DeleteRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionRolesDelete}, tc.role, tc.version, event); !errors.Is(err, tc.want) {
			t.Errorf("%s: DeleteRole = %v, want %v", tc.name, err, tc.want)
		}
	}
	if got := env.auditCount(t, audit.ActionRoleUpdated) + env.auditCount(t, audit.ActionRoleDeleted); got != 0 {
		t.Errorf("refused role mutations committed %d events", got)
	}
}

func TestMutationsReauthorizeTheActorUnderTheOrganizationLock(t *testing.T) {
	env := newAdministrationEnv(t)
	ctx := context.Background()
	requesterRole, err := env.store.GetRole(ctx, env.org, env.roles["requester"])
	if err != nil {
		t.Fatal(err)
	}
	managerPermissions := append(slices.Clone(requesterRole.Permissions),
		identity.PermissionUsersList, identity.PermissionUsersGet, identity.PermissionUsersCreate, identity.PermissionUsersUpdate, identity.PermissionUsersDisable,
		identity.PermissionRolesCreate, identity.PermissionRolesUpdate)
	managerRole := env.createCustomRole(t, "user manager", managerPermissions...)
	manager := env.createMemberWithRole(t, "manager@example.com", managerRole.ID).User.ID
	bystander := env.createMember(t, "bystander@example.com", "requester").User.ID
	userEvent := func(action audit.Action) audit.Event {
		return administrationEvent(manager, action, audit.TargetTypeUser)
	}
	roleEvent := administrationEvent(manager, audit.ActionRoleUpdated, audit.TargetTypeRole)

	created, _, err := env.store.CreateMember(ctx, env.org, identity.Delegation{Actor: manager, Gate: identity.PermissionUsersCreate}, identity.NewMember{Email: "created@example.com", DisplayName: "Created", RoleID: env.roles["requester"]}, setupIssue("created"), userEvent(audit.ActionUserCreated))
	if err != nil {
		t.Fatalf("CreateMember by an authorized manager: %v", err)
	}
	if _, err := env.store.SetMemberStatus(ctx, env.org, identity.Delegation{Actor: manager, Gate: identity.PermissionUsersDisable}, created.User.ID, identity.StatusDisabled, userEvent(audit.ActionUserDisabled)); err != nil {
		t.Fatalf("disable by an authorized manager: %v", err)
	}
	if _, err := env.store.SetMemberStatus(ctx, env.org, identity.Delegation{Actor: manager, Gate: identity.PermissionUsersDisable}, created.User.ID, identity.StatusActive, userEvent(audit.ActionUserEnabled)); err != nil {
		t.Fatalf("enable by an authorized manager: %v", err)
	}
	narrow := env.createCustomRole(t, "narrow", identity.PermissionRequestsList)
	if _, err := env.store.UpdateRole(ctx, env.org, identity.Delegation{Actor: manager, Gate: identity.PermissionRolesUpdate}, narrow.ID, 1, identity.RoleDefinition{Name: "narrow", Permissions: []identity.Permission{identity.PermissionRequestsList, identity.PermissionRequestsGet}}, roleEvent); err != nil {
		t.Fatalf("UpdateRole by an authorized manager: %v", err)
	}
	committed := env.count(t, `select count(*) from audit_events where actor_user_id = $1::uuid`, string(manager))

	if _, err := env.store.AssignMemberRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersUpdate}, created.User.ID, env.roles["approver"], administrationEvent(env.admin.ID, audit.ActionUserRoleAssigned, audit.TargetTypeUser)); err != nil {
		t.Fatal(err)
	}
	if _, err := env.store.SetMemberStatus(ctx, env.org, identity.Delegation{Actor: manager, Gate: identity.PermissionUsersDisable}, created.User.ID, identity.StatusDisabled, userEvent(audit.ActionUserDisabled)); !errors.Is(err, identity.ErrPrivilegeEscalation) {
		t.Errorf("disable a member promoted meanwhile = %v, want ErrPrivilegeEscalation", err)
	}
	if member, err := env.store.GetMember(ctx, env.org, created.User.ID); err != nil || !member.User.Active() {
		t.Errorf("refused disable changed the member: %+v, %v", member, err)
	}
	if _, err := env.store.AssignMemberRole(ctx, env.org, identity.Delegation{Actor: manager, Gate: identity.PermissionUsersUpdate}, bystander, env.roles["approver"], userEvent(audit.ActionUserRoleAssigned)); !errors.Is(err, identity.ErrPrivilegeEscalation) {
		t.Errorf("assign a role beyond the actor = %v, want ErrPrivilegeEscalation", err)
	}
	adminRoleEvent := administrationEvent(env.admin.ID, audit.ActionRoleUpdated, audit.TargetTypeRole)
	if _, err := env.store.UpdateRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionRolesUpdate}, narrow.ID, 2, identity.RoleDefinition{Name: "narrow", Permissions: []identity.Permission{identity.PermissionRequestsList, identity.PermissionAuditList}}, adminRoleEvent); err != nil {
		t.Fatal(err)
	}
	if _, err := env.store.UpdateRole(ctx, env.org, identity.Delegation{Actor: manager, Gate: identity.PermissionRolesUpdate}, narrow.ID, 3, identity.RoleDefinition{Name: "narrow"}, roleEvent); !errors.Is(err, identity.ErrPrivilegeEscalation) {
		t.Errorf("update a role that grew meanwhile = %v, want ErrPrivilegeEscalation", err)
	}
	if role, err := env.store.GetRole(ctx, env.org, narrow.ID); err != nil || role.Version != 3 {
		t.Errorf("refused role update changed the role: %+v, %v", role, err)
	}
	withoutDisable := slices.DeleteFunc(slices.Clone(managerPermissions), func(permission identity.Permission) bool { return permission == identity.PermissionUsersDisable })
	if _, err := env.store.UpdateRole(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionRolesUpdate}, managerRole.ID, 1, identity.RoleDefinition{Name: "user manager", Permissions: withoutDisable}, adminRoleEvent); err != nil {
		t.Fatal(err)
	}
	if _, err := env.store.SetMemberStatus(ctx, env.org, identity.Delegation{Actor: manager, Gate: identity.PermissionUsersDisable}, bystander, identity.StatusDisabled, userEvent(audit.ActionUserDisabled)); !errors.Is(err, identity.ErrActorNotAuthorized) {
		t.Errorf("disable after the actor lost users.disable = %v, want ErrActorNotAuthorized", err)
	}
	if _, err := env.store.SetMemberStatus(ctx, env.org, identity.Delegation{Actor: env.admin.ID, Gate: identity.PermissionUsersDisable}, manager, identity.StatusDisabled, administrationEvent(env.admin.ID, audit.ActionUserDisabled, audit.TargetTypeUser)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.store.CreateMember(ctx, env.org, identity.Delegation{Actor: manager, Gate: identity.PermissionUsersCreate}, identity.NewMember{Email: "late@example.com", DisplayName: "Late", RoleID: env.roles["requester"]}, setupIssue("late"), userEvent(audit.ActionUserCreated)); !errors.Is(err, identity.ErrActorNotAuthorized) {
		t.Errorf("create by a disabled actor = %v, want ErrActorNotAuthorized", err)
	}
	if got := env.count(t, `select count(*) from users where email = 'late@example.com'`); got != 0 {
		t.Error("a refused creation left an account behind")
	}
	if got := env.count(t, `select count(*) from audit_events where actor_user_id = $1::uuid`, string(manager)); got != committed {
		t.Errorf("refused mutations committed %d audit events", got-committed)
	}
}

func TestMembersAndRolesStayInsideTheOrganization(t *testing.T) {
	env := newAdministrationEnv(t)
	ctx := context.Background()
	foreignOrg, foreignRole, foreignUser := env.foreignOrganization(t)
	members, err := env.store.ListMembers(ctx, env.org)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if member.User.ID == foreignUser {
			t.Error("ListMembers leaked a foreign member")
		}
	}
	if _, err := env.store.GetMember(ctx, env.org, foreignUser); !errors.Is(err, identity.ErrUserNotFound) {
		t.Errorf("GetMember(foreign) = %v", err)
	}
	if _, err := env.store.GetMember(ctx, foreignOrg, env.admin.ID); !errors.Is(err, identity.ErrUserNotFound) {
		t.Errorf("GetMember(admin in foreign org) = %v", err)
	}
	roles, err := env.store.ListRoles(ctx, env.org)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 3 || !roles[0].IsSystem {
		t.Errorf("home roles = %+v, want the three system roles", roles)
	}
	if _, err := env.store.GetRole(ctx, env.org, foreignRole); !errors.Is(err, identity.ErrRoleNotFound) {
		t.Errorf("GetRole(foreign) = %v", err)
	}
}
