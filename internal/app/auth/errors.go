package auth

import "errors"

// Use-case sentinels for the Google login flow (ADR-0007). These are protocol failures of the redirect dance, not identity-domain concepts, so they live in the app layer; account-level rejections reuse the identity sentinels (ErrNoLinkedAccount, ErrIdentityLinkedToAnotherUser, ErrUserDisabled).
var (
	ErrOIDCNotConfigured = errors.New("auth: oidc provider not configured")

	ErrOIDCPendingInvalid = errors.New("auth: oidc pending state missing, expired, or mismatched")

	ErrOIDCNonceMismatch = errors.New("auth: oidc nonce mismatch")

	ErrOIDCEmailUnverified = errors.New("auth: oidc email not verified by provider")
)
