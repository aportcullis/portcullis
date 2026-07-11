package auth

import "errors"

// Use-case sentinels for the Google login flow (ADR-0007). These are protocol
// failures of the redirect dance, not identity-domain concepts, so they live in
// the app layer; account-level rejections reuse the identity sentinels
// (ErrNoLinkedAccount, ErrIdentityLinkedToAnotherUser, ErrUserDisabled).
var (
	// ErrOIDCNotConfigured means an OIDC use case ran without a provider (Google
	// login is disabled unless the composition root injects one).
	ErrOIDCNotConfigured = errors.New("auth: oidc provider not configured")
	// ErrOIDCPendingInvalid means the pending state was missing, expired, or did
	// not match the returned state — one sentinel for all three, so a probe can't
	// distinguish which check failed.
	ErrOIDCPendingInvalid = errors.New("auth: oidc pending state missing, expired, or mismatched")
	// ErrOIDCNonceMismatch means the ID token's nonce did not echo the pending
	// nonce (replay defense — the one check the provider library leaves to us).
	ErrOIDCNonceMismatch = errors.New("auth: oidc nonce mismatch")
	// ErrOIDCEmailUnverified means the provider did not assert email_verified, so
	// the identity may not be linked to a local account by email (ADR-0007).
	ErrOIDCEmailUnverified = errors.New("auth: oidc email not verified by provider")
)
