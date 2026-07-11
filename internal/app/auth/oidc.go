package auth

import (
	"context"
	"crypto/subtle"
	"errors"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// WithOIDCProvider enables Google login by injecting the provider adapter
// (ADR-0007). It is a chaining setter, not a New parameter, because the
// provider is optional — the deployment may not configure Google at all.
func (s *Service) WithOIDCProvider(p OIDCProvider) *Service {
	s.oidc = p
	return s
}

// StartGoogleLogin begins the redirect flow: it mints the per-flow secrets
// (CSRF state, replay nonce, PKCE verifier — three independent values, so one
// leaked secret can't stand in for another) and returns the provider's
// authorization URL plus the pending state the transport seals into the
// short-lived cookie (ADR-0007).
func (s *Service) StartGoogleLogin(_ context.Context) (string, OIDCPending, error) {
	if s.oidc == nil {
		return "", OIDCPending{}, ErrOIDCNotConfigured
	}
	state, err := newToken()
	if err != nil {
		return "", OIDCPending{}, err
	}
	nonce, err := newToken()
	if err != nil {
		return "", OIDCPending{}, err
	}
	// newToken's 32 random bytes base64url-encode to 43 characters, meeting the
	// RFC 7636 code-verifier length floor; the adapter derives the S256 challenge.
	verifier, err := newToken()
	if err != nil {
		return "", OIDCPending{}, err
	}
	pending := OIDCPending{State: state, Nonce: nonce, Verifier: verifier, ExpiresAt: s.now().Add(oidcPendingTTL)}
	return s.oidc.AuthCodeURL(state, nonce, verifier), pending, nil
}

// LoginWithGoogle completes the callback: it validates the pending state,
// redeems the code, verifies the nonce, resolves the local account (linked
// subject first, then link-by-verified-email to an existing user — never
// auto-creating one, ADR-0007), and issues a session exactly like a password
// login (rotation + same-transaction audit, ADR-0006/0009).
func (s *Service) LoginWithGoogle(ctx context.Context, state, code string, pending OIDCPending) (_ Session, err error) {
	if s.oidc == nil {
		return Session{}, ErrOIDCNotConfigured
	}
	// EVERY failure exit records one best-effort event, tagged with the method so
	// Google sign-ins stay distinguishable under the single AUTH_LOGIN action.
	// The event gains the actor once a user resolves (mirrors Login).
	failed := newEvent(ctx, audit.ActionAuthLogin, audit.OutcomeFailed)
	failed.Metadata = map[string]any{"method": oidcMethodMetadata}
	defer func() {
		if err != nil {
			s.recordAudit(ctx, failed)
		}
	}()

	// The pending checks gate the network call: a forged, stale, or cookie-less
	// callback must never spend a code exchange against the provider. One
	// sentinel for all three, so a probe can't tell which check failed; the
	// state comparison is constant-time out of caution (the state is single-use,
	// but a timing oracle on it is free to avoid).
	if pending.State == "" || s.now().After(pending.ExpiresAt) ||
		subtle.ConstantTimeCompare([]byte(state), []byte(pending.State)) != 1 {
		return Session{}, ErrOIDCPendingInvalid
	}

	// The adapter verifies signature/iss/aud/exp; the nonce is ours to check.
	claims, err := s.oidc.Exchange(ctx, code, pending.Verifier)
	if err != nil {
		return Session{}, err
	}
	if subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(pending.Nonce)) != 1 {
		return Session{}, ErrOIDCNonceMismatch
	}

	u, err := s.resolveOIDCUser(ctx, claims, &failed)
	if err != nil {
		return Session{}, err
	}
	return s.issueSession(ctx, u, map[string]any{"method": oidcMethodMetadata})
}

// resolveOIDCUser maps verified claims to a local user: the (issuer, subject)
// link is the stable key; a first login may attach to an existing account by
// provider-verified email, and nothing is ever auto-created (ADR-0007). The
// failure event is attributed as soon as a user resolves.
func (s *Service) resolveOIDCUser(ctx context.Context, claims identity.OIDCClaims, failed *audit.Event) (identity.User, error) {
	u, err := s.repo.FindUserBySubject(ctx, claims.Issuer, claims.Subject)
	switch {
	case err == nil:
		*failed = withActor(*failed, u.ID)
		if !u.Active() {
			return identity.User{}, identity.ErrUserDisabled
		}
		return u, nil
	case !errors.Is(err, identity.ErrNoLinkedAccount):
		return identity.User{}, err
	}

	// First login for this subject: only a provider-verified email may attach it
	// to an existing account.
	if !claims.EmailVerified {
		return identity.User{}, ErrOIDCEmailUnverified
	}
	u, err = s.repo.GetUserByEmail(ctx, identity.NormalizeEmail(claims.Email))
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return identity.User{}, identity.ErrNoLinkedAccount
		}
		return identity.User{}, err
	}
	*failed = withActor(*failed, u.ID)
	// Disabled is checked BEFORE linking: re-enabling the user later must not
	// silently activate a link that was never approved while active.
	if !u.Active() {
		return identity.User{}, identity.ErrUserDisabled
	}
	// The store refuses to re-point an identity claimed by another user in the
	// window since our lookup (unique (issuer, subject)); the race is a
	// rejection, never a retry.
	if err := s.repo.LinkIdentity(ctx, identity.OIDCIdentity{
		UserID:  u.ID,
		Issuer:  claims.Issuer,
		Subject: claims.Subject,
		Email:   identity.NormalizeEmail(claims.Email),
	}); err != nil {
		return identity.User{}, err
	}
	return u, nil
}
