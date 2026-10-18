package administration

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// ManagementQuery selects organization administration data without accepting authority or ancestry.
type ManagementQuery struct {
	Kind             identity.ResourceType
	ID               string
	Page, PageSize   uint32
	ExpectedRevision int64
	Catalog          bool
}

// ManagementRead carries only authorized rows and their coherent page decision.
type ManagementRead struct {
	Users                []identity.PolicyAccount
	Roles                []identity.Role
	Bindings             []identity.RoleBinding
	Permissions          []identity.Permission
	Page, PageSize       uint32
	TotalCount, Revision int64
	EvaluatedAt          time.Time
}

// ManagementReadRepository owns one coherent transaction and bounded context for authorization and data.
type ManagementReadRepository interface {
	WithManagementRead(context.Context, identity.OrganizationID, func(context.Context, ManagementReadSnapshot) error) error
}

// ManagementReadSnapshot exposes bounded metadata reads inside one coherent authorization snapshot.
type ManagementReadSnapshot interface {
	authz.ResourceRepository
	LoadPermissionCatalog(context.Context) ([]identity.Permission, error)
	ListManagementIDs(context.Context, identity.ResourceType, int64, int32) ([]string, int64, error)
	GetPolicyAccount(context.Context, identity.UserID) (identity.PolicyAccount, error)
	GetPolicyRole(context.Context, identity.RoleID) (identity.Role, error)
	GetPolicyBinding(context.Context, identity.BindingID) (identity.RoleBinding, error)
}

// ErrInvalidManagementPage refuses ambiguous or stale list continuation.
var ErrInvalidManagementPage = errors.New("administration: invalid management page")

// ManagementReader evaluates scoped read authority before loading administration data.
type ManagementReader struct {
	repository    ManagementReadRepository
	conditions    authz.ResourceConditions
	settings      BindingConfig
	authorization authz.ResourceConfig
}

// NewManagementReader constructs the coherent administration query boundary.
func NewManagementReader(repository ManagementReadRepository, conditions authz.ResourceConditions, settings BindingConfig, authorization authz.ResourceConfig) (*ManagementReader, error) {
	if repository == nil || conditions == nil || settings.Validate() != nil || authorization.Validate() != nil {
		return nil, ErrBindingManagementUnavailable
	}
	return &ManagementReader{repository: repository, conditions: conditions, settings: settings, authorization: authorization}, nil
}

// Read returns one authorized detail or numbered collection page.
func (s *ManagementReader) Read(ctx context.Context, organization identity.OrganizationID, actor identity.UserID, query ManagementQuery) (ManagementRead, error) {
	bounded, cancel := context.WithTimeout(ctx, s.settings.Timeout)
	defer cancel()
	if query.Page == 0 {
		query.Page = 1
	}
	switch query.PageSize {
	case 10, 20, 50, 100:
	default:
		query.PageSize = 20
	}
	if query.ExpectedRevision < 0 || (query.ID == "" && query.Page > 1 && query.ExpectedRevision <= 0) {
		return ManagementRead{}, ErrInvalidManagementPage
	}
	permission, targetType := identity.Permission(""), query.Kind
	if query.ID == "" {
		targetType = identity.ResourceOrganization
	}
	switch query.Kind {
	case identity.ResourceUser:
		if query.ID == "" {
			permission = identity.PermissionUsersList
		} else {
			permission = identity.PermissionUsersGet
		}
	case identity.ResourceRole:
		if query.ID == "" {
			permission = identity.PermissionRolesList
		} else {
			permission = identity.PermissionRolesGet
		}
	case identity.ResourceBinding:
		if query.ID == "" {
			permission = identity.PermissionBindingsList
		} else {
			permission = identity.PermissionBindingsGet
		}
	default:
		return ManagementRead{}, ErrInvalidManagementPage
	}
	if query.Catalog && (query.Kind != identity.ResourceRole || query.ID != "") {
		return ManagementRead{}, ErrInvalidManagementPage
	}
	var result ManagementRead
	err := s.repository.WithManagementRead(bounded, organization, func(bounded context.Context, snapshot ManagementReadSnapshot) error {
		catalog, err := snapshot.LoadPermissionCatalog(bounded)
		if err != nil {
			return err
		}
		if identity.ValidatePermissionCatalog(catalog) != nil {
			return ErrBindingManagementInactive
		}
		for _, required := range identity.BindingManagementPermissions() {
			if !slices.Contains(catalog, required) {
				return ErrBindingManagementInactive
			}
		}
		authorizer, err := authz.NewResourceService(snapshot, s.conditions, catalog, s.authorization)
		if err != nil {
			return err
		}
		targetID := query.ID
		if targetID == "" {
			targetID = string(organization)
		}
		decision, err := authorizer.AuthorizeResource(bounded, actor, permission, authz.ResourceTarget{OrganizationID: organization, Type: targetType, ID: targetID})
		if err != nil {
			return err
		}
		if query.ExpectedRevision > 0 && query.ExpectedRevision != decision.Revision {
			return identity.ErrRoleConflict
		}
		result = ManagementRead{Page: query.Page, PageSize: query.PageSize, Revision: decision.Revision, EvaluatedAt: decision.EvaluatedAt}
		if query.Catalog {
			result.Permissions = slices.Clone(catalog)
			return nil
		}
		identifiers := []string{query.ID}
		if query.ID == "" {
			identifiers, result.TotalCount, err = snapshot.ListManagementIDs(bounded, query.Kind, int64(query.Page-1)*int64(query.PageSize), int32(query.PageSize))
			if err != nil {
				return err
			}
		}
		for _, identifier := range identifiers {
			switch query.Kind {
			case identity.ResourceUser:
				account, err := snapshot.GetPolicyAccount(bounded, identity.UserID(identifier))
				if err != nil {
					return err
				}
				result.Users = append(result.Users, account)
			case identity.ResourceRole:
				role, err := snapshot.GetPolicyRole(bounded, identity.RoleID(identifier))
				if err != nil {
					return err
				}
				result.Roles = append(result.Roles, role)
			case identity.ResourceBinding:
				binding, err := snapshot.GetPolicyBinding(bounded, identity.BindingID(identifier))
				if err != nil {
					return err
				}
				result.Bindings = append(result.Bindings, binding)
			}
		}
		return bindingContextError(bounded)
	})
	if failure := bindingContextError(bounded); failure != nil {
		return ManagementRead{}, failure
	}
	if err != nil {
		for _, safe := range []error{identity.ErrRoleConflict, identity.ErrUserNotFound, identity.ErrRoleNotFound, identity.ErrBindingNotFound, authz.ErrPermissionDenied, authz.ErrAuthorizationInactive, authz.ErrBindingLimitExceeded, ErrBindingManagementInactive, context.Canceled, context.DeadlineExceeded} {
			if errors.Is(err, safe) {
				return ManagementRead{}, safe
			}
		}
		return ManagementRead{}, ErrBindingManagementUnavailable
	}
	return result, nil
}
