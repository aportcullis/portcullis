package administration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

const (
	actorAdmin   identity.UserID = "admin-1"
	secondAdmin  identity.UserID = "admin-2"
	userManager  identity.UserID = "manager-1"
	requesterOne identity.UserID = "requester-1"
	foreignUser  identity.UserID = "foreign-1"
	managerRole  identity.RoleID = "role-manager"
)

// userManagerPermissions administer users without holding audit or approval keys.
var userManagerPermissions = []identity.Permission{
	identity.PermissionUsersList, identity.PermissionUsersGet, identity.PermissionUsersCreate, identity.PermissionUsersUpdate, identity.PermissionUsersDisable,
	identity.PermissionRolesList, identity.PermissionRequestsList, identity.PermissionRequestsGet, identity.PermissionRequestsCreate, identity.PermissionRequestsExecute,
}

type userFixture struct {
	store   *fakeStore
	auditor *recordingAuditor
	service *administration.UserService
}

func newUserFixture(t *testing.T) userFixture {
	t.Helper()
	store := newFakeStore()
	store.addMember(homeOrganization, actorAdmin, adminRole)
	store.addMember(homeOrganization, requesterOne, requesterRole)
	store.addCustomRole(managerRole, "user manager", userManagerPermissions...)
	store.addMember(homeOrganization, userManager, managerRole)
	store.addMember(foreignOrganization, foreignUser, foreignRole)
	auditor := &recordingAuditor{}
	service, err := administration.NewUserService(store, auditor)
	if err != nil {
		t.Fatalf("NewUserService: %v", err)
	}
	return userFixture{store: store, auditor: auditor, service: service}
}

// lastCommittedEvent returns the newest event the store committed with a mutation.
func (f userFixture) lastCommittedEvent(t *testing.T) audit.Event {
	t.Helper()
	if len(f.store.committedEvents) == 0 {
		t.Fatal("no audit event committed")
	}
	return f.store.committedEvents[len(f.store.committedEvents)-1]
}

// assertRefusalAudited requires exactly one FAILED event of the attempted action by the actor.
func (f userFixture) assertRefusalAudited(t *testing.T, action audit.Action, actor identity.UserID) {
	t.Helper()
	if len(f.auditor.events) != 1 {
		t.Fatalf("refusal events = %+v, want one", f.auditor.events)
	}
	event := f.auditor.events[0]
	if event.Action != action || event.Outcome != audit.OutcomeFailed || event.ActorUserID == nil || *event.ActorUserID != actor || event.OrganizationID != homeOrganization {
		t.Errorf("refusal event = %+v, want FAILED %s by %s", event, action, actor)
	}
}

func digestOfSetupToken(t *testing.T, token string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		t.Fatalf("setup token %q is not 32 base64url bytes", token)
	}
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func TestCreateUserIssuesOneTimeSetupLink(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		actor       identity.UserID
		email       string
		displayName string
		role        identity.RoleID
		wantEmail   string
	}{
		{"admin creates an approver", actorAdmin, "reviewer@example.com", "Reviewer", approverRole, "reviewer@example.com"},
		{"email is normalized", actorAdmin, "  Mixed.Case@Example.COM ", "Mixed", requesterRole, "mixed.case@example.com"},
		{"unicode display name", actorAdmin, "kim@example.com", "김 검토자", requesterRole, "kim@example.com"},
		{"user manager grants a role it fully holds", userManager, "dev@example.com", "Dev", requesterRole, "dev@example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := newUserFixture(t)
			created, err := fixture.service.Create(t.Context(), tc.actor, administration.CreateUserParams{Email: tc.email, DisplayName: tc.displayName, RoleID: tc.role})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if created.Member.User.Email != tc.wantEmail || created.Member.RoleID != tc.role || created.Member.HasPassword {
				t.Errorf("member = %+v, want %s with role %s and no password", created.Member, tc.wantEmail, tc.role)
			}
			stored := fixture.store.members[created.Member.User.ID]
			if !bytes.Equal(stored.openSetupHash, digestOfSetupToken(t, created.SetupLink.Token)) {
				t.Error("store must hold the SHA-256 digest of the returned token, never the token")
			}
			if want := fakeNow.Add(identity.PasswordSetupValidity); !created.SetupLink.ExpiresAt.Equal(want) {
				t.Errorf("ExpiresAt = %v, want %v", created.SetupLink.ExpiresAt, want)
			}
			event := fixture.lastCommittedEvent(t)
			if event.Action != audit.ActionUserCreated || event.Outcome != audit.OutcomeSucceeded || *event.ActorUserID != tc.actor || event.TargetType != audit.TargetTypeUser || event.OrganizationID != homeOrganization {
				t.Errorf("event = %+v, want USER_CREATED by %s", event, tc.actor)
			}
			if event.Metadata["role_id"] != string(tc.role) {
				t.Errorf("event metadata = %v, want role_id %s", event.Metadata, tc.role)
			}
		})
	}
}

