package identity

import (
	"net/mail"
	"strings"
)

// MaxEmailLength is the RFC 5321 maximum for a forward path (address), in
// bytes. Every consumer of an email — syntactic validation, storage, and
// rate-limit keying — shares this one bound via EmailTooLong.
const MaxEmailLength = 254

// EmailTooLong reports whether email exceeds MaxEmailLength (RFC 5321 forward-path
// bound, byte length). It lives beside the constant so the one cap is enforced in a
// single predicate by every consumer — syntactic validation, the login
// oversized-input gate, and the rate limiter's bucket-key derivation.
func EmailTooLong(email string) bool {
	return len(email) > MaxEmailLength
}

// ValidateEmail checks an already-normalized (trimmed, folded — see NormalizeEmail)
// address, so the email value object owns its full validity rule rather than
// leaving structural checks to a caller. It uses the stdlib RFC 5322 parser for
// structure — which rejects "a@@x", ".a@x", embedded spaces, and empty
// local/domain — then tightens the domain, which the parser leaves permissive: it
// must be a bare addr-spec (no display name), carry a dot, and have labels with no
// leading/trailing hyphen (rejects "a@b", "a@-x.com"). Returns ErrInvalidEmail.
func ValidateEmail(email string) error {
	if email == "" || EmailTooLong(email) {
		return ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return ErrInvalidEmail
	}
	domain := email[strings.LastIndex(email, "@")+1:]
	if !strings.Contains(domain, ".") {
		return ErrInvalidEmail
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return ErrInvalidEmail
		}
	}
	return nil
}

// NormalizeEmail canonicalizes an email for storage, lookup, and rate-limit
// keying so the same address in different letter-case (or with surrounding
// whitespace) resolves to one identity and one throttling bucket. Domain names
// are case-insensitive; the local part technically is not, but treating it
// case-insensitively matches the login query's lower() and universal user
// expectation. This does not validate the address — see the auth service.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
