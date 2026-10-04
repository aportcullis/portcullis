package administration_test

import (
	"context"
	"slices"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

const (
	homeOrganization    identity.OrganizationID = "org-home"
	foreignOrganization identity.OrganizationID = "org-foreign"

	adminRole     identity.RoleID = "role-admin"
	approverRole  identity.RoleID = "role-approver"
	requesterRole identity.RoleID = "role-requester"
	foreignRole   identity.RoleID = "role-foreign"
)

// fakeNow is the database clock the fake store stamps setup links with.
var fakeNow = time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)

// approverPermissions mirrors the seeded approver grant.
var approverPermissions = []identity.Permission{
	identity.PermissionRequestsList, identity.PermissionRequestsGet, identity.PermissionRequestsCreate, identity.PermissionRequestsExecute,
	identity.PermissionRequestsApprove, identity.PermissionRequestsReject, identity.PermissionAuditList, identity.PermissionAuditGet,
}

// requesterPermissions mirrors the seeded requester grant.
var requesterPermissions = []identity.Permission{
	identity.PermissionRequestsList, identity.PermissionRequestsGet, identity.PermissionRequestsCreate, identity.PermissionRequestsExecute,
}

// fakeMember is one membership with the account's credential and session state.
type fakeMember struct {
	organization  identity.OrganizationID
	user          identity.User
	role          identity.RoleID
	hasPassword   bool
	openSetupHash []byte
}

// fakeRole is one role with its owning organization and soft-delete marker.
type fakeRole struct {
	organization identity.OrganizationID
	role         identity.Role
	deleted      bool
}

// fakeStore is an in-memory UserRepository and RoleRepository that mirrors the store contract: organization scoping, atomic audit, session revocation and the locked last-administrator check.
type fakeStore struct {
	members map[identity.UserID]*fakeMember
	roles   map[identity.RoleID]*fakeRole
	// actorPermissions overrides role-derived permissions for an actor, modelling a grant observed before a concurrent change.
	actorPermissions   map[identity.UserID][]identity.Permission
	committedEvents    []audit.Event
	sessionRevocations map[identity.UserID]int
	nextID             int
	delegations        []identity.Delegation
	// lockedRefusal is returned once by the next mutation.
	lockedRefusal error
}

func (f *fakeStore) reauthorize(delegation identity.Delegation) error {
	f.delegations = append(f.delegations, delegation)
	refusal := f.lockedRefusal
	f.lockedRefusal = nil
	return refusal
}

func newFakeStore() *fakeStore {
	store := &fakeStore{
		members:            map[identity.UserID]*fakeMember{},
		roles:              map[identity.RoleID]*fakeRole{},
		actorPermissions:   map[identity.UserID][]identity.Permission{},
		sessionRevocations: map[identity.UserID]int{},
	}
	store.roles[adminRole] = &fakeRole{organization: homeOrganization, role: identity.Role{ID: adminRole, Name: "admin", IsSystem: true, Permissions: identity.EnforcedPermissions(), Version: 1}}
	store.roles[approverRole] = &fakeRole{organization: homeOrganization, role: identity.Role{ID: approverRole, Name: "approver", IsSystem: true, Permissions: approverPermissions, Version: 1}}
	store.roles[requesterRole] = &fakeRole{organization: homeOrganization, role: identity.Role{ID: requesterRole, Name: "requester", IsSystem: true, Permissions: requesterPermissions, Version: 1}}
	store.roles[foreignRole] = &fakeRole{organization: foreignOrganization, role: identity.Role{ID: foreignRole, Name: "foreign", Permissions: requesterPermissions, Version: 1}}
	return store
}

// addMember seeds an account with a password in the given organization and role.
func (f *fakeStore) addMember(organization identity.OrganizationID, user identity.UserID, role identity.RoleID) {
	f.members[user] = &fakeMember{
		organization: organization,
		user:         identity.User{ID: user, Email: string(user) + "@example.com", DisplayName: string(user), Status: identity.StatusActive},
		role:         role,
		hasPassword:  true,
	}
}

// addCustomRole seeds a custom role of the home organization.
func (f *fakeStore) addCustomRole(role identity.RoleID, name string, permissions ...identity.Permission) {
	f.roles[role] = &fakeRole{organization: homeOrganization, role: identity.Role{ID: role, Name: name, Permissions: permissions, Version: 1}}
}

// withoutAdministratorRole strips users.disable from a role so its members stop counting as administrators.
func (f *fakeStore) withoutAdministratorRole(role identity.RoleID) {
	stored := f.roles[role]
	stored.role.Permissions = slices.DeleteFunc(slices.Clone(stored.role.Permissions), func(permission identity.Permission) bool {
		return permission == identity.PermissionUsersDisable
	})
}

func (f *fakeStore) DefaultOrganizationID(context.Context) (identity.OrganizationID, error) {
	return homeOrganization, nil
}

