package administration_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

const (
	auditorRole identity.RoleID = "role-auditor"
	unusedRole  identity.RoleID = "role-unused"
)

type roleFixture struct {
	store   *fakeStore
	auditor *recordingAuditor
	service *administration.RoleService
}

func newRoleFixture(t *testing.T) roleFixture {
	t.Helper()
	store := newFakeStore()
	store.addMember(homeOrganization, actorAdmin, adminRole)
	store.addCustomRole(managerRole, "user manager", userManagerPermissions...)
	store.addMember(homeOrganization, userManager, managerRole)
	store.addCustomRole(auditorRole, "auditor", identity.PermissionAuditList)
	store.addMember(homeOrganization, requesterOne, auditorRole)
	store.addCustomRole(unusedRole, "unused", identity.PermissionRequestsList)
	auditor := &recordingAuditor{}
	service, err := administration.NewRoleService(store, auditor, identity.EnforcedPermissions())
	if err != nil {
		t.Fatalf("NewRoleService: %v", err)
	}
	return roleFixture{store: store, auditor: auditor, service: service}
}

func (f roleFixture) lastCommittedEvent(t *testing.T) audit.Event {
	t.Helper()
	if len(f.store.committedEvents) == 0 {
		t.Fatal("no audit event committed")
	}
	return f.store.committedEvents[len(f.store.committedEvents)-1]
}

func TestCreateRoleFromHeldCatalogPermissions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		actor           identity.UserID
		params          administration.RoleParams
		wantName        string
		wantPermissions []identity.Permission
	}{
		{"auditor role", actorAdmin, administration.RoleParams{Name: "read auditor", Permissions: []identity.Permission{identity.PermissionAuditGet, identity.PermissionAuditList}}, "read auditor", []identity.Permission{identity.PermissionAuditGet, identity.PermissionAuditList}},
		{"role without permissions", actorAdmin, administration.RoleParams{Name: "parked"}, "parked", nil},
		{"trimmed unicode name with duplicates", actorAdmin, administration.RoleParams{Name: " 검토자 ", Permissions: []identity.Permission{identity.PermissionRequestsApprove, identity.PermissionRequestsApprove}}, "검토자", []identity.Permission{identity.PermissionRequestsApprove}},
		{"user manager delegates a subset", userManager, administration.RoleParams{Name: "user reader", Permissions: []identity.Permission{identity.PermissionUsersList}}, "user reader", []identity.Permission{identity.PermissionUsersList}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := newRoleFixture(t)
			role, err := fixture.service.Create(t.Context(), tc.actor, tc.params)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if role.Name != tc.wantName || !slices.Equal(role.Permissions, tc.wantPermissions) || role.IsSystem || role.Version != 1 {
				t.Errorf("role = %+v, want custom %q %v at version 1", role, tc.wantName, tc.wantPermissions)
			}
			event := fixture.lastCommittedEvent(t)
			if event.Action != audit.ActionRoleCreated || event.TargetType != audit.TargetTypeRole || *event.ActorUserID != tc.actor || event.OrganizationID != homeOrganization {
				t.Errorf("event = %+v, want ROLE_CREATED by %s", event, tc.actor)
			}
		})
	}
}

