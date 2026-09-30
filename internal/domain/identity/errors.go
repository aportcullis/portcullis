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

	ErrNoLinkedAccount = errors.New("identity: no local account for this identity")

	ErrIdentityLinkedToAnotherUser = errors.New("identity: external identity already linked to another user")

	ErrEmailTaken = errors.New("identity: email already registered")

	ErrMembershipExists = errors.New("identity: membership already exists")
)