func (f *fakeStore) PermissionsForUser(_ context.Context, org identity.OrganizationID, user identity.UserID) ([]identity.Permission, error) {
	if permissions, ok := f.actorPermissions[user]; ok {
		return permissions, nil
	}
	member, ok := f.members[user]
	if !ok || member.organization != org {
		return nil, nil
	}
	return f.roles[member.role].role.Permissions, nil
}

func (f *fakeStore) GetRole(_ context.Context, org identity.OrganizationID, role identity.RoleID) (identity.Role, error) {
	stored, ok := f.roles[role]
	if !ok || stored.deleted || stored.organization != org {
		return identity.Role{}, identity.ErrRoleNotFound
	}
	return f.roleView(stored), nil
}

func (f *fakeStore) roleView(stored *fakeRole) identity.Role {
	view := stored.role
	view.Permissions = slices.Clone(stored.role.Permissions)
	view.MemberCount = 0
	for _, member := range f.members {
		if member.role == stored.role.ID {
			view.MemberCount++
		}
	}
	return view
}

func (f *fakeStore) memberView(member *fakeMember) identity.Member {
	return identity.Member{User: member.user, RoleID: member.role, RoleName: f.roles[member.role].role.Name, HasPassword: member.hasPassword}
}

func (f *fakeStore) ListMembers(_ context.Context, org identity.OrganizationID) ([]identity.Member, error) {
	var members []identity.Member
	for _, member := range f.members {
		if member.organization == org {
			members = append(members, f.memberView(member))
		}
	}
	slices.SortFunc(members, func(left, right identity.Member) int {
		switch {
		case left.User.Email < right.User.Email:
			return -1
		case left.User.Email > right.User.Email:
			return 1
		}
		return 0
	})
	return members, nil
}

func (f *fakeStore) GetMember(_ context.Context, org identity.OrganizationID, user identity.UserID) (identity.Member, error) {
	member, ok := f.members[user]
	if !ok || member.organization != org {
		return identity.Member{}, identity.ErrUserNotFound
	}
	return f.memberView(member), nil
}

func (f *fakeStore) CreateMember(_ context.Context, org identity.OrganizationID, delegation identity.Delegation, member identity.NewMember, setup identity.PasswordSetupIssue, event audit.Event) (identity.Member, identity.PasswordSetup, error) {
	if err := f.reauthorize(delegation); err != nil {
		return identity.Member{}, identity.PasswordSetup{}, err
	}
	for _, existing := range f.members {
		if existing.user.Email == member.Email {
			return identity.Member{}, identity.PasswordSetup{}, identity.ErrEmailTaken
		}
	}
	if role, ok := f.roles[member.RoleID]; !ok || role.organization != org || role.deleted {
		return identity.Member{}, identity.PasswordSetup{}, identity.ErrRoleNotFound
	}
	f.nextID++
	id := identity.UserID("created-" + string(rune('a'+f.nextID)))
	created := &fakeMember{
		organization:  org,
		user:          identity.User{ID: id, Email: member.Email, DisplayName: member.DisplayName, Status: identity.StatusActive},
		role:          member.RoleID,
		openSetupHash: setup.TokenHash,
	}
	f.members[id] = created
	event.TargetID = string(id)
	f.committedEvents = append(f.committedEvents, event)
	return f.memberView(created), identity.PasswordSetup{UserID: id, OrganizationID: org, ExpiresAt: fakeNow.Add(setup.Validity)}, nil
}

func (f *fakeStore) IssuePasswordSetup(_ context.Context, org identity.OrganizationID, delegation identity.Delegation, user identity.UserID, setup identity.PasswordSetupIssue, event audit.Event) (identity.PasswordSetup, error) {
	if err := f.reauthorize(delegation); err != nil {
		return identity.PasswordSetup{}, err
	}
	member, ok := f.members[user]
	switch {
	case !ok || member.organization != org:
		return identity.PasswordSetup{}, identity.ErrUserNotFound
	case !member.user.Active():
		return identity.PasswordSetup{}, identity.ErrUserDisabled
	case member.hasPassword:
		return identity.PasswordSetup{}, identity.ErrPasswordAlreadySet
	}
	member.openSetupHash = setup.TokenHash
	f.committedEvents = append(f.committedEvents, event)
	return identity.PasswordSetup{UserID: user, OrganizationID: org, ExpiresAt: fakeNow.Add(setup.Validity)}, nil
}

func (f *fakeStore) SetMemberStatus(_ context.Context, org identity.OrganizationID, delegation identity.Delegation, user identity.UserID, status identity.UserStatus, event audit.Event) (identity.Member, error) {
	if err := f.reauthorize(delegation); err != nil {
		return identity.Member{}, err
	}
	member, ok := f.members[user]
	if !ok || member.organization != org {
		return identity.Member{}, identity.ErrUserNotFound
	}
	previous := member.user.Status
	member.user.Status = status
	if !f.hasActiveAdministrator(org) {
		member.user.Status = previous
		return identity.Member{}, identity.ErrLastAdministrator
	}
	if status == identity.StatusDisabled {
		member.openSetupHash = nil
	}
	f.sessionRevocations[user]++
	f.committedEvents = append(f.committedEvents, event)
	return f.memberView(member), nil
}

