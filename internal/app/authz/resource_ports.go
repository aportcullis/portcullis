package authz

import (
	"context"
	"time"

	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// ResourceTarget identifies a resource without accepting client-supplied ancestry.
type ResourceTarget struct {
	OrganizationID identity.OrganizationID
	Type           identity.ResourceType
	ID             string
}

// BindingGrant pairs a current binding with the requested live role permission.
type BindingGrant struct {
	Binding     identity.RoleBinding
	Permission  identity.Permission
	RoleVersion int64
}

// AuthorizationSnapshot contains one coherent repository read, never an execution lease.
type AuthorizationSnapshot struct {
	ActiveMember bool
	Resource     identity.ResourceReference
	EvaluatedAt  time.Time
	Revision     int64
	Grants       []BindingGrant
}

// ResourceRepository resolves ancestry and bounded grants together with live membership and database time.
type ResourceRepository interface {
	LoadAuthorizationSnapshot(context.Context, identity.UserID, identity.Permission, ResourceTarget, int) (AuthorizationSnapshot, error)
}

// ResourceConditions prepares and evaluates binding predicates under operational limits.
type ResourceConditions interface {
	Prepare(context.Context, identity.Condition) (condition.CheckedCondition, error)
	Evaluate(context.Context, condition.CheckedCondition, condition.AttributeSnapshot) condition.Result
}