func TestCreateUserRefusesInvalidInputEscalationAndForeignRoles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		actor         identity.UserID
		params        administration.CreateUserParams
		want          error
		wantRefusalBy identity.UserID
	}{
		{"malformed email", actorAdmin, administration.CreateUserParams{Email: "not-an-email", DisplayName: "X", RoleID: requesterRole}, identity.ErrInvalidEmail, ""},
		{"control character display name", actorAdmin, administration.CreateUserParams{Email: "x@example.com", DisplayName: "X\u0007", RoleID: requesterRole}, identity.ErrInvalidDisplayName, ""},
		{"user manager grants the admin role", userManager, administration.CreateUserParams{Email: "x@example.com", DisplayName: "X", RoleID: adminRole}, identity.ErrPrivilegeEscalation, userManager},
		{"role of another organization", actorAdmin, administration.CreateUserParams{Email: "x@example.com", DisplayName: "X", RoleID: foreignRole}, identity.ErrRoleNotFound, ""},
		{"email already registered", actorAdmin, administration.CreateUserParams{Email: "requester-1@example.com", DisplayName: "X", RoleID: requesterRole}, identity.ErrEmailTaken, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := newUserFixture(t)
			before := len(fixture.store.members)
			if _, err := fixture.service.Create(t.Context(), tc.actor, tc.params); !errors.Is(err, tc.want) {
				t.Fatalf("Create = %v, want %v", err, tc.want)
			}
			if len(fixture.store.members) != before || len(fixture.store.committedEvents) != 0 {
				t.Error("a refused create must not persist a member or a success event")
			}
			if tc.wantRefusalBy != "" {
				fixture.assertRefusalAudited(t, audit.ActionUserCreated, tc.wantRefusalBy)
			} else if len(fixture.auditor.events) != 0 {
				t.Errorf("input refusals are not privilege refusals; got %+v", fixture.auditor.events)
			}
		})
	}
}

func TestIssueSetupLinkReplacesThePendingLink(t *testing.T) {
	t.Parallel()
	fixture := newUserFixture(t)
	created, err := fixture.service.Create(t.Context(), actorAdmin, administration.CreateUserParams{Email: "pending@example.com", DisplayName: "Pending", RoleID: requesterRole})
	if err != nil {
		t.Fatal(err)
	}
	user := created.Member.User.ID
	reissued, err := fixture.service.IssueSetupLink(t.Context(), actorAdmin, user)
	if err != nil {
		t.Fatalf("IssueSetupLink: %v", err)
	}
	if reissued.Token == created.SetupLink.Token {
		t.Error("a reissued link must carry a fresh token")
	}
	if !bytes.Equal(fixture.store.members[user].openSetupHash, digestOfSetupToken(t, reissued.Token)) {
		t.Error("the open link must be the reissued one")
	}
	event := fixture.lastCommittedEvent(t)
	if event.Action != audit.ActionUserSetupLinkIssued || event.TargetID != string(user) {
		t.Errorf("event = %+v, want USER_SETUP_LINK_ISSUED for %s", event, user)
	}
	byManager, err := fixture.service.IssueSetupLink(t.Context(), userManager, user)
	if err != nil || byManager.Token == reissued.Token {
		t.Errorf("user manager reissue = %v, %v; want a fresh token", byManager, err)
	}
}

