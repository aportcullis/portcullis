package connectapi

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// authenticatedSession is the user and raw session token a request proved it holds.
type authenticatedSession struct {
	user  identity.User
	token string
}

// authenticateSessionRequest resolves the session cookie, enforces HMAC double-submit CSRF, then slides the idle window, for unary and streaming RPCs alike (ADR-0006). Errors are generic so they reveal nothing about why authentication failed.
func authenticateSessionRequest(ctx context.Context, sessions sessionAuthenticator, header http.Header) (authenticatedSession, error) {
	// One parse of the Cookie header yields both values (see the helper).
	sessionToken, csrfCookieValue := sessionAndCSRFCookies(header)
	if sessionToken == "" {
		return authenticatedSession{}, newAuthenticationRequiredError()
	}
	user, session, err := sessions.Authenticate(ctx, sessionToken)
	if err != nil {
		return authenticatedSession{}, sessionAuthenticationError(err)
	}
	// The readable cookie must equal the X-CSRF-Token header and verify against the session token (a header==cookie match alone is bypassable).
	csrfHeaderValue := header.Get(csrfHeader)
	if csrfCookieValue == "" || csrfHeaderValue == "" || csrfCookieValue != csrfHeaderValue {
		return authenticatedSession{}, newInvalidCSRFError()
	}
	if err := sessions.VerifyCSRF(sessionToken, csrfHeaderValue); err != nil {
		// A token whose key version is not loaded cannot be re-verified, so the session must sign in again rather than read as forgery.
		if errors.Is(err, identity.ErrCSRFKeyVersionUnknown) {
			return authenticatedSession{}, newAuthenticationRequiredError()
		}
		return authenticatedSession{}, newInvalidCSRFError()
	}
	// Slide the idle window only now that the request is authorized, so a CSRF-rejected request can't keep the session alive. Zero rows means the session died after Authenticate and must reject this request.
	if err := sessions.SlideIdle(ctx, session); err != nil {
		return authenticatedSession{}, sessionAuthenticationError(err)
	}
	return authenticatedSession{user: user, token: sessionToken}, nil
}

// withAuthenticatedSession injects the authenticated user and raw session token for handlers.
func withAuthenticatedSession(ctx context.Context, session authenticatedSession) context.Context {
	ctx = context.WithValue(ctx, ctxUser, session.user)
	return context.WithValue(ctx, ctxSessionToken, session.token)
}

// sessionAuthenticationError maps an invalid session to Unauthenticated and a lookup or slide infrastructure failure to retryable Unavailable, so a database blip never reads as a logout.
func sessionAuthenticationError(err error) error {
	if isAuthFailure(err) {
		return newAuthenticationRequiredError()
	}
	return newServerFaultError(connect.CodeUnavailable, "temporarily unavailable", err)
}

// newAuthenticationRequiredError builds the generic Unauthenticated refusal.
func newAuthenticationRequiredError() error {
	return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
}

// newInvalidCSRFError builds the generic CSRF refusal.
func newInvalidCSRFError() error {
	return connect.NewError(connect.CodePermissionDenied, errors.New("invalid CSRF token"))
}
