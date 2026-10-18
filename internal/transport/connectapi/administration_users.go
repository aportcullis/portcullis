package connectapi

import (
	"connectrpc.com/connect"
	"context"
	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// accountPolicyApp is the account mutation surface consumed by Users.
type accountPolicyApp interface {
	Create(context.Context, identity.OrganizationID, identity.UserID, identity.NewPolicyAccount) (administration.CreatedPolicyAccount, error)
	Disable(context.Context, identity.OrganizationID, identity.UserID, identity.UserID) (identity.PolicyAccount, error)
	Enable(context.Context, identity.OrganizationID, identity.UserID, identity.UserID) (identity.PolicyAccount, error)
	IssueSetupLink(context.Context, identity.OrganizationID, identity.UserID, identity.UserID) (administration.SetupLink, error)
}

// UsersService exposes independent membership management.
type UsersService struct {
	access  managementAccess
	service accountPolicyApp
}

// NewUsersService injects the account use cases and authorized query boundary.
func NewUsersService(org identity.OrganizationID, service accountPolicyApp, reader managementReader) *UsersService {
	return &UsersService{access: managementAccess{organization: org, reader: reader}, service: service}
}

// List returns only organization-authorized account summaries.
func (s *UsersService) List(ctx context.Context, request *connect.Request[portcullisv1.ListUsersRequest]) (*connect.Response[portcullisv1.ListUsersResponse], error) {
	read, err := s.access.readManagement(ctx, managementPage(identity.ResourceUser, request.Msg.GetPagination()))
	if err != nil {
		return nil, err
	}
	users := make([]*portcullisv1.User, 0, len(read.Users))
	for _, account := range read.Users {
		users = append(users, toProtoPolicyAccount(account).User)
	}
	return connect.NewResponse(&portcullisv1.ListUsersResponse{Users: users, Pagination: managementPageInfo(read)}), nil
}

// Get returns a member after its specific resource authorization.
func (s *UsersService) Get(ctx context.Context, request *connect.Request[portcullisv1.GetUserRequest]) (*connect.Response[portcullisv1.GetUserResponse], error) {
	identifier, err := managementID(ctx, request.Msg.GetId())
	if err != nil {
		return nil, err
	}
	read, err := s.access.readManagement(ctx, administration.ManagementQuery{Kind: identity.ResourceUser, ID: identifier})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&portcullisv1.GetUserResponse{User: toProtoPolicyAccount(read.Users[0]), AuthorizationRevision: read.Revision}), nil
}

// Create establishes grantless membership and returns its setup token once.
func (s *UsersService) Create(ctx context.Context, request *connect.Request[portcullisv1.CreateUserRequest]) (*connect.Response[portcullisv1.CreateUserResponse], error) {
	actor, err := managementActor(ctx)
	if err != nil {
		return nil, err
	}
	created, err := s.service.Create(ctx, s.access.organization, actor, identity.NewPolicyAccount{Email: request.Msg.GetEmail(), DisplayName: request.Msg.GetDisplayName()})
	if err != nil {
		return nil, managementError(err, false)
	}
	return connect.NewResponse(&portcullisv1.CreateUserResponse{User: toProtoAccountStatus(created.Account), SetupLink: toProtoSetupLink(created.SetupLink)}), nil
}

// Disable blocks another account while preserving its binding history.
func (s *UsersService) Disable(ctx context.Context, request *connect.Request[portcullisv1.DisableUserRequest]) (*connect.Response[portcullisv1.DisableUserResponse], error) {
	account, err := s.changeAccountStatus(ctx, request.Msg.GetId(), false)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&portcullisv1.DisableUserResponse{User: toProtoAccountStatus(account)}), nil
}

// Enable restores another account through complete delegation and recovery checks.
func (s *UsersService) Enable(ctx context.Context, request *connect.Request[portcullisv1.EnableUserRequest]) (*connect.Response[portcullisv1.EnableUserResponse], error) {
	account, err := s.changeAccountStatus(ctx, request.Msg.GetId(), true)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&portcullisv1.EnableUserResponse{User: toProtoAccountStatus(account)}), nil
}

// changeAccountStatus passes server-selected operations to the transactional account boundary.
func (s *UsersService) changeAccountStatus(ctx context.Context, rawID string, enable bool) (identity.PolicyAccount, error) {
	identifier, err := managementID(ctx, rawID)
	if err != nil {
		return identity.PolicyAccount{}, err
	}
	actor, err := managementActor(ctx)
	if err != nil {
		return identity.PolicyAccount{}, err
	}
	var account identity.PolicyAccount
	if enable {
		account, err = s.service.Enable(ctx, s.access.organization, actor, identity.UserID(identifier))
	} else {
		account, err = s.service.Disable(ctx, s.access.organization, actor, identity.UserID(identifier))
	}
	if err != nil {
		return identity.PolicyAccount{}, managementError(err, false)
	}
	return account, nil
}

// IssueSetupLink returns a replacement one-time token for an eligible passwordless account.
func (s *UsersService) IssueSetupLink(ctx context.Context, request *connect.Request[portcullisv1.IssueUserSetupLinkRequest]) (*connect.Response[portcullisv1.IssueUserSetupLinkResponse], error) {
	identifier, err := managementID(ctx, request.Msg.GetId())
	if err != nil {
		return nil, err
	}
	actor, err := managementActor(ctx)
	if err != nil {
		return nil, err
	}
	link, err := s.service.IssueSetupLink(ctx, s.access.organization, actor, identity.UserID(identifier))
	if err != nil {
		return nil, managementError(err, false)
	}
	return connect.NewResponse(&portcullisv1.IssueUserSetupLinkResponse{SetupLink: toProtoSetupLink(link)}), nil
}

// toProtoPolicyAccount omits all legacy role and credential material.
func toProtoPolicyAccount(account identity.PolicyAccount) *portcullisv1.ManagedUser {
	return &portcullisv1.ManagedUser{User: &portcullisv1.User{Id: string(account.User.ID), Email: account.User.Email, DisplayName: account.User.DisplayName, Status: string(account.User.Status)}, HasPassword: account.HasPassword, CreatedAt: timestamppb.New(account.User.CreatedAt)}
}

// toProtoSetupLink exposes only the freshly issued token and its expiry.
func toProtoSetupLink(link administration.SetupLink) *portcullisv1.UserSetupLink {
	return &portcullisv1.UserSetupLink{Token: link.Token, ExpiresAt: timestamppb.New(link.ExpiresAt)}
}

// toProtoAccountStatus excludes existing profile and credential metadata from write acknowledgments.
func toProtoAccountStatus(account identity.PolicyAccount) *portcullisv1.AccountStatus {
	return &portcullisv1.AccountStatus{Id: string(account.User.ID), Status: string(account.User.Status)}
}
