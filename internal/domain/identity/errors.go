package identity

import "errors"

var (
	ErrUserNotFound = errors.New("identity: user not found")

	ErrSessionNotFound = errors.New("identity: session not found")

	ErrInvalidCredentials = errors.New("identity: invalid credentials")

	ErrInvalidEmail = errors.New("identity: invalid email")

	ErrWeakPassword = errors.New("identity: password does not meet policy")

	ErrInvalidDisplayName = errors.New("identity: invalid display name")

	ErrUserDisabled = errors.New("identity: user is disabled")

	ErrAlreadyBootstrapped = errors.New("identity: already bootstrapped")

	// ErrSetupTokenInvalid covers a missing, wrong, expired, rotated or consumed first-run setup token alike, so a probe learns nothing about which (ADR-0052).
	ErrSetupTokenInvalid = errors.New("identity: setup token invalid")

	ErrNoLinkedAccount = errors.New("identity: no local account for this identity")

	ErrIdentityLinkedToAnotherUser = errors.New("identity: external identity already linked to another user")

	ErrEmailTaken = errors.New("identity: email already registered")

	ErrMembershipExists = errors.New("identity: membership already exists")

	// ErrCSRFTokenInvalid means a CSRF token is malformed, forged, or bound to another session.
	ErrCSRFTokenInvalid = errors.New("identity: invalid csrf token")

	// ErrCSRFKeyVersionUnknown means a well-formed CSRF token names a key version this process has not loaded (or predates key versioning), so its session must sign in again (ADR-0006).
	ErrCSRFKeyVersionUnknown = errors.New("identity: csrf token key version not loaded")
)
