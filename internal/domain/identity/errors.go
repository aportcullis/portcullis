package identity

import "errors"

var (
	// ErrUserNotFound means no user matched the lookup.
	ErrUserNotFound = errors.New("identity: user not found")
	// ErrSessionNotFound means no session matched the token hash.
	ErrSessionNotFound = errors.New("identity: session not found")
	// ErrInvalidCredentials means the email/password pair did not verify.
	ErrInvalidCredentials = errors.New("identity: invalid credentials")
	// ErrUserDisabled means the account exists but may not authenticate.
	ErrUserDisabled = errors.New("identity: user is disabled")
	// ErrAlreadyBootstrapped means bootstrap ran when users already exist.
	ErrAlreadyBootstrapped = errors.New("identity: already bootstrapped")
	// ErrNoLinkedAccount means an external identity has no matching local user
	// (no auto-provisioning — see ADR-0007).
	ErrNoLinkedAccount = errors.New("identity: no local account for this identity")
)
