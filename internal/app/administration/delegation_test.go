package administration_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

const customRole identity.RoleID = "role-custom"

type refusalRecord struct {
	action         audit.Action
	outcome        audit.Outcome
	actor          identity.UserID
	reasonRecorded bool
}

func refusalRecords(events []audit.Event) []refusalRecord {
	records := make([]refusalRecord, 0, len(events))
	for _, event := range events {
		record := refusalRecord{action: event.Action, outcome: event.Outcome}
		if event.ActorUserID != nil {
			record.actor = *event.ActorUserID
		}
		_, record.reasonRecorded = event.Metadata["reason"]
		records = append(records, record)
	}
	return records
}

type delegationScenario struct {
	name   string
	action audit.Action
	gate   identity.Permission
	run    func(ctx context.Context, users *administration.UserService, roles *administration.RoleService) error
}

func newDelegationServices(t *testing.T) (*fakeStore, *recordingAuditor, *administration.UserService, *administration.RoleService) {
	t.Helper()
	store := newFakeStore()
	store.addMember(homeOrganization, actorAdmin, adminRole)
	store.addMember(homeOrganization, secondAdmin, adminRole)
	store.addMember(homeOrganization, requesterOne, requesterRole)
	store.members[requesterOne].hasPassword = false
	store.addMember(homeOrganization, userManager, requesterRole)
	store.members[userManager].user.Status = identity.StatusDisabled
	store.addCustomRole(customRole, "custom", identity.PermissionAuditList)
	auditor := &recordingAuditor{}
	users, err := administration.NewUserService(store, auditor)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := administration.NewRoleService(store, auditor, identity.EnforcedPermissions())
	if err != nil {
		t.Fatal(err)
	}
	return store, auditor, users, roles
}

func delegationScenarios() []delegationScenario {
	return []delegationScenario{
		{name: "create user", action: audit.ActionUserCreated, gate: identity.PermissionUsersCreate, run: func(ctx context.Context, users *administration.UserService, _ *administration.RoleService) error {
			_, err := users.Create(ctx, actorAdmin, administration.CreateUserParams{Email: "new@example.com", DisplayName: "New", RoleID: requesterRole})
			return err
		}},
		{name: "issue setup link", action: audit.ActionUserSetupLinkIssued, gate: identity.PermissionUsersUpdate, run: func(ctx context.Context, users *administration.UserService, _ *administration.RoleService) error {
			_, err := users.IssueSetupLink(ctx, actorAdmin, requesterOne)
			return err
		}},
		{name: "disable user", action: audit.ActionUserDisabled, gate: identity.PermissionUsersDisable, run: func(ctx context.Context, users *administration.UserService, _ *administration.RoleService) error {
			_, err := users.Disable(ctx, actorAdmin, requesterOne)
			return err
		}},
		{name: "enable user", action: audit.ActionUserEnabled, gate: identity.PermissionUsersDisable, run: func(ctx context.Context, users *administration.UserService, _ *administration.RoleService) error {
			_, err := users.Enable(ctx, actorAdmin, userManager)
			return err
		}},
		{name: "assign role", action: audit.ActionUserRoleAssigned, gate: identity.PermissionUsersUpdate, run: func(ctx context.Context, users *administration.UserService, _ *administration.RoleService) error {
			_, err := users.AssignRole(ctx, actorAdmin, requesterOne, approverRole)
			return err
		}},
		{name: "create role", action: audit.ActionRoleCreated, gate: identity.PermissionRolesCreate, run: func(ctx context.Context, _ *administration.UserService, roles *administration.RoleService) error {
			_, err := roles.Create(ctx, actorAdmin, administration.RoleParams{Name: "reader", Permissions: []identity.Permission{identity.PermissionAuditGet}})
			return err
		}},
		{name: "update role", action: audit.ActionRoleUpdated, gate: identity.PermissionRolesUpdate, run: func(ctx context.Context, _ *administration.UserService, roles *administration.RoleService) error {
			_, err := roles.Update(ctx, actorAdmin, customRole, 1, administration.RoleParams{Name: "custom", Permissions: []identity.Permission{identity.PermissionAuditGet}})
			return err
		}},
		{name: "delete role", action: audit.ActionRoleDeleted, gate: identity.PermissionRolesDelete, run: func(ctx context.Context, _ *administration.UserService, roles *administration.RoleService) error {
			return roles.Delete(ctx, actorAdmin, customRole, 1)
		}},
	}
}

func TestMutationsHandTheStoreTheActorAndTheGateThatAdmittedThem(t *testing.T) {
	t.Parallel()
	for _, scenario := range delegationScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			store, _, users, roles := newDelegationServices(t)
			if err := scenario.run(context.Background(), users, roles); err != nil {
				t.Fatalf("%s: %v", scenario.name, err)
			}
			want := []identity.Delegation{{Actor: actorAdmin, Gate: scenario.gate}}
			if !slices.Equal(store.delegations, want) {
				t.Errorf("store delegations = %+v, want %+v", store.delegations, want)
			}
		})
	}
}

func TestLockedReauthorizationRefusalsAreReturnedAndAudited(t *testing.T) {
	t.Parallel()
	refusals := map[string]error{
		"create user":  identity.ErrActorNotAuthorized,
		"disable user": identity.ErrActorNotAuthorized,
		"assign role":  identity.ErrPrivilegeEscalation,
		"update role":  identity.ErrPrivilegeEscalation,
		"delete role":  identity.ErrActorNotAuthorized,
	}
	for _, scenario := range delegationScenarios() {
		refusal, refused := refusals[scenario.name]
		if !refused {
			continue
		}
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			store, auditor, users, roles := newDelegationServices(t)
			store.lockedRefusal = refusal
			if err := scenario.run(context.Background(), users, roles); !errors.Is(err, refusal) {
				t.Fatalf("%s = %v, want %v", scenario.name, err, refusal)
			}
			if len(store.committedEvents) != 0 {
				t.Errorf("a refused mutation committed %+v", store.committedEvents)
			}
			want := []refusalRecord{{action: scenario.action, outcome: audit.OutcomeFailed, actor: actorAdmin, reasonRecorded: true}}
			if got := refusalRecords(auditor.events); !slices.Equal(got, want) {
				t.Errorf("refusal events = %+v, want %+v", got, want)
			}
		})
	}
}