func TestCreateRoleRefusesInvalidDefinitionsAndEscalation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		actor         identity.UserID
		params        administration.RoleParams
		want          error
		wantRefusalBy identity.UserID
	}{
		{"user manager grants audit access", userManager, administration.RoleParams{Name: "auditor two", Permissions: []identity.Permission{identity.PermissionAuditGet}}, identity.ErrPrivilegeEscalation, userManager},
		{"key outside the catalog", actorAdmin, administration.RoleParams{Name: "root", Permissions: []identity.Permission{"database.superuser"}}, identity.ErrPermissionNotInCatalog, ""},
		{"blank name", actorAdmin, administration.RoleParams{Name: "  "}, identity.ErrInvalidRoleName, ""},
		{"name of a live role", actorAdmin, administration.RoleParams{Name: "auditor"}, identity.ErrRoleNameTaken, ""},
		{"name of a system role", actorAdmin, administration.RoleParams{Name: "admin"}, identity.ErrRoleNameTaken, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := newRoleFixture(t)
			before := len(fixture.store.roles)
			if _, err := fixture.service.Create(t.Context(), tc.actor, tc.params); !errors.Is(err, tc.want) {
				t.Fatalf("Create = %v, want %v", err, tc.want)
			}
			if len(fixture.store.roles) != before || len(fixture.store.committedEvents) != 0 {
				t.Error("a refused create must not persist a role or success event")
			}
			if tc.wantRefusalBy != "" && (len(fixture.auditor.events) != 1 || fixture.auditor.events[0].Action != audit.ActionRoleCreated || fixture.auditor.events[0].Outcome != audit.OutcomeFailed) {
				t.Errorf("refusal events = %+v, want one FAILED ROLE_CREATED", fixture.auditor.events)
			}
		})
	}
}

func TestUpdateRoleReplacesDefinitionAndRevokesMemberSessions(t *testing.T) {
	t.Parallel()
	fixture := newRoleFixture(t)
	steps := []struct {
		name        string
		actor       identity.UserID
		params      administration.RoleParams
		wantAdded   []string
		wantRemoved []string
	}{
		{"grant audit get", actorAdmin, administration.RoleParams{Name: "auditor", Permissions: []identity.Permission{identity.PermissionAuditList, identity.PermissionAuditGet}}, []string{"audit.get"}, nil},
		{"rename only", actorAdmin, administration.RoleParams{Name: "senior auditor", Permissions: []identity.Permission{identity.PermissionAuditList, identity.PermissionAuditGet}}, nil, nil},
		{"swap keys", actorAdmin, administration.RoleParams{Name: "senior auditor", Permissions: []identity.Permission{identity.PermissionAuditGet, identity.PermissionUsersList}}, []string{"users.list"}, []string{"audit.list"}},
		{"revoke everything", actorAdmin, administration.RoleParams{Name: "senior auditor"}, nil, []string{"audit.get", "users.list"}},
	}
	for idx, step := range steps {
		revocationsBefore := fixture.store.sessionRevocations[requesterOne]
		role, err := fixture.service.Update(t.Context(), step.actor, auditorRole, int64(idx+1), step.params)
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if role.Version != int64(idx+2) || role.Name != step.params.Name {
			t.Errorf("%s: role = %+v", step.name, role)
		}
		if fixture.store.sessionRevocations[requesterOne] != revocationsBefore+1 {
			t.Errorf("%s: member sessions were not revoked", step.name)
		}
		event := fixture.lastCommittedEvent(t)
		added, _ := event.Metadata["added_permissions"].([]string)
		removed, _ := event.Metadata["removed_permissions"].([]string)
		if event.Action != audit.ActionRoleUpdated || event.TargetID != string(auditorRole) || !slices.Equal(added, step.wantAdded) || !slices.Equal(removed, step.wantRemoved) {
			t.Errorf("%s: event = %+v, want added %v removed %v", step.name, event, step.wantAdded, step.wantRemoved)
		}
	}
}

