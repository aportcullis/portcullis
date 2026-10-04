package connectapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"

	"connectrpc.com/connect"

	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

func NewRecoverOption(logger *slog.Logger) connect.HandlerOption {
	return connect.WithRecover(func(ctx context.Context, spec connect.Spec, _ http.Header, r any) error {
		logger.ErrorContext(ctx, "recovered from panic in RPC handler",
			"procedure", spec.Procedure, "panic_type", fmt.Sprintf("%T", r))
		return connect.NewError(connect.CodeInternal, errors.New("internal error"))
	})
}

func userFromContext(ctx context.Context) (identity.User, bool) {
	u, ok := ctx.Value(ctxUser).(identity.User)
	return u, ok
}

func sessionTokenFromContext(ctx context.Context) string {
	t, _ := ctx.Value(ctxSessionToken).(string)
	return t
}

// Parse session and CSRF cookies once, skipping invalid sibling pairs so another same-host application cannot hide a valid session.
func sessionAndCSRFCookies(h http.Header) (session, csrf string) {
	for _, c := range (&http.Request{Header: h}).Cookies() {
		switch c.Name {
		case sessionCookie:
			session = c.Value
		case csrfCookie:
			csrf = c.Value
		}
	}
	return session, csrf
}

// publicProcedures need no session — they run before one exists.
var publicProcedures = map[string]bool{
	portcullisv1connect.AuthBootstrapProcedure: true,
	portcullisv1connect.AuthLoginProcedure:     true,
	portcullisv1connect.AuthGetConfigProcedure: true,
}

// Credential procedures use tight limits before Argon2 hashing; the cheap public GetConfig read shares the broader non-credential bucket.
var credentialProcedures = map[string]bool{
	portcullisv1connect.AuthBootstrapProcedure: true,
	portcullisv1connect.AuthLoginProcedure:     true,
}

// BrowserOriginRequiredProcedures returns, sorted, the pre-session credential procedures whose unsafe requests must carry browser origin evidence at the HTTP boundary (ADR-0052).
func BrowserOriginRequiredProcedures() []string {
	return slices.Sorted(maps.Keys(credentialProcedures))
}

// isAuthFailure reports whether an Authenticate error means the session itself is invalid (missing/expired/revoked session, gone or disabled user) — as opposed to an infrastructure failure looking the session up.
func isAuthFailure(err error) bool {
	return errors.Is(err, identity.ErrSessionNotFound) ||
		errors.Is(err, identity.ErrUserNotFound) ||
		errors.Is(err, identity.ErrUserDisabled)
}

// sessionAuthenticator is the slice of the auth service the interceptor consumes (DIP/ISP).
type sessionAuthenticator interface {
	Authenticate(ctx context.Context, token string) (identity.User, identity.Session, error)
	VerifyCSRF(sessionToken, csrfToken string) error
	SlideIdle(ctx context.Context, sess identity.Session) error
}

// NewAuthInterceptor runs the shared session pipeline on every non-public unary RPC and injects the user and raw session token into the context (ADR-0006).
func NewAuthInterceptor(svc sessionAuthenticator) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if publicProcedures[req.Spec().Procedure] {
				return next(ctx, req)
			}
			session, err := authenticateSessionRequest(ctx, svc, req.Header())
			if err != nil {
				return nil, err
			}
			return next(withAuthenticatedSession(ctx, session), req)
		}
	}
}
