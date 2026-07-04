package connectapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// AuthService implements the Auth RPC over the application auth.Service. Session
// and CSRF tokens are delivered as __Host- cookies, never in the response body.
type AuthService struct {
	svc *auth.Service
}

// NewAuthService builds the Auth RPC handler.
func NewAuthService(svc *auth.Service) *AuthService { return &AuthService{svc: svc} }

func (a *AuthService) Bootstrap(
	ctx context.Context,
	req *connect.Request[portcullisv1.BootstrapRequest],
) (*connect.Response[portcullisv1.BootstrapResponse], error) {
	u, err := a.svc.Bootstrap(ctx, req.Msg.GetEmail(), req.Msg.GetPassword(), req.Msg.GetDisplayName())
	if err != nil {
		return nil, authError(err)
	}
	return connect.NewResponse(&portcullisv1.BootstrapResponse{User: toProtoUser(u)}), nil
}

func (a *AuthService) Login(
	ctx context.Context,
	req *connect.Request[portcullisv1.LoginRequest],
) (*connect.Response[portcullisv1.LoginResponse], error) {
	sess, err := a.svc.Login(ctx, req.Msg.GetEmail(), req.Msg.GetPassword())
	if err != nil {
		return nil, authError(err)
	}
	resp := connect.NewResponse(&portcullisv1.LoginResponse{User: toProtoUser(sess.User)})
	setCookie(resp.Header(), sessionCookie, sess.Token, true, sess.Session.AbsoluteExpiresAt)
	setCookie(resp.Header(), csrfCookie, sess.CSRF, false, sess.Session.AbsoluteExpiresAt)
	return resp, nil
}

func (a *AuthService) Logout(
	ctx context.Context,
	_ *connect.Request[portcullisv1.LogoutRequest],
) (*connect.Response[portcullisv1.LogoutResponse], error) {
	if err := a.svc.Logout(ctx, sessionTokenFromContext(ctx)); err != nil {
		return nil, authError(err)
	}
	resp := connect.NewResponse(&portcullisv1.LogoutResponse{})
	clearCookie(resp.Header(), sessionCookie, true)
	clearCookie(resp.Header(), csrfCookie, false)
	return resp, nil
}

func (a *AuthService) Me(
	ctx context.Context,
	_ *connect.Request[portcullisv1.MeRequest],
) (*connect.Response[portcullisv1.MeResponse], error) {
	u, ok := userFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	return connect.NewResponse(&portcullisv1.MeResponse{User: toProtoUser(u)}), nil
}

func toProtoUser(u identity.User) *portcullisv1.User {
	return &portcullisv1.User{
		Id:          string(u.ID),
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Status:      string(u.Status),
	}
}

// setCookie writes a __Host- cookie: Secure + Path=/ + no Domain (host-locked),
// SameSite=Lax (ADR-0006). httpOnly is true for the session token, false for the
// CSRF token so the SPA can echo it.
func setCookie(h http.Header, name, value string, httpOnly bool, expires time.Time) {
	h.Add("Set-Cookie", (&http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		Secure:   true,
		HttpOnly: httpOnly,
		SameSite: http.SameSiteLaxMode,
	}).String())
}

func clearCookie(h http.Header, name string, httpOnly bool) {
	h.Add("Set-Cookie", (&http.Cookie{
		Name:     name,
		Path:     "/",
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: httpOnly,
		SameSite: http.SameSiteLaxMode,
	}).String())
}

// authError maps domain errors to Connect codes with generic, leak-free messages.
func authError(err error) error {
	switch {
	case errors.Is(err, identity.ErrInvalidCredentials),
		errors.Is(err, identity.ErrUserDisabled),
		errors.Is(err, identity.ErrSessionNotFound):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
	case errors.Is(err, identity.ErrAlreadyBootstrapped):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("already bootstrapped"))
	case errors.Is(err, identity.ErrInvalidEmail):
		return connect.NewError(connect.CodeInvalidArgument, errors.New("invalid email"))
	case errors.Is(err, identity.ErrWeakPassword):
		return connect.NewError(connect.CodeInvalidArgument, errors.New("password does not meet policy"))
	case errors.Is(err, identity.ErrInvalidDisplayName):
		return connect.NewError(connect.CodeInvalidArgument, errors.New("invalid display name"))
	default:
		return connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
}