func TestUpdateRoleRefusesSystemStaleEscalationAndLastAdministrator(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		prepare       func(*fakeStore)
		actor         identity.UserID
		role          identity.RoleID
		version       int64
		params        administration.RoleParams
		want          error
		wantRefusalBy identity.UserID
	}{
		{"system role", nil, actorAdmin, adminRole, 1, administration.RoleParams{Name: "admin"}, identity.ErrSystemRoleImmutable, ""},
		{"stale version", nil, actorAdmin, auditorRole, 7, administration.RoleParams{Name: "auditor"}, identity.ErrRoleConflict, ""},
		{"user manager adds a key it lacks", nil, userManager, unusedRole, 1, administration.RoleParams{Name: "unused", Permissions: []identity.Permission{identity.PermissionAuditGet}}, identity.ErrPrivilegeEscalation, userManager},
		{"user manager strips a key it lacks", nil, userManager, auditorRole, 1, administration.RoleParams{Name: "auditor"}, identity.ErrPrivilegeEscalation, userManager},
		{"role of another organization", nil, actorAdmin, foreignRole, 1, administration.RoleParams{Name: "foreign"}, identity.ErrRoleNotFound, ""},
		{"last administrator role loses users.disable", func(store *fakeStore) {
			store.addCustomRole("role-owner", "owner", identity.EnforcedPermissions()...)
			store.members[actorAdmin].role = "role-owner"
			store.withoutAdministratorRole(managerRole)
		}, actorAdmin, "role-owner", 1, administration.RoleParams{Name: "owner", Permissions: []identity.Permission{identity.PermissionUsersUpdate}}, identity.ErrLastAdministrator, actorAdmin},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := newRoleFixture(t)
			if tc.prepare != nil {
				tc.prepare(fixture.store)
			}
			if _, err := fixture.service.Update(t.Context(), tc.actor, tc.role, tc.version, tc.params); !errors.Is(err, tc.want) {
				t.Fatalf("Update = %v, want %v", err, tc.want)
			}
			if len(fixture.store.committedEvents) != 0 {
				t.Errorf("refused update committed %+v", fixture.store.committedEvents)
			}
			if tc.wantRefusalBy != "" && (len(fixture.auditor.events) != 1 || fixture.auditor.events[0].Action != audit.ActionRoleUpdated) {
				t.Errorf("refusal events = %+v, want one FAILED ROLE_UPDATED", fixture.auditor.events)
			}
		})
	}
}

func TestDeleteRoleSoftDeletesOnlyUnassignedCustomRoles(t *testing.T) {
	t.Parallel()
	fixture := newRoleFixture(t)
	if err := fixture.service.Delete(t.Context(), actorAdmin, unusedRole, 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !fixture.store.roles[unusedRole].deleted {
		t.Error("the role must be soft-deleted, not removed")
	}
	if event := fixture.lastCommittedEvent(t); event.Action != audit.ActionRoleDeleted || event.TargetID != string(unusedRole) {
		t.Errorf("event = %+v, want ROLE_DELETED", event)
	}
	recreated, err := fixture.service.Create(t.Context(), actorAdmin, administration.RoleParams{Name: "unused"})
	if err != nil || recreated.ID == unusedRole {
		t.Errorf("recreate deleted name = %+v, %v; want a new role", recreated, err)
	}
	roles, err := fixture.service.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range roles {
		if role.ID == unusedRole || role.ID == foreignRole {
			t.Errorf("List returned %s", role.ID)
		}
	}
	cases := []struct {
		name    string
		role    identity.RoleID
		version int64
		want    error
	}{
		{"system role", requesterRole, 1, identity.ErrSystemRoleImmutable},
		{"assigned custom role", auditorRole, 1, identity.ErrRoleInUse},
		{"stale version", recreated.ID, 4, identity.ErrRoleConflict},
		{"already deleted", unusedRole, 1, identity.ErrRoleNotFound},
		{"role of another organization", foreignRole, 1, identity.ErrRoleNotFound},
	}
	for _, tc := range cases {
		if err := fixture.service.Delete(t.Context(), actorAdmin, tc.role, tc.version); !errors.Is(err, tc.want) {
			t.Errorf("%s: Delete = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestListPermissionsReturnsTheSortedCatalog(t *testing.T) {
	t.Parallel()
	fixture := newRoleFixture(t)
	permissions := fixture.service.ListPermissions()
	if len(permissions) != len(identity.EnforcedPermissions()) || !slices.IsSorted(permissions) {
		t.Errorf("ListPermissions = %v, want the sorted catalog", permissions)
	}
	permissions[0] = "tampered"
	if fixture.service.ListPermissions()[0] == "tampered" {
		t.Error("ListPermissions must return a copy")
	}
}
