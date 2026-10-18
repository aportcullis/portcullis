package connectapi

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// managementReader supplies coherently authorized administration data.
type managementReader interface {
	Read(context.Context, identity.OrganizationID, identity.UserID, administration.ManagementQuery) (administration.ManagementRead, error)
}

// managementAccess pins organization identity outside all client messages.
type managementAccess struct {
	organization identity.OrganizationID
	reader       managementReader
}

// readManagement authenticates the caller before querying authorized data.
func (access managementAccess) readManagement(ctx context.Context, query administration.ManagementQuery) (administration.ManagementRead, error) {
	actor, err := managementActor(ctx)
	if err != nil {
		return administration.ManagementRead{}, err
	}
	result, err := access.reader.Read(ctx, access.organization, actor, query)
	if err != nil {
		return administration.ManagementRead{}, managementError(err, query.ID != "")
	}
	return result, nil
}

// managementActor accepts only the server-authenticated principal.
func managementActor(ctx context.Context) (identity.UserID, error) {
	user, exists := userFromContext(ctx)
	if !exists || user.ID == "" {
		return "", newAuthenticationRequiredError()
	}
	return user.ID, nil
}

// managementID requires a canonical UUID before lock keys or identity comparisons can use it.
func managementID(ctx context.Context, raw string) (string, error) {
	if _, err := managementActor(ctx); err != nil {
		return "", err
	}
	identifier, err := uuid.Parse(raw)
	if err != nil || identifier.String() != raw {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("invalid resource id"))
	}
	return raw, nil
}

// managementPage translates optional numbered pagination without adding authority fields.
func managementPage(kind identity.ResourceType, page *portcullisv1.AdministrationPage) administration.ManagementQuery {
	return administration.ManagementQuery{Kind: kind, Page: page.GetPage(), PageSize: page.GetPageSize(), ExpectedRevision: page.GetExpectedRevision()}
}

// managementPageInfo copies one coherent authorized page decision.
func managementPageInfo(read administration.ManagementRead) *portcullisv1.AdministrationPageInfo {
	return &portcullisv1.AdministrationPageInfo{Page: read.Page, PageSize: read.PageSize, TotalCount: uint64(read.TotalCount), AuthorizationRevision: read.Revision, EvaluatedAt: timestamppb.New(read.EvaluatedAt)}
}

// managementError keeps source text, storage details and inaccessible identities outside the wire contract.
func managementError(err error, detail bool) error {
	switch {
	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, errors.New("request canceled"))
	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, errors.New("request deadline exceeded"))
	case errors.Is(err, authz.ErrPermissionDenied):
		if detail {
			return connect.NewError(connect.CodeNotFound, errors.New("resource not found"))
		}
		return connect.NewError(connect.CodePermissionDenied, errors.New("permission denied"))
	case errors.Is(err, identity.ErrActorNotAuthorized), errors.Is(err, identity.ErrPrivilegeEscalation), errors.Is(err, identity.ErrSelfAdministration):
		return connect.NewError(connect.CodePermissionDenied, errors.New("permission denied"))
	case errors.Is(err, identity.ErrUserNotFound), errors.Is(err, identity.ErrRoleNotFound), errors.Is(err, identity.ErrBindingNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("resource not found"))
	case errors.Is(err, identity.ErrRoleConflict), errors.Is(err, identity.ErrBindingConflict):
		return connect.NewError(connect.CodeAborted, errors.New("resource changed; reload current state"))
	case errors.Is(err, identity.ErrLastAdministrator), errors.Is(err, identity.ErrSystemRoleImmutable), errors.Is(err, identity.ErrRoleInUse), errors.Is(err, identity.ErrBindingRevoked), errors.Is(err, identity.ErrUserDisabled), errors.Is(err, identity.ErrPasswordAlreadySet):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("administration change refused"))
	case errors.Is(err, identity.ErrEmailTaken), errors.Is(err, identity.ErrRoleNameTaken):
		return connect.NewError(connect.CodeAlreadyExists, errors.New("resource already exists"))
	case errors.Is(err, identity.ErrInvalidEmail), errors.Is(err, identity.ErrInvalidDisplayName), errors.Is(err, identity.ErrInvalidRoleName), errors.Is(err, identity.ErrPermissionNotInCatalog), errors.Is(err, identity.ErrInvalidRoleBinding), errors.Is(err, identity.ErrInvalidResourceScope), errors.Is(err, identity.ErrInvalidCondition), errors.Is(err, administration.ErrInvalidManagementPage):
		return connect.NewError(connect.CodeInvalidArgument, errors.New("invalid administration request"))
	default:
		return newServerFaultError(connect.CodeUnavailable, "temporarily unavailable", err)
	}
}

// AdministrationRoute holds one generated management service mount.
type AdministrationRoute struct {
	Pattern string
	Handler http.Handler
}

// NewAdministrationRoutes applies no-store headers to successful and refused management responses.
func NewAdministrationRoutes(users *UsersService, roles *RolesService, bindings *BindingsService, options ...connect.HandlerOption) []AdministrationRoute {
	userPath, userHandler := portcullisv1connect.NewUsersHandler(users, options...)
	rolePath, roleHandler := portcullisv1connect.NewRolesHandler(roles, options...)
	bindingPath, bindingHandler := portcullisv1connect.NewBindingsHandler(bindings, options...)
	routes := []AdministrationRoute{{userPath, userHandler}, {rolePath, roleHandler}, {bindingPath, bindingHandler}}
	for index := range routes {
		handler := routes[index].Handler
		routes[index].Handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Cache-Control", "no-store")
			handler.ServeHTTP(writer, request)
		})
	}
	return routes
}

// NewPendingAdministrationInterceptor preserves inactive production authority until atomic activation.
func NewPendingAdministrationInterceptor() connect.UnaryInterceptorFunc {
	return func(_ connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
			if _, err := managementActor(ctx); err != nil {
				return nil, err
			}
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("administration authorization is not active"))
		}
	}
}

// toProtoPolicyVersion returns only committed identity/version metadata to write callers.
func toProtoPolicyVersion(identifier string, version int64) *portcullisv1.PolicyVersion {
	return &portcullisv1.PolicyVersion{Id: identifier, Version: version}
}
