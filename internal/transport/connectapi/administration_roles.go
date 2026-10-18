package connectapi

import (
	"connectrpc.com/connect"
	"context"
	"errors"
	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// rolePolicyApp exposes only the shared-role mutations consumed by Roles.
type rolePolicyApp interface {
	Create(context.Context, identity.OrganizationID, identity.UserID, administration.RoleParams) (identity.Role, error)
	Update(context.Context, identity.OrganizationID, identity.UserID, identity.RoleID, int64, administration.RoleParams) (identity.Role, error)
	Delete(context.Context, identity.OrganizationID, identity.UserID, identity.RoleID, int64) error
}

// RolesService exposes reusable definition management.
type RolesService struct {
	access  managementAccess
	service rolePolicyApp
}

// NewRolesService injects shared-role use cases and authorized queries.
func NewRolesService(org identity.OrganizationID, service rolePolicyApp, reader managementReader) *RolesService {
	return &RolesService{access: managementAccess{organization: org, reader: reader}, service: service}
}

// List returns summaries without permission bundles.
func (s *RolesService) List(ctx context.Context, request *connect.Request[portcullisv1.ListRolesRequest]) (*connect.Response[portcullisv1.ListRolesResponse], error) {
	read, err := s.access.readManagement(ctx, managementPage(identity.ResourceRole, request.Msg.GetPagination()))
	if err != nil {
		return nil, err
	}
	roles := make([]*portcullisv1.RoleSummary, 0, len(read.Roles))
	for _, role := range read.Roles {
		roles = append(roles, toProtoRoleSummary(role))
	}
	return connect.NewResponse(&portcullisv1.ListRolesResponse{Roles: roles, Pagination: managementPageInfo(read)}), nil
}

// Get returns a permission bundle under its specific read grant.
func (s *RolesService) Get(ctx context.Context, request *connect.Request[portcullisv1.GetRoleRequest]) (*connect.Response[portcullisv1.GetRoleResponse], error) {
	identifier, err := managementID(ctx, request.Msg.GetId())
	if err != nil {
		return nil, err
	}
	read, err := s.access.readManagement(ctx, administration.ManagementQuery{Kind: identity.ResourceRole, ID: identifier})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&portcullisv1.GetRoleResponse{Role: toProtoManagedRole(read.Roles[0]), AuthorizationRevision: read.Revision}), nil
}

// ListPermissions returns the current catalog only to authorized role-list readers.
func (s *RolesService) ListPermissions(ctx context.Context, _ *connect.Request[portcullisv1.ListRolePermissionsRequest]) (*connect.Response[portcullisv1.ListRolePermissionsResponse], error) {
	read, err := s.access.readManagement(ctx, administration.ManagementQuery{Kind: identity.ResourceRole, Catalog: true})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&portcullisv1.ListRolePermissionsResponse{Permissions: managementPermissionStrings(read.Permissions), AuthorizationRevision: read.Revision}), nil
}

// Create adds a validated custom permission bundle.
func (s *RolesService) Create(ctx context.Context, request *connect.Request[portcullisv1.CreateRoleRequest]) (*connect.Response[portcullisv1.CreateRoleResponse], error) {
	actor, err := managementActor(ctx)
	if err != nil {
		return nil, err
	}
	role, err := s.service.Create(ctx, s.access.organization, actor, administration.RoleParams{Name: request.Msg.GetName(), Permissions: managementPermissions(request.Msg.GetPermissions())})
	if err != nil {
		return nil, managementError(err, false)
	}
	return connect.NewResponse(&portcullisv1.CreateRoleResponse{Role: toProtoPolicyVersion(string(role.ID), role.Version)}), nil
}

// Update requires a current positive optimistic version.
func (s *RolesService) Update(ctx context.Context, request *connect.Request[portcullisv1.UpdateRoleRequest]) (*connect.Response[portcullisv1.UpdateRoleResponse], error) {
	identifier, err := managementID(ctx, request.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := requireManagementVersion(request.Msg.GetExpectedVersion()); err != nil {
		return nil, err
	}
	actor, err := managementActor(ctx)
	if err != nil {
		return nil, err
	}
	role, err := s.service.Update(ctx, s.access.organization, actor, identity.RoleID(identifier), request.Msg.GetExpectedVersion(), administration.RoleParams{Name: request.Msg.GetName(), Permissions: managementPermissions(request.Msg.GetPermissions())})
	if err != nil {
		return nil, managementError(err, false)
	}
	return connect.NewResponse(&portcullisv1.UpdateRoleResponse{Role: toProtoPolicyVersion(string(role.ID), role.Version)}), nil
}

// Delete refuses current grant references and immutable system roles.
func (s *RolesService) Delete(ctx context.Context, request *connect.Request[portcullisv1.DeleteRoleRequest]) (*connect.Response[portcullisv1.DeleteRoleResponse], error) {
	identifier, err := managementID(ctx, request.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := requireManagementVersion(request.Msg.GetExpectedVersion()); err != nil {
		return nil, err
	}
	actor, err := managementActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.service.Delete(ctx, s.access.organization, actor, identity.RoleID(identifier), request.Msg.GetExpectedVersion()); err != nil {
		return nil, managementError(err, false)
	}
	return connect.NewResponse(&portcullisv1.DeleteRoleResponse{}), nil
}

// requireManagementVersion rejects unguarded policy changes at the transport boundary.
func requireManagementVersion(version int64) error {
	if version <= 0 {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("expected_version is required"))
	}
	return nil
}

// managementPermissions translates submitted catalog keys without granting authority.
func managementPermissions(keys []string) []identity.Permission {
	permissions := make([]identity.Permission, 0, len(keys))
	for _, key := range keys {
		permissions = append(permissions, identity.Permission(key))
	}
	return permissions
}

// managementPermissionStrings serializes permission bundle data.
func managementPermissionStrings(permissions []identity.Permission) []string {
	keys := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		keys = append(keys, string(permission))
	}
	return keys
}

// toProtoRoleSummary exposes binding usage rather than legacy member assignments.
func toProtoRoleSummary(role identity.Role) *portcullisv1.RoleSummary {
	return &portcullisv1.RoleSummary{Id: string(role.ID), Name: role.Name, IsSystem: role.IsSystem, Version: role.Version, BindingCount: role.BindingCount}
}

// toProtoManagedRole includes permission details only on authorized detail responses.
func toProtoManagedRole(role identity.Role) *portcullisv1.ManagedRole {
	return &portcullisv1.ManagedRole{Summary: toProtoRoleSummary(role), Permissions: managementPermissionStrings(role.Permissions)}
}
