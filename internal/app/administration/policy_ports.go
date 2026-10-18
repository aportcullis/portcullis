package administration

import (
	"context"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// PolicyAuthorityTransaction supplies current unrestricted authority and recovery checks under the policy lock.
type PolicyAuthorityTransaction interface {
	LoadPermissionCatalog(context.Context) ([]identity.Permission, error)
	LockMemberStatus(context.Context, identity.UserID) (identity.UserStatus, error)
	LoadUnconditionalPermissions(context.Context, identity.UserID) ([]identity.Permission, error)
	HasRecoveryAdministrator(context.Context, []identity.Permission) (bool, error)
	ObservePolicyMutationTime(context.Context, []identity.UserID) (time.Time, error)
	FinalizePolicyChange(context.Context, []identity.UserID, audit.Event) (int64, error)
}

// AccountPolicyRepository keeps account mutations in one locked metadata transaction.
type AccountPolicyRepository interface {
	WithAccountPolicyTransaction(context.Context, identity.OrganizationID, func(AccountPolicyTransaction) error) error
}

// AccountPolicyTransaction administers independent memberships and all their live binding permissions.
type AccountPolicyTransaction interface {
	PolicyAuthorityTransaction
	LockPolicyAccount(context.Context, identity.UserID) (identity.PolicyAccount, error)
	LoadAccountBindingPermissions(context.Context, identity.UserID) ([]identity.Permission, error)
	InsertPolicyAccount(context.Context, identity.NewPolicyAccount) (identity.PolicyAccount, error)
	SetPolicyAccountStatus(context.Context, identity.UserID, identity.UserStatus) error
	ReplacePolicyPasswordSetup(context.Context, identity.UserID, identity.PasswordSetupIssue) (identity.PasswordSetup, error)
}

// RolePolicyRepository keeps shared role mutations in one locked metadata transaction.
type RolePolicyRepository interface {
	WithRolePolicyTransaction(context.Context, identity.OrganizationID, func(RolePolicyTransaction) error) error
}

// RolePolicyTransaction administers definitions independently from legacy membership assignments.
type RolePolicyTransaction interface {
	PolicyAuthorityTransaction
	LoadRole(context.Context, identity.RoleID) (identity.Role, error)
	LoadRoleBindingSubjects(context.Context, identity.RoleID, int) ([]identity.UserID, error)
	InsertPolicyRole(context.Context, identity.RoleDefinition) (identity.Role, error)
	UpdatePolicyRole(context.Context, identity.RoleID, int64, identity.RoleDefinition) (identity.Role, error)
	DeletePolicyRole(context.Context, identity.RoleID, int64) error
}
