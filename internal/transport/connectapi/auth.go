package connectapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// authApp is the slice of the auth application service this handler consumes
// (DIP/ISP — the handler depends on the methods it calls, not the concrete
// *auth.Service; tests substitute fakes without a database).
type authApp interface {
	Bootstrap(ctx context.Context, email, password, displayName string) (identity.User, error)
	Login(ctx context.Context, email, password string) (auth.Session, error)
	Logout(ctx context.Context, token string) error
	PublicConfig(ctx context.Context) (auth.PublicConfig, error)
}

// AuthService implements the Auth RPC over the auth application service.
// Session and CSRF tokens are delivered as __Host- cookies, never in the
// response body.
type AuthService struct {
	svc    authApp
	perms  permissionLister
	logger *slog.Logger
}

// permissionLister enumerates the caller's own permission keys for the SPA's
// affordance gating (Me/Login). Consumer-defined (ISP): the app authz service
// satisfies it.
type permissionLister interface {
	PermissionsFor(ctx context.Context, user identity.User) ([]identity.Permission, error)
}

// NewAuthService builds the Auth RPC handler. The permission lister is a
// REQUIRED collaborator: an optional builder was a silent footgun — forgotten
// wiring rendered an authenticated UI with every affordance hidden and no
// signal (self-review F6).
func NewAuthService(svc authApp, perms permissionLister) *AuthService {
	return &AuthService{svc: svc, perms: perms, logger: slog.Default()}
}

// WithLogger routes the service's own warnings (degraded permission listing).
func (a *AuthService) WithLogger(l *slog.Logger) *AuthService { a.logger = l; return a }

// listPermissions resolves the caller's permission keys as wire strings. The
// keys are ADVISORY UI data (the server still authorizes every RPC), so a
// resolution failure fails OPEN: the response proceeds with an empty list and
// a warning, because failing the RPC would gate login availability and
// authenticated-session usability on the authz store — Login would error
// after the session row was committed but before the cookies were delivered,
// and the SPA maps an Internal from Me to the unreachable retry card
// (self-review F1). The UI recovers on the next successful Me.
func (a *AuthService) listPermissions(ctx context.Context, u identity.User) []string {
	perms, err := a.perms.PermissionsFor(ctx, u)
	if err != nil {
		a.logger.Warn("permission enumeration failed — responding with none (UI affordances only; server-side authorization is unaffected)", "err", err)
		return nil
	}
	keys := make([]string, len(perms))
	for i, p := range perms {
		keys[i] = string(p)
	}
	return keys
}

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
	resp := connect.NewResponse(&portcullisv1.LoginResponse{User: toProtoUser(sess.User), Permissions: a.listPermissions(ctx, sess.User)})
	noStore(resp.Header())
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
	noStore(resp.Header())
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
	return connect.NewResponse(&portcullisv1.MeResponse{User: toProtoUser(u), Permissions: a.listPermissions(ctx, u)}), nil
}

func (a *AuthService) GetConfig(
	ctx context.Context,
	_ *connect.Request[portcullisv1.GetConfigRequest],
) (*connect.Response[portcullisv1.GetConfigResponse], error) {
	cfg, err := a.svc.PublicConfig(ctx)
	if err != nil {
		return nil, authError(err)
	}
	return connect.NewResponse(&portcullisv1.GetConfigResponse{
		GoogleEnabled:  cfg.GoogleEnabled,
		NeedsBootstrap: cfg.NeedsBootstrap,
	}), nil
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

// noStore prevents session-bearing responses from being retained by browser or
// intermediary caches. It is applied only to authentication routes so static
// SPA assets remain cacheable (OWASP Session Management guidance).
func noStore(h http.Header) {
	h.Set("Cache-Control", "no-store")
	h.Set("Pragma", "no-cache")
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
