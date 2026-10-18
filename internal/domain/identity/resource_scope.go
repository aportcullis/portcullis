package identity

import "errors"

// ResourceType names the server-resolved resources supported by the initial binding contract.
type ResourceType string

const (
	ResourceOrganization ResourceType = "organization"
	ResourceConnection   ResourceType = "connection"
	ResourcePolicy       ResourceType = "policy"
	ResourceRequest      ResourceType = "request"
	ResourceExecution    ResourceType = "execution"
	ResourceResult       ResourceType = "result"
	ResourceUser         ResourceType = "user"
	ResourceRole         ResourceType = "role"
	ResourceBinding      ResourceType = "binding"
	ResourceSettings     ResourceType = "settings"
	ResourceAudit        ResourceType = "audit"
)

// ScopeType distinguishes organization and exact-connection assignments.
type ScopeType string

const (
	ScopeOrganization ScopeType = "organization"
	ScopeConnection   ScopeType = "connection"
)

// ErrInvalidResourceScope means a resource reference or binding scope has an unsupported shape.
var ErrInvalidResourceScope = errors.New("identity: invalid resource scope")

// ResourceReference carries trusted identity and ancestry, never client-claimed ancestry.
type ResourceReference struct {
	OrganizationID OrganizationID
	Type           ResourceType
	ID             string
	ConnectionID   string
}

// ResourceScope is data selecting an organization or one connection inside the binding's organization.
type ResourceScope struct {
	Type         ScopeType
	ConnectionID string
}

// Validate checks that the resource type and ancestry form a supported reference.
func (resource ResourceReference) Validate() error {
	if resource.OrganizationID == "" || resource.ID == "" {
		return ErrInvalidResourceScope
	}
	switch resource.Type {
	case ResourceOrganization:
		if resource.ID != string(resource.OrganizationID) || resource.ConnectionID != "" {
			return ErrInvalidResourceScope
		}
	case ResourceConnection:
		if resource.ConnectionID != resource.ID {
			return ErrInvalidResourceScope
		}
	case ResourcePolicy, ResourceRequest, ResourceExecution, ResourceResult:
		if resource.ConnectionID == "" {
			return ErrInvalidResourceScope
		}
	case ResourceUser, ResourceRole, ResourceBinding, ResourceSettings, ResourceAudit:
		if resource.ConnectionID != "" {
			return ErrInvalidResourceScope
		}
	default:
		return ErrInvalidResourceScope
	}
	return nil
}

// Validate rejects unknown scopes and ambiguous connection selectors.
func (scope ResourceScope) Validate() error {
	if (scope.Type == ScopeOrganization && scope.ConnectionID == "") || (scope.Type == ScopeConnection && scope.ConnectionID != "") {
		return nil
	}
	return ErrInvalidResourceScope
}

// Matches checks resource shape and scope without evaluating role permissions or conditions.
func (scope ResourceScope) Matches(resource ResourceReference) bool {
	if scope.Validate() != nil || resource.Validate() != nil {
		return false
	}
	return scope.Type == ScopeOrganization || resource.ConnectionID == scope.ConnectionID
}
