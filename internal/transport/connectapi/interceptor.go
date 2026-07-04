package connectapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// NewRecoverOption returns a handler option that converts a panic in any handler
// or interceptor into a clean CodeInternal error and logs it through the structured
// logger — so it goes through the redaction-controlled pipeline instead of
// net/http's default stack-trace dump to stderr, and the client gets a proper RPC
// error instead of a reset connection. Wire it FIRST so it wraps the whole chain.
// The recovered value is logged by TYPE only: its contents may carry sensitive data.
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

// sessionAndCSRFCookies reads both the session and CSRF cookie values from a
// SINGLE parse of the Cookie header. Browsers send every cookie for the host in one
// header, so reading each separately would tokenize the whole (potentially large)
// header twice per authenticated request. Parsing is lenient per PAIR, not per line:
// a strict parser (http.ParseCookie errors out the whole line) would let one
// malformed sibling cookie — set by any other app on the same host — hide the
// session cookie and lock the user out. The stdlib request path (Cookies/readCookies)
// skips only the invalid pairs.
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
}

// isAuthFailure reports whether an Authenticate error means the session itself
// is invalid (missing/expired/revoked session, gone or disabled user) — as
// opposed to an infrastructure failure looking the session up.
func isAuthFailure(err error) bool {
	return errors.Is(err, identity.ErrSessionNotFound) ||
		errors.Is(err, identity.ErrUserNotFound) ||
		errors.Is(err, identity.ErrUserDisabled)
}

// NewAuthInterceptor authenticates the session cookie and enforces HMAC
// double-submit CSRF on every non-public unary RPC, injecting the user and raw
// session token into the context (ADR-0006). Errors are generic so they reveal
// nothing about why authentication failed.
func NewAuthInterceptor(svc *auth.Service) connect.UnaryInterceptorFunc {
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
				// An infra failure during the lookup is not an invalid session — a DB
				// blip must not read as a logout (same rule as the idle slide below).
				if !isAuthFailure(err) {
					return nil, unavailable
				}
				return nil, unauthenticated
			}
			// CSRF: the readable cookie must equal the X-CSRF-Token header and verify
			// against the session token (a header==cookie match alone is bypassable).
			header := req.Header().Get(csrfHeader)
			if csrfTok == "" || header == "" || csrfTok != header || !svc.VerifyCSRF(sessionTok, header) {
				return nil, connect.NewError(connect.CodePermissionDenied, errors.New("invalid CSRF token"))
			}
			// Slide the idle window only now that the request is authorized, so a
			// CSRF-rejected request can't keep the session alive. By here the request
			// is authenticated and CSRF-valid, so a failed slide write is an infra
			// hiccup, not an invalid session — surface it as retryable Unavailable,
			// never Unauthenticated (which clients treat as a logout).
			if err := svc.SlideIdle(ctx, sess); err != nil {
				return nil, unavailable
			}
			ctx = context.WithValue(ctx, ctxUser, user)
			ctx = context.WithValue(ctx, ctxSessionToken, sessionTok)
			return next(ctx, req)
		}
	}
}
