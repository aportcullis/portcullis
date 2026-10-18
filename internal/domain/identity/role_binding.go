package identity

import "errors"

// BindingID identifies a role assignment independently of membership and role identity.
type BindingID string

// Membership records organization participation without assigning a role.
type Membership struct {
	OrganizationID OrganizationID
	UserID         UserID
}

// ErrInvalidRoleBinding means a binding snapshot is incomplete or invalid.
var ErrInvalidRoleBinding = errors.New("identity: invalid role binding")

// RoleBinding is one version of a structured grant; condition absence is explicit.
type RoleBinding struct {
	ID             BindingID
	OrganizationID OrganizationID
	UserID         UserID
	RoleID         RoleID
	Version        int64
	Scope          ResourceScope
	Condition      Condition
	HasCondition   bool
	Revoked        bool
}

// Validate checks complete binding data without granting access or running an expression.
func (binding RoleBinding) Validate() error {
	if binding.ID == "" || binding.OrganizationID == "" || binding.Version <= 0 {
		return ErrInvalidRoleBinding
	}
	return binding.Definition().Validate()
}

// AppliesTo checks the principal, organization, live state and typed resource scope only.
func (binding RoleBinding) AppliesTo(principal UserID, resource ResourceReference) bool {
	return binding.Validate() == nil && !binding.Revoked && binding.UserID == principal && binding.OrganizationID == resource.OrganizationID && binding.Scope.Matches(resource)
}
