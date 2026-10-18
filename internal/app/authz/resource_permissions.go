package authz

import "github.com/aportcullis/portcullis/internal/domain/identity"

// resourcePermissionTypes declares concrete enforcement targets; collection verbs apply per candidate.
var resourcePermissionTypes = map[identity.Permission][]identity.ResourceType{
	identity.PermissionConnectionsList:   {identity.ResourceConnection},
	identity.PermissionConnectionsGet:    {identity.ResourceConnection},
	identity.PermissionConnectionsCreate: {identity.ResourceOrganization},
	identity.PermissionConnectionsUpdate: {identity.ResourceConnection},
	identity.PermissionConnectionsDelete: {identity.ResourceConnection},
	identity.PermissionConnectionsTest:   {identity.ResourceConnection, identity.ResourceOrganization},
	identity.PermissionPoliciesGet:       {identity.ResourcePolicy},
	identity.PermissionPoliciesUpdate:    {identity.ResourcePolicy},
	identity.PermissionRequestsList:      {identity.ResourceRequest},
	identity.PermissionRequestsGet:       {identity.ResourceRequest, identity.ResourceExecution, identity.ResourceResult},
	identity.PermissionRequestsCreate:    {identity.ResourceConnection, identity.ResourceRequest},
	identity.PermissionRequestsApprove:   {identity.ResourceRequest},
	identity.PermissionRequestsReject:    {identity.ResourceRequest},
	identity.PermissionRequestsExecute:   {identity.ResourceRequest, identity.ResourceExecution},
	identity.PermissionUsersList:         {identity.ResourceOrganization},
	identity.PermissionUsersCreate:       {identity.ResourceOrganization},
	identity.PermissionUsersGet:          {identity.ResourceUser},
	identity.PermissionUsersUpdate:       {identity.ResourceUser},
	identity.PermissionUsersDisable:      {identity.ResourceUser},
	identity.PermissionRolesList:         {identity.ResourceOrganization},
	identity.PermissionRolesCreate:       {identity.ResourceOrganization},
	identity.PermissionRolesGet:          {identity.ResourceRole},
	identity.PermissionRolesUpdate:       {identity.ResourceRole},
	identity.PermissionRolesDelete:       {identity.ResourceRole},
	identity.PermissionAuditList:         {identity.ResourceAudit},
	identity.PermissionAuditGet:          {identity.ResourceAudit},
}

// supportsResourcePermission denies unregistered permission and resource combinations.
func supportsResourcePermission(permission identity.Permission, resource identity.ResourceType) bool {
	for _, supported := range resourcePermissionTypes[permission] {
		if supported == resource {
			return true
		}
	}
	return false
}

// requiresUnconditionalManagement identifies authorization-policy writes with unrestricted authority.
func requiresUnconditionalManagement(permission identity.Permission) bool {
	switch permission {
	case identity.PermissionUsersCreate, identity.PermissionUsersUpdate, identity.PermissionUsersDisable, identity.PermissionRolesCreate, identity.PermissionRolesUpdate, identity.PermissionRolesDelete:
		return true
	default:
		return false
	}
}
