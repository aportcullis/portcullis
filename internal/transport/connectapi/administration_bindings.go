package connectapi

import (
	"connectrpc.com/connect"
	"context"
	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// bindingPolicyApp exposes versioned grant mutations and source validation.
type bindingPolicyApp interface {
	Create(context.Context, identity.OrganizationID, identity.UserID, identity.BindingDefinition) (administration.BindingMutation, error)
	Update(context.Context, identity.OrganizationID, identity.UserID, identity.BindingID, int64, identity.BindingUpdate) (administration.BindingMutation, error)
	Revoke(context.Context, identity.OrganizationID, identity.UserID, identity.BindingID, int64) (administration.BindingMutation, error)
	ValidateCondition(context.Context, identity.OrganizationID, identity.UserID, identity.Condition) error
}

// BindingsService exposes versioned grant management.
type BindingsService struct {
	access  managementAccess
	service bindingPolicyApp
}

// NewBindingsService injects binding use cases and authorized queries.
func NewBindingsService(org identity.OrganizationID, service bindingPolicyApp, reader managementReader) *BindingsService {
	return &BindingsService{access: managementAccess{organization: org, reader: reader}, service: service}
}

// List returns bounded grant summaries without source expressions.
func (s *BindingsService) List(ctx context.Context, request *connect.Request[portcullisv1.ListBindingsRequest]) (*connect.Response[portcullisv1.ListBindingsResponse], error) {
	read, err := s.access.readManagement(ctx, managementPage(identity.ResourceBinding, request.Msg.GetPagination()))
	if err != nil {
		return nil, err
	}
	bindings := make([]*portcullisv1.BindingSummary, 0, len(read.Bindings))
	for _, binding := range read.Bindings {
		bindings = append(bindings, toProtoBindingSummary(binding))
	}
	return connect.NewResponse(&portcullisv1.ListBindingsResponse{Bindings: bindings, Pagination: managementPageInfo(read)}), nil
}

// Get preserves complete condition data for authorized inspection.
func (s *BindingsService) Get(ctx context.Context, request *connect.Request[portcullisv1.GetBindingRequest]) (*connect.Response[portcullisv1.GetBindingResponse], error) {
	identifier, err := managementID(ctx, request.Msg.GetId())
	if err != nil {
		return nil, err
	}
	read, err := s.access.readManagement(ctx, administration.ManagementQuery{Kind: identity.ResourceBinding, ID: identifier})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&portcullisv1.GetBindingResponse{Binding: toProtoManagedBinding(read.Bindings[0]), AuthorizationRevision: read.Revision}), nil
}