func (f *fakeStore) AssignMemberRole(_ context.Context, org identity.OrganizationID, delegation identity.Delegation, user identity.UserID, role identity.RoleID, event audit.Event) (identity.Member, error) {
	if err := f.reauthorize(delegation); err != nil {
		return identity.Member{}, err
	}
	member, ok := f.members[user]
	if !ok || member.organization != org {
		return identity.Member{}, identity.ErrUserNotFound
	}
	if stored, ok := f.roles[role]; !ok || stored.organization != org || stored.deleted {
		return identity.Member{}, identity.ErrRoleNotFound
	}
	previous := member.role
	member.role = role
	if !f.hasActiveAdministrator(org) {
		member.role = previous
		return identity.Member{}, identity.ErrLastAdministrator
	}
	f.sessionRevocations[user]++
	f.committedEvents = append(f.committedEvents, event)
	return f.memberView(member), nil
}

func (f *fakeStore) ListRoles(_ context.Context, org identity.OrganizationID) ([]identity.Role, error) {
	var roles []identity.Role
	for _, stored := range f.roles {
		if stored.organization == org && !stored.deleted {
			roles = append(roles, f.roleView(stored))
		}
	}
	slices.SortFunc(roles, func(left, right identity.Role) int {
		switch {
		case left.Name < right.Name:
			return -1
		case left.Name > right.Name:
			return 1
		}
		return 0
	})
	return roles, nil
}

func (f *fakeStore) CreateRole(_ context.Context, org identity.OrganizationID, delegation identity.Delegation, definition identity.RoleDefinition, event audit.Event) (identity.Role, error) {
	if err := f.reauthorize(delegation); err != nil {
		return identity.Role{}, err
	}
	for _, stored := range f.roles {
		if stored.organization == org && !stored.deleted && stored.role.Name == definition.Name {
			return identity.Role{}, identity.ErrRoleNameTaken
		}
	}
	f.nextID++
	id := identity.RoleID("custom-" + string(rune('a'+f.nextID)))
	f.roles[id] = &fakeRole{organization: org, role: identity.Role{ID: id, Name: definition.Name, Permissions: definition.Permissions, Version: 1}}
	event.TargetID = string(id)
	f.committedEvents = append(f.committedEvents, event)
	return f.roleView(f.roles[id]), nil
}

func (f *fakeStore) UpdateRole(_ context.Context, org identity.OrganizationID, delegation identity.Delegation, role identity.RoleID, expectedVersion int64, definition identity.RoleDefinition, event audit.Event) (identity.Role, error) {
	if err := f.reauthorize(delegation); err != nil {
		return identity.Role{}, err
	}
	stored, ok := f.roles[role]
	switch {
	case !ok || stored.organization != org || stored.deleted:
		return identity.Role{}, identity.ErrRoleNotFound
	case stored.role.IsSystem:
		return identity.Role{}, identity.ErrSystemRoleImmutable
	case stored.role.Version != expectedVersion:
		return identity.Role{}, identity.ErrRoleConflict
	}
	previous := stored.role
	stored.role.Name = definition.Name
	stored.role.Permissions = definition.Permissions
	stored.role.Version++
	if !f.hasActiveAdministrator(org) {
		stored.role = previous
		return identity.Role{}, identity.ErrLastAdministrator
	}
	for user, member := range f.members {
		if member.role == role {
			f.sessionRevocations[user]++
		}
	}
	f.committedEvents = append(f.committedEvents, event)
	return f.roleView(stored), nil
}

func (f *fakeStore) DeleteRole(_ context.Context, org identity.OrganizationID, delegation identity.Delegation, role identity.RoleID, expectedVersion int64, event audit.Event) error {
	if err := f.reauthorize(delegation); err != nil {
		return err
	}
	stored, ok := f.roles[role]
	switch {
	case !ok || stored.organization != org || stored.deleted:
		return identity.ErrRoleNotFound
	case stored.role.IsSystem:
		return identity.ErrSystemRoleImmutable
	case stored.role.Version != expectedVersion:
		return identity.ErrRoleConflict
	case f.roleView(stored).MemberCount > 0:
		return identity.ErrRoleInUse
	}
	stored.deleted = true
	f.committedEvents = append(f.committedEvents, event)
	return nil
}

// hasActiveAdministrator applies the locked post-change count the PostgreSQL store runs.
func (f *fakeStore) hasActiveAdministrator(org identity.OrganizationID) bool {
	for _, member := range f.members {
		if member.organization == org && member.user.Active() && identity.IsAdministrator(f.roles[member.role].role.Permissions) {
			return true
		}
	}
	return false
}

// recordingAuditor captures best-effort refusal events.
type recordingAuditor struct {
	events []audit.Event
}

func (r *recordingAuditor) Record(_ context.Context, event audit.Event) error {
	r.events = append(r.events, event)
	return nil
}
