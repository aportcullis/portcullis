package connectapi

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

func userFromContext(ctx context.Context) (identity.User, bool) {
	u, ok := ctx.Value(ctxUser).(identity.User)
	return u, ok
}

func sessionTokenFromContext(ctx context.Context) string {
	t, _ := ctx.Value(ctxSessionToken).(string)
	return t
}

// cookieValue returns the named cookie's value from the request headers, or "".
// Uses the stdlib http.ParseCookie (Go 1.23+) rather than a synthetic Request.
func cookieValue(h http.Header, name string) string {
	for _, line := range h.Values("Cookie") {
		cookies, err := http.ParseCookie(line)
		if err != nil {
			continue
		}
		for _, c := range cookies {
			if c.Name == name {
				return c.Value
			}
		}
	}
	return ""
}

// publicProcedures need no session — they run before one exists.
var publicProcedures = map[string]bool{
	portcullisv1connect.AuthBootstrapProcedure: true,
	portcullisv1connect.AuthLoginProcedure:     true,
}

// NewAuthInterceptor authenticates the session cookie and enforces HMAC
// double-submit CSRF on every non-public unary RPC, injecting the user and raw
// session token into the context (ADR-0006). Errors are generic so they reveal
// nothing about why authentication failed.
func NewAuthInterceptor(svc *auth.Service) connect.UnaryInterceptorFunc {
	unauthenticated := connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if publicProcedures[req.Spec().Procedure] {
				return next(ctx, req)
			}
			sessionTok := cookieValue(req.Header(), sessionCookie)
			if sessionTok == "" {
				return nil, unauthenticated
			}
			user, _, err := svc.Authenticate(ctx, sessionTok)
			if err != nil {
				return nil, unauthenticated
			}
			// CSRF: the readable cookie must equal the X-CSRF-Token header and verify
			// against the session token (a header==cookie match alone is bypassable).
			csrfTok := cookieValue(req.Header(), csrfCookie)
			header := req.Header().Get(csrfHeader)
			if csrfTok == "" || header == "" || csrfTok != header || !svc.VerifyCSRF(sessionTok, header) {
				return nil, connect.NewError(connect.CodePermissionDenied, errors.New("invalid CSRF token"))
			}
			ctx = context.WithValue(ctx, ctxUser, user)
			ctx = context.WithValue(ctx, ctxSessionToken, sessionTok)
			return next(ctx, req)
		}
	}
}
