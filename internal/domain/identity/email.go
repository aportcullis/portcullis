package identity

import "strings"

// NormalizeEmail canonicalizes an email for storage, lookup, and rate-limit
// keying so the same address in different letter-case (or with surrounding
// whitespace) resolves to one identity and one throttling bucket. Domain names
// are case-insensitive; the local part technically is not, but treating it
// case-insensitively matches the login query's lower() and universal user
// expectation. This does not validate the address — see the auth service.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