func TestIssueSetupLinkRefusesAccountsThatCannotUseOne(t *testing.T) {
	t.Parallel()
	fixture := newUserFixture(t)
	fixture.store.members[requesterOne].hasPassword = false
	fixture.store.members[requesterOne].user.Status = identity.StatusDisabled
	cases := []struct {
		name  string
		actor identity.UserID
		user  identity.UserID
		want  error
	}{
		{"user with a password", actorAdmin, userManager, identity.ErrPasswordAlreadySet},
		{"disabled user", actorAdmin, requesterOne, identity.ErrUserDisabled},
		{"user of another organization", actorAdmin, foreignUser, identity.ErrUserNotFound},
		{"user manager targets an administrator", userManager, actorAdmin, identity.ErrPrivilegeEscalation},
		{"unknown user", actorAdmin, "missing", identity.ErrUserNotFound},
	}
	for _, tc := range cases {
		if _, err := fixture.service.IssueSetupLink(t.Context(), tc.actor, tc.user); !errors.Is(err, tc.want) {
			t.Errorf("%s: IssueSetupLink = %v, want %v", tc.name, err, tc.want)
		}
	}
	if len(fixture.store.committedEvents) != 0 {
		t.Errorf("refused issues committed %+v", fixture.store.committedEvents)
	}
}

func TestDisableAndEnableRevokeSessionsAndAudit(t *testing.T) {
	t.Parallel()
	fixture := newUserFixture(t)
	fixture.store.addMember(homeOrganization, secondAdmin, adminRole)
	steps := []struct {
		name   string
		actor  identity.UserID
		user   identity.UserID
		enable bool
		action audit.Action
		status identity.UserStatus
	}{
		{"admin disables a requester", actorAdmin, requesterOne, false, audit.ActionUserDisabled, identity.StatusDisabled},
		{"admin enables the requester", actorAdmin, requesterOne, true, audit.ActionUserEnabled, identity.StatusActive},
		{"user manager disables a requester", userManager, requesterOne, false, audit.ActionUserDisabled, identity.StatusDisabled},
		{"admin disables another admin", actorAdmin, secondAdmin, false, audit.ActionUserDisabled, identity.StatusDisabled},
	}
	for _, step := range steps {
		revocationsBefore := fixture.store.sessionRevocations[step.user]
		var member identity.Member
		var err error
		if step.enable {
			member, err = fixture.service.Enable(t.Context(), step.actor, step.user)
		} else {
			member, err = fixture.service.Disable(t.Context(), step.actor, step.user)
		}
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if member.User.Status != step.status {
			t.Errorf("%s: status = %s, want %s", step.name, member.User.Status, step.status)
		}
		if fixture.store.sessionRevocations[step.user] != revocationsBefore+1 {
			t.Errorf("%s: sessions were not revoked with the change", step.name)
		}
		event := fixture.lastCommittedEvent(t)
		if event.Action != step.action || event.TargetID != string(step.user) || *event.ActorUserID != step.actor {
			t.Errorf("%s: event = %+v", step.name, event)
		}
	}
	if len(fixture.auditor.events) != 0 {
		t.Errorf("successful changes recorded refusals %+v", fixture.auditor.events)
	}
}

func TestDisableRefusesSelfEscalationForeignAndLastAdministrator(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		prepare       func(*fakeStore)
		actor         identity.UserID
		user          identity.UserID
		want          error
		wantRefusalBy identity.UserID
	}{
		{"own account", nil, actorAdmin, actorAdmin, identity.ErrSelfAdministration, actorAdmin},
		{"user manager locks out an administrator", nil, userManager, actorAdmin, identity.ErrPrivilegeEscalation, userManager},
		{"user of another organization", nil, actorAdmin, foreignUser, identity.ErrUserNotFound, ""},
		{"last active administrator", func(store *fakeStore) {
			// The actor's grant was read before a concurrent change removed its own administrator role.
			store.actorPermissions[userManager] = identity.EnforcedPermissions()
			store.withoutAdministratorRole(managerRole)
		}, userManager, actorAdmin, identity.ErrLastAdministrator, userManager},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := newUserFixture(t)
			if tc.prepare != nil {
				tc.prepare(fixture.store)
			}
			if _, err := fixture.service.Disable(t.Context(), tc.actor, tc.user); !errors.Is(err, tc.want) {
				t.Fatalf("Disable = %v, want %v", err, tc.want)
			}
			if member, ok := fixture.store.members[tc.user]; ok && !member.user.Active() {
				t.Error("a refused disable must leave the account active")
			}
			if len(fixture.store.committedEvents) != 0 {
				t.Errorf("refused disable committed %+v", fixture.store.committedEvents)
			}
			if tc.wantRefusalBy != "" {
				fixture.assertRefusalAudited(t, audit.ActionUserDisabled, tc.wantRefusalBy)
			}
		})
	}
}

