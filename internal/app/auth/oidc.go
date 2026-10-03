package auth

import (
	"context"
	"crypto/subtle"
	"errors"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// WithOIDCProvider enables Google login by injecting the provider adapter (ADR-0007). It is a chaining setter, not a New parameter, because the provider is optional — the deployment may not configure Google at all.
func (s *Service) WithOIDCProvider(p OIDCProvider) *Service {
	s.oidc = p
	return s
}

// StartGoogleLogin begins the redirect flow: it mints the per-flow secrets (CSRF state, replay nonce, PKCE verifier — three independent values, so one leaked secret can't stand in for another) and returns the provider's authorization URL plus the pending state the transport seals into the short-lived cookie (ADR-0007).
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
	// newToken's 32 random bytes base64url-encode to 43 characters, meeting the RFC 7636 code-verifier length floor; the adapter derives the S256 challenge.
	verifier, err := newToken()
	if err != nil {
		return "", OIDCPending{}, err
	}
	pending := OIDCPending{State: state, Nonce: nonce, Verifier: verifier, ExpiresAt: s.now().Add(oidcPendingTTL)}
	return s.oidc.AuthCodeURL(state, nonce, verifier), pending, nil
}

// LoginWithGoogle verifies flow secrets and resolves a subject or authoritative email before issuing an audited session.
func (s *Service) LoginWithGoogle(ctx context.Context, state, code string, pending OIDCPending) (_ Session, err error) {
	if s.oidc == nil {
		return Session{}, ErrOIDCNotConfigured
	}
	// Audit every login failure with a detached context so disconnects cannot erase the trail. Unknown accounts retain neither actor nor attempted email.
	failed := newEvent(ctx, audit.ActionAuthLogin, audit.OutcomeFailed)
	failed.Metadata = map[string]any{"method": oidcMethodMetadata}
	defer func() {
		if err != nil {
			s.recordAudit(ctx, failed)
		}
	}()

	// The pending checks gate the network call: a forged, stale, or cookie-less callback must never spend a code exchange against the provider. One sentinel for all three, so a probe can't tell which check failed; the state comparison is constant-time out of caution (the state is single-use, but a timing oracle on it is free to avoid).
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

	u, link, err := s.resolveOIDCUser(ctx, claims, &failed)
	if err != nil {
		return Session{}, err
	}
	meta := map[string]any{"method": oidcMethodMetadata}
	if link != nil {
		meta["identity_linked"] = true
	}
	return s.issueSession(ctx, u, meta, link)
}

// resolveOIDCUser resolves stable subject links or currently authoritative emails without creating users.
func (s *Service) resolveOIDCUser(ctx context.Context, claims identity.OIDCClaims, failed *audit.Event) (identity.User, *identity.OIDCIdentity, error) {
	u, err := s.repo.FindUserBySubject(ctx, claims.Issuer, claims.Subject)
	switch {
	case err == nil:
		*failed = withActor(*failed, u.ID)
		if !u.Active() {
			return identity.User{}, nil, identity.ErrUserDisabled
		}
		return u, nil, nil
	case !errors.Is(err, identity.ErrNoLinkedAccount):
		return identity.User{}, nil, err
	}

	// Historical email verification alone cannot attach a new authenticator.
	if !claims.EmailVerified {
		return identity.User{}, nil, ErrOIDCEmailUnverified
	}
	if !claims.EmailAuthoritative {
		return identity.User{}, nil, identity.ErrNoLinkedAccount
	}
	u, err = s.repo.GetUserByEmail(ctx, identity.NormalizeEmail(claims.Email))
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return identity.User{}, nil, identity.ErrNoLinkedAccount
		}
		return identity.User{}, nil, err
	}
	*failed = withActor(*failed, u.ID)
	// Disabled is checked BEFORE linking: re-enabling the user later must not silently activate a link that was never approved while active.
	if !u.Active() {
		return identity.User{}, nil, identity.ErrUserDisabled
	}
	// The store claims this link inside the session/audit transaction. Its unique (issuer, subject) constraint still turns a first-login race into a rejection, never a re-point or retry.
	link := &identity.OIDCIdentity{
		UserID:  u.ID,
		Issuer:  claims.Issuer,
		Subject: claims.Subject,
		Email:   identity.NormalizeEmail(claims.Email),
	}
	return u, link, nil
}
