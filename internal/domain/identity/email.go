package identity

import "strings"

// EmailTooLong reports whether email exceeds MaxEmailLength (RFC 5321 forward-path
// bound, byte length). It lives beside the constant so the one cap is enforced in a
// single predicate by every consumer — syntactic validation, the login
// oversized-input gate, and the rate limiter's bucket-key derivation.
func EmailTooLong(email string) bool {
	return len(email) > MaxEmailLength
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