func TestAssignRoleReplacesTheMembersRole(t *testing.T) {
	t.Parallel()
	fixture := newUserFixture(t)
	fixture.store.addMember(homeOrganization, secondAdmin, adminRole)
	steps := []struct {
		name  string
		actor identity.UserID
		user  identity.UserID
		role  identity.RoleID
	}{
		{"promote requester to approver", actorAdmin, requesterOne, approverRole},
		{"promote approver to admin", actorAdmin, requesterOne, adminRole},
		{"demote another admin while one remains", actorAdmin, secondAdmin, requesterRole},
		{"user manager moves a requester to its own role", userManager, secondAdmin, managerRole},
	}
	for _, step := range steps {
		previous := fixture.store.members[step.user].role
		member, err := fixture.service.AssignRole(t.Context(), step.actor, step.user, step.role)
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if member.RoleID != step.role {
			t.Errorf("%s: role = %s, want %s", step.name, member.RoleID, step.role)
		}
		event := fixture.lastCommittedEvent(t)
		if event.Action != audit.ActionUserRoleAssigned || event.Metadata["previous_role_id"] != string(previous) || event.Metadata["role_id"] != string(step.role) {
			t.Errorf("%s: event = %+v", step.name, event)
		}
		if fixture.store.sessionRevocations[step.user] == 0 {
			t.Errorf("%s: sessions were not revoked", step.name)
		}
	}
	events := len(fixture.store.committedEvents)
	if _, err := fixture.service.AssignRole(t.Context(), actorAdmin, requesterOne, adminRole); err != nil {
		t.Fatalf("unchanged role: %v", err)
	}
	if len(fixture.store.committedEvents) != events {
		t.Error("assigning the current role must not commit a change")
	}
}

func TestAssignRoleRefusesSelfEscalationForeignRolesAndLastAdministrator(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		prepare       func(*fakeStore)
		actor         identity.UserID
		user          identity.UserID
		role          identity.RoleID
		want          error
		wantRefusalBy identity.UserID
	}{
		{"own account", nil, actorAdmin, actorAdmin, requesterRole, identity.ErrSelfAdministration, actorAdmin},
		{"user manager grants the admin role", nil, userManager, requesterOne, adminRole, identity.ErrPrivilegeEscalation, userManager},
		{"user manager strips an approver", func(store *fakeStore) { store.members[requesterOne].role = approverRole }, userManager, requesterOne, requesterRole, identity.ErrPrivilegeEscalation, userManager},
		{"role of another organization", nil, actorAdmin, requesterOne, foreignRole, identity.ErrRoleNotFound, ""},
		{"last active administrator", func(store *fakeStore) {
			store.actorPermissions[userManager] = identity.EnforcedPermissions()
			store.withoutAdministratorRole(managerRole)
		}, userManager, actorAdmin, requesterRole, identity.ErrLastAdministrator, userManager},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := newUserFixture(t)
			if tc.prepare != nil {
				tc.prepare(fixture.store)
			}
			previous := fixture.store.members[tc.user].role
			if _, err := fixture.service.AssignRole(t.Context(), tc.actor, tc.user, tc.role); !errors.Is(err, tc.want) {
				t.Fatalf("AssignRole = %v, want %v", err, tc.want)
			}
			if fixture.store.members[tc.user].role != previous || len(fixture.store.committedEvents) != 0 {
				t.Error("a refused assignment must leave the role and trail unchanged")
			}
			if tc.wantRefusalBy != "" {
				fixture.assertRefusalAudited(t, audit.ActionUserRoleAssigned, tc.wantRefusalBy)
			}
		})
	}
}

func TestListAndGetStayInsideTheOrganization(t *testing.T) {
	t.Parallel()
	fixture := newUserFixture(t)
	members, err := fixture.service.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("members = %+v, want the three home members", members)
	}
	for _, member := range members {
		if member.User.ID == foreignUser {
			t.Error("List leaked a member of another organization")
		}
	}
	got, err := fixture.service.Get(t.Context(), requesterOne)
	if err != nil || got.RoleName != "requester" {
		t.Errorf("Get = %+v, %v; want the requester", got, err)
	}
	if _, err := fixture.service.Get(t.Context(), foreignUser); !errors.Is(err, identity.ErrUserNotFound) {
		t.Errorf("Get(foreign) = %v, want ErrUserNotFound", err)
	}
}
