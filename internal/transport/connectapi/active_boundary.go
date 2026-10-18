package connectapi

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type operationDeclaration struct {
	kind                                     identity.ResourceType
	permission                               identity.Permission
	idField                                  string
	collection, resolveResult, protectOutput bool
}

// activeOperationDeclarations binds each protected procedure to a server-owned target vocabulary.
var activeOperationDeclarations = map[string]operationDeclaration{
	"Connections/List":                          {identity.ResourceConnection, identity.PermissionConnectionsList, "", true, false, true},
	"Connections/Get":                           {identity.ResourceConnection, identity.PermissionConnectionsGet, "id", false, false, true},
	"Connections/Create":                        {identity.ResourceOrganization, identity.PermissionConnectionsCreate, "", false, false, true},
	"Connections/Update":                        {identity.ResourceConnection, identity.PermissionConnectionsUpdate, "id", false, false, true},
	"Connections/Archive":                       {identity.ResourceConnection, identity.PermissionConnectionsDelete, "id", false, false, true},
	"Connections/Test":                          {identity.ResourceConnection, identity.PermissionConnectionsTest, "id", false, false, true},
	"ConnectionPolicies/Get":                    {identity.ResourcePolicy, identity.PermissionPoliciesGet, "connection_id", false, false, true},
	"ConnectionPolicies/Update":                 {identity.ResourcePolicy, identity.PermissionPoliciesUpdate, "connection_id", false, false, true},
	"AccessRequests/List":                       {identity.ResourceRequest, identity.PermissionRequestsList, "", true, false, true},
	"AccessRequests/Get":                        {identity.ResourceRequest, identity.PermissionRequestsGet, "id", false, false, true},
	"AccessRequests/Create":                     {identity.ResourceConnection, identity.PermissionRequestsCreate, "connection_id", false, false, true},
	"AccessRequests/UpdateDraft":                {identity.ResourceRequest, identity.PermissionRequestsCreate, "id", false, false, true},
	"AccessRequests/Submit":                     {identity.ResourceRequest, identity.PermissionRequestsCreate, "id", false, false, true},
	"AccessRequests/Cancel":                     {identity.ResourceRequest, identity.PermissionRequestsCreate, "id", false, false, true},
	"AccessRequests/Approve":                    {identity.ResourceRequest, identity.PermissionRequestsApprove, "id", false, false, true},
	"AccessRequests/Reject":                     {identity.ResourceRequest, identity.PermissionRequestsReject, "id", false, false, true},
	"AccessRequests/ListRequestableConnections": {identity.ResourceConnection, identity.PermissionRequestsCreate, "", true, false, true},
	"QueryExecutions/Execute":                   {identity.ResourceRequest, identity.PermissionRequestsExecute, "request_id", false, false, true},
	"QueryExecutions/Cancel":                    {identity.ResourceExecution, identity.PermissionRequestsExecute, "request_id", false, false, false},
	"QueryExecutions/Get":                       {identity.ResourceExecution, identity.PermissionRequestsGet, "request_id", false, false, true},
	"QueryExecutions/GetResult":                 {identity.ResourceResult, identity.PermissionRequestsGet, "request_id", false, true, true},
	"QueryExecutions/ExportCSV":                 {identity.ResourceResult, identity.PermissionRequestsGet, "request_id", false, true, true},
	"Audit/List":                                {identity.ResourceAudit, identity.PermissionAuditList, "", true, false, true},
	"Audit/Get":                                 {identity.ResourceAudit, identity.PermissionAuditGet, "id", false, false, true},
	"Users/List":                                {identity.ResourceOrganization, identity.PermissionUsersList, "", false, false, true},
	"Users/Get":                                 {identity.ResourceUser, identity.PermissionUsersGet, "id", false, false, true},
	"Roles/List":                                {identity.ResourceOrganization, identity.PermissionRolesList, "", false, false, true},
	"Roles/Get":                                 {identity.ResourceRole, identity.PermissionRolesGet, "id", false, false, true},
	"Roles/ListPermissions":                     {identity.ResourceOrganization, identity.PermissionRolesList, "", false, false, true},
	"Bindings/List":                             {identity.ResourceOrganization, identity.PermissionBindingsList, "", false, false, true},
	"Bindings/Get":                              {identity.ResourceBinding, identity.PermissionBindingsGet, "id", false, false, true},
}

// operationForMessage takes IDs only from the declared field, never browser-supplied ancestry or authority.
func operationForMessage(ctx context.Context, procedure string, message any) (context.Context, operationDeclaration, bool) {
	declaration, exists := activeOperationDeclarations[strings.TrimPrefix(procedure, "/portcullis.v1.")]
	user, present := userFromContext(ctx)
	if !exists || !present {
		return ctx, declaration, false
	}
	identifier := ""
	if declaration.idField != "" {
		if body, ok := message.(proto.Message); ok {
			reflection := body.ProtoReflect()
			field := reflection.Descriptor().Fields().ByName(protoreflect.Name(declaration.idField))
			if field != nil && field.Kind() == protoreflect.StringKind {
				identifier = reflection.Get(field).String()
			}
		}
		if parsed, err := uuid.Parse(identifier); err == nil {
			identifier = parsed.String()
		}
	}
	if strings.HasSuffix(procedure, "Connections/Test") && identifier == "" {
		declaration.kind = identity.ResourceOrganization
	}
	operation := authz.Operation{Principal: user.ID, Type: declaration.kind, ID: identifier, Collection: declaration.collection, ResolveResult: declaration.resolveResult, Permission: declaration.permission}
	return authz.WithOperation(ctx, operation), declaration, true
}

// NewActiveBoundaryInterceptor establishes purpose and rechecks protected unary output after use-case processing.
func NewActiveBoundaryInterceptor(authorization authorizer, sessions sessionAuthenticator) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
			scoped, declaration, declared := operationForMessage(ctx, request.Spec().Procedure, request.Any())
			if declared && declaration.idField != "" {
				operation, _ := authz.OperationFromContext(scoped)
				if operation.Type != identity.ResourceOrganization {
					if _, err := uuid.Parse(operation.ID); err != nil {
						user, _ := userFromContext(scoped)
						operation.Collection = true
						if err := authorization.Authorize(authz.WithOperation(scoped, operation), user, declaration.permission); err != nil {
							return nil, activeAuthorizationError(err)
						}
						return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid resource id"))
					}
				}
			}
			scoped = authz.WithOutputFence(scoped)
			response, err := next(scoped, request)
			if err != nil {
				return nil, err
			}
			currentSession := request.Spec().Procedure == portcullisv1connect.AuthMeProcedure
			if (!declared || !declaration.protectOutput) && !currentSession {
				return response, nil
			}
			if err := authz.CheckOutput(scoped); err != nil {
				return nil, activeAuthorizationError(err)
			}
			if !currentSession {
				if err := requirePermission(scoped, authorization, declaration.permission); err != nil {
					return nil, err
				}
			}
			_, current, err := sessions.Authenticate(scoped, sessionTokenFromContext(scoped))
			if err != nil {
				return nil, sessionAuthenticationError(err)
			}
			if err := sessions.SlideIdle(scoped, current); err != nil {
				return nil, sessionAuthenticationError(err)
			}
			return response, nil
		}
	}
}

// activeAuthorizationError omits storage, predicate and inaccessible resource diagnostics.
func activeAuthorizationError(err error) error {
	if errors.Is(err, query.ErrResultUnavailable) {
		return executionError(err)
	}
	return managementError(err, false)
}
