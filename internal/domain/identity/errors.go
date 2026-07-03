package identity

import "errors"

var (
	// ErrUserNotFound means no user matched the lookup.
	ErrUserNotFound = errors.New("identity: user not found")
	// ErrSessionNotFound means no session matched the token hash.
	ErrSessionNotFound = errors.New("identity: session not found")
	// ErrInvalidCredentials means the email/password pair did not verify.
	ErrInvalidCredentials = errors.New("identity: invalid credentials")
	// ErrInvalidEmail means an email failed the syntactic checks (empty, missing
	// "@", or over the RFC 5321 length limit).
	ErrInvalidEmail = errors.New("identity: invalid email")
	// ErrWeakPassword means a password was shorter than the minimum or longer than
	// the maximum allowed length.
	ErrWeakPassword = errors.New("identity: password does not meet policy")
	// ErrUserDisabled means the account exists but may not authenticate.
	ErrUserDisabled = errors.New("identity: user is disabled")
	// ErrAlreadyBootstrapped means bootstrap ran when users already exist.
	ErrAlreadyBootstrapped = errors.New("identity: already bootstrapped")
	// ErrNoLinkedAccount means an external identity has no matching local user
	// (no auto-provisioning — see ADR-0007).
	ErrNoLinkedAccount = errors.New("identity: no local account for this identity")
	// ErrIdentityLinkedToAnotherUser means the (issuer, subject) is already linked
	// to a different local user, so it must not be re-pointed.
	ErrIdentityLinkedToAnotherUser = errors.New("identity: external identity already linked to another user")
)
