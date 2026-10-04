package identity_test

import (
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

func TestValidateDelegationAcceptsPermissionsTheActorHolds(t *testing.T) {
	t.Parallel()
	actor := []identity.Permission{identity.PermissionUsersUpdate, identity.PermissionUsersDisable, identity.PermissionAuditList}
	cases := map[string][][]identity.Permission{
		"strict subset":               {{identity.PermissionAuditList}},
		"identical set":               {{identity.PermissionAuditList, identity.PermissionUsersDisable, identity.PermissionUsersUpdate}},
		"empty role":                  {{}},
		"old and new sets both held":  {{identity.PermissionUsersUpdate}, {identity.PermissionAuditList}},
		"no affected sets":            nil,
		"duplicated keys in the role": {{identity.PermissionAuditList, identity.PermissionAuditList}},
	}
	for name, affected := range cases {
		if err := identity.ValidateDelegation(actor, affected...); err != nil {
			t.Errorf("%s: ValidateDelegation = %v, want nil", name, err)
		}
	}
}

func TestValidateDelegationRefusesPermissionsTheActorLacks(t *testing.T) {
	t.Parallel()
	actor := []identity.Permission{identity.PermissionUsersUpdate, identity.PermissionAuditList}
	cases := map[string][][]identity.Permission{
		"one extra key":                   {{identity.PermissionAuditList, identity.PermissionUsersDisable}},
		"held new set but richer old set": {{identity.PermissionRolesDelete}, {identity.PermissionAuditList}},
		"case-changed key":                {{"Audit.List"}},
		"padded key":                      {{"audit.list "}},
		"second set adds an approval key": {{identity.PermissionAuditList}, {identity.PermissionUsersUpdate, identity.PermissionRequestsApprove}},
	}
	for name, affected := range cases {
		if err := identity.ValidateDelegation(actor, affected...); !errors.Is(err, identity.ErrPrivilegeEscalation) {
			t.Errorf("%s: ValidateDelegation = %v, want ErrPrivilegeEscalation", name, err)
		}
	}
	if err := identity.ValidateDelegation(nil, []identity.Permission{identity.PermissionAuditGet}); !errors.Is(err, identity.ErrPrivilegeEscalation) {
		t.Errorf("empty actor: ValidateDelegation = %v, want ErrPrivilegeEscalation", err)
	}
}

func TestIsAdministratorRequiresEveryAdministratorPermission(t *testing.T) {
	t.Parallel()
	administrators := map[string][]identity.Permission{
		"exactly the administrator keys": identity.AdministratorPermissions(),
		"full catalog":                   identity.EnforcedPermissions(),
		"reordered with extras":          {identity.PermissionAuditGet, identity.PermissionUsersDisable, identity.PermissionUsersUpdate},
	}
	for name, permissions := range administrators {
		if !identity.IsAdministrator(permissions) {
			t.Errorf("%s: IsAdministrator = false, want true", name)
		}
	}
	others := map[string][]identity.Permission{
		"update only":          {identity.PermissionUsersUpdate},
		"disable only":         {identity.PermissionUsersDisable, identity.PermissionRolesUpdate},
		"no permissions":       nil,
		"look-alike user keys": {"users.update ", "Users.Disable"},
	}
	for name, permissions := range others {
		if identity.IsAdministrator(permissions) {
			t.Errorf("%s: IsAdministrator = true, want false", name)
		}
	}
}

func TestValidateNotSelfRefusesOnlyTheActorsOwnAccount(t *testing.T) {
	t.Parallel()
	for _, target := range []identity.UserID{"user-2", "USER-1", ""} {
		if err := identity.ValidateNotSelf("user-1", target); err != nil {
			t.Errorf("target %q: ValidateNotSelf = %v, want nil", target, err)
		}
	}
	if err := identity.ValidateNotSelf("user-1", "user-1"); !errors.Is(err, identity.ErrSelfAdministration) {
		t.Errorf("own account: ValidateNotSelf = %v, want ErrSelfAdministration", err)
	}
}

func TestDelegationAuthorizeAdmitsAnActiveActorStillHoldingItsAuthority(t *testing.T) {
	t.Parallel()
	held := []identity.Permission{identity.PermissionUsersUpdate, identity.PermissionUsersDisable, identity.PermissionAuditList}
	delegation := identity.Delegation{Actor: "actor-1", Gate: identity.PermissionUsersUpdate}
	cases := map[string][][]identity.Permission{
		"gate alone":                      nil,
		"target role held":                {{identity.PermissionAuditList}},
		"current and new roles both held": {{identity.PermissionAuditList}, {identity.PermissionUsersDisable}},
		"empty affected role":             {{}},
	}
	for name, affected := range cases {
		if err := delegation.Authorize(true, held, affected...); err != nil {
			t.Errorf("%s: Authorize = %v, want nil", name, err)
		}
	}
}

func TestDelegationAuthorizeRefusesAnActorWhoseAuthorityChanged(t *testing.T) {
	t.Parallel()
	held := []identity.Permission{identity.PermissionUsersUpdate, identity.PermissionAuditList}
	delegation := identity.Delegation{Actor: "actor-1", Gate: identity.PermissionUsersUpdate}
	cases := []struct {
		name     string
		active   bool
		held     []identity.Permission
		affected [][]identity.Permission
		want     error
	}{
		{name: "actor disabled meanwhile", active: false, held: held, want: identity.ErrActorNotAuthorized},
		{name: "actor lost the gate permission", active: true, held: []identity.Permission{identity.PermissionAuditList}, want: identity.ErrActorNotAuthorized},
		{name: "actor lost every permission", active: true, held: nil, want: identity.ErrActorNotAuthorized},
		{name: "target promoted beyond the actor", active: true, held: held, affected: [][]identity.Permission{{identity.PermissionUsersDisable}}, want: identity.ErrPrivilegeEscalation},
		{name: "new role grants what the actor lacks", active: true, held: held, affected: [][]identity.Permission{{identity.PermissionAuditList}, {identity.PermissionRolesDelete}}, want: identity.ErrPrivilegeEscalation},
	}
	for _, scenario := range cases {
		if err := delegation.Authorize(scenario.active, scenario.held, scenario.affected...); !errors.Is(err, scenario.want) {
			t.Errorf("%s: Authorize = %v, want %v", scenario.name, err, scenario.want)
		}
	}
}
