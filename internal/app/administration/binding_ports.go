package administration

import (
	"context"
	"time"

	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// BindingRepository holds the organization policy lock through authorization, mutation and commit.
type BindingRepository interface {
	WithBindingTransaction(context.Context, identity.OrganizationID, func(BindingTransaction) error) error
}

// BindingTransaction exposes only the operations needed inside one locked policy mutation.
type BindingTransaction interface {
	ObserveBindingMutationTime(context.Context, identity.UserID) (time.Time, error)
	LoadPermissionCatalog(context.Context) ([]identity.Permission, error)
	LockMemberStatus(context.Context, identity.UserID) (identity.UserStatus, error)
	LoadUnconditionalPermissions(context.Context, identity.UserID) ([]identity.Permission, error)
	LoadRole(context.Context, identity.RoleID) (identity.Role, error)
	LockBindingVersion(context.Context, identity.BindingID) (identity.RoleBinding, error)
	LockBindingScope(context.Context, identity.ResourceScope) error
	InsertBinding(context.Context, identity.BindingDefinition) (identity.RoleBinding, error)
	AppendBindingVersion(context.Context, identity.RoleBinding, int64) error
	HasRecoveryAdministrator(context.Context, []identity.Permission) (bool, error)
	FinalizeBindingChange(context.Context, identity.UserID, audit.Event) (int64, error)
}

// BindingConditionPreparer validates source without granting authority through its predicate result.
type BindingConditionPreparer interface {
	Prepare(context.Context, identity.Condition) (condition.CheckedCondition, error)
}
