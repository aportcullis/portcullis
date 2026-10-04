package connectapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

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

// NewAuthInterceptor authenticates the session cookie and enforces HMAC double-submit CSRF on every non-public unary RPC, injecting the user and raw session token into the context (ADR-0006). Errors are generic so they reveal nothing about why authentication failed.
func NewAuthInterceptor(svc sessionAuthenticator) connect.UnaryInterceptorFunc {
	unauthenticated := connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	unavailable := connect.NewError(connect.CodeUnavailable, errors.New("temporarily unavailable"))
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if publicProcedures[req.Spec().Procedure] {
				return next(ctx, req)
			}
			// One parse of the Cookie header yields both values (see the helper).
			sessionTok, csrfTok := sessionAndCSRFCookies(req.Header())
			if sessionTok == "" {
				return nil, unauthenticated
			}
			user, sess, err := svc.Authenticate(ctx, sessionTok)
			if err != nil {
				// An infra failure during the lookup is not an invalid session — a DB blip must not read as a logout (same rule as the idle slide below).
				if !isAuthFailure(err) {
					return nil, unavailable
				}
				return nil, unauthenticated
			}
			// CSRF: the readable cookie must equal the X-CSRF-Token header and verify against the session token (a header==cookie match alone is bypassable).
			header := req.Header().Get(csrfHeader)
			if csrfTok == "" || header == "" || csrfTok != header {
				return nil, connect.NewError(connect.CodePermissionDenied, errors.New("invalid CSRF token"))
			}
			if err := svc.VerifyCSRF(sessionTok, header); err != nil {
				// A token whose key version is not loaded cannot be re-verified, so the session must sign in again rather than read as forgery (ADR-0006).
				if errors.Is(err, identity.ErrCSRFKeyVersionUnknown) {
					return nil, unauthenticated
				}
				return nil, connect.NewError(connect.CodePermissionDenied, errors.New("invalid CSRF token"))
			}
			// Slide the idle window only now that the request is authorized, so a CSRF-rejected request can't keep the session alive. The conditional write is also the final server-side expiry/revocation check: zero rows means the session died after Authenticate and must reject this request. Other write failures remain retryable infrastructure faults.
			if err := svc.SlideIdle(ctx, sess); err != nil {
				if isAuthFailure(err) {
					return nil, unauthenticated
				}
				return nil, unavailable
			}
			ctx = context.WithValue(ctx, ctxUser, user)
			ctx = context.WithValue(ctx, ctxSessionToken, sessionTok)
			return next(ctx, req)
		}
	}
}