// Create grants a role only after transactional delegation and recovery checks.
func (s *BindingsService) Create(ctx context.Context, request *connect.Request[portcullisv1.CreateBindingRequest]) (*connect.Response[portcullisv1.CreateBindingResponse], error) {
	actor, err := managementActor(ctx)
	if err != nil {
		return nil, err
	}
	user, err := managementID(ctx, request.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	role, err := managementID(ctx, request.Msg.GetRoleId())
	if err != nil {
		return nil, err
	}
	scope, err := parseManagementScope(ctx, request.Msg.GetScope())
	if err != nil {
		return nil, err
	}
	definition := identity.BindingDefinition{UserID: identity.UserID(user), RoleID: identity.RoleID(role), Scope: scope}
	if request.Msg.Condition != nil {
		definition.HasCondition = true
		definition.Condition = managementCondition(request.Msg.Condition)
	}
	mutation, err := s.service.Create(ctx, s.access.organization, actor, definition)
	if err != nil {
		return nil, managementError(err, false)
	}
	return connect.NewResponse(&portcullisv1.CreateBindingResponse{Binding: toProtoPolicyVersion(string(mutation.Binding.ID), mutation.Binding.Version), AuthorizationRevision: mutation.Revision}), nil
}

// Update preserves omitted condition edits and refuses ambiguous explicit removal.
func (s *BindingsService) Update(ctx context.Context, request *connect.Request[portcullisv1.UpdateBindingRequest]) (*connect.Response[portcullisv1.UpdateBindingResponse], error) {
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
	update := identity.BindingUpdate{}
	if request.Msg.RoleId != nil {
		role, err := managementID(ctx, request.Msg.GetRoleId())
		if err != nil {
			return nil, err
		}
		update.RoleID = identity.RoleID(role)
	}
	if request.Msg.Scope != nil {
		scope, err := parseManagementScope(ctx, request.Msg.Scope)
		if err != nil {
			return nil, err
		}
		update.Scope = &scope
	}
	switch edit := request.Msg.GetConditionEdit().(type) {
	case nil:
	case *portcullisv1.UpdateBindingRequest_ReplaceCondition:
		update.ConditionMode = identity.ConditionReplace
		update.Condition = managementCondition(edit.ReplaceCondition)
	case *portcullisv1.UpdateBindingRequest_ClearCondition:
		if !edit.ClearCondition {
			return nil, managementError(identity.ErrInvalidCondition, false)
		}
		update.ConditionMode = identity.ConditionClear
	default:
		return nil, managementError(identity.ErrInvalidCondition, false)
	}
	mutation, err := s.service.Update(ctx, s.access.organization, actor, identity.BindingID(identifier), request.Msg.GetExpectedVersion(), update)
	if err != nil {
		return nil, managementError(err, false)
	}
	return connect.NewResponse(&portcullisv1.UpdateBindingResponse{Binding: toProtoPolicyVersion(string(mutation.Binding.ID), mutation.Binding.Version), AuthorizationRevision: mutation.Revision}), nil
}

// Revoke appends a terminal grant version with the current condition evidence.
func (s *BindingsService) Revoke(ctx context.Context, request *connect.Request[portcullisv1.RevokeBindingRequest]) (*connect.Response[portcullisv1.RevokeBindingResponse], error) {
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
	mutation, err := s.service.Revoke(ctx, s.access.organization, actor, identity.BindingID(identifier), request.Msg.GetExpectedVersion())
	if err != nil {
		return nil, managementError(err, false)
	}
	return connect.NewResponse(&portcullisv1.RevokeBindingResponse{Binding: toProtoPolicyVersion(string(mutation.Binding.ID), mutation.Binding.Version), AuthorizationRevision: mutation.Revision}), nil
}

// ValidateCondition applies the write-time CEL preparation contract without a policy mutation.
func (s *BindingsService) ValidateCondition(ctx context.Context, request *connect.Request[portcullisv1.ValidateBindingConditionRequest]) (*connect.Response[portcullisv1.ValidateBindingConditionResponse], error) {
	actor, err := managementActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.service.ValidateCondition(ctx, s.access.organization, actor, managementCondition(request.Msg.GetCondition())); err != nil {
		return nil, managementError(err, false)
	}
	return connect.NewResponse(&portcullisv1.ValidateBindingConditionResponse{}), nil
}

// parseManagementScope rejects ambiguous selectors and noncanonical connection identities.
func parseManagementScope(ctx context.Context, source *portcullisv1.BindingScope) (identity.ResourceScope, error) {
	scope := identity.ResourceScope{Type: identity.ScopeType(source.GetType()), ConnectionID: source.GetConnectionId()}
	if scope.Validate() != nil {
		return identity.ResourceScope{}, managementError(identity.ErrInvalidResourceScope, false)
	}
	if scope.Type == identity.ScopeConnection {
		if _, err := managementID(ctx, scope.ConnectionID); err != nil {
			return identity.ResourceScope{}, err
		}
	}
	return scope, nil
}

// managementCondition preserves every supplied source and contract field.
func managementCondition(source *portcullisv1.BindingCondition) identity.Condition {
	return identity.Condition{Title: source.GetTitle(), Description: source.GetDescription(), Expression: source.GetExpression(), LanguageVersion: source.GetLanguageVersion(), EnvironmentVersion: source.GetEnvironmentVersion()}
}

// toProtoBindingSummary omits protected condition source while retaining explicit presence.
func toProtoBindingSummary(binding identity.RoleBinding) *portcullisv1.BindingSummary {
	return &portcullisv1.BindingSummary{Id: string(binding.ID), UserId: string(binding.UserID), RoleId: string(binding.RoleID), Version: binding.Version, Scope: &portcullisv1.BindingScope{Type: string(binding.Scope.Type), ConnectionId: binding.Scope.ConnectionID}, HasCondition: binding.HasCondition, Revoked: binding.Revoked}
}

// toProtoManagedBinding retains source verbatim, including future contracts, for authorized detail responses.
func toProtoManagedBinding(binding identity.RoleBinding) *portcullisv1.ManagedBinding {
	result := &portcullisv1.ManagedBinding{Summary: toProtoBindingSummary(binding)}
	if binding.HasCondition {
		result.Condition = &portcullisv1.BindingCondition{Title: binding.Condition.Title, Description: binding.Condition.Description, Expression: binding.Condition.Expression, LanguageVersion: binding.Condition.LanguageVersion, EnvironmentVersion: binding.Condition.EnvironmentVersion}
	}
	return result
}
