package identity

import (
	"net/mail"
	"strings"
)

// MaxEmailLength is the RFC 5321 maximum for a forward path (address), in bytes. Every consumer of an email — syntactic validation, storage, and rate-limit keying — shares this one bound via EmailTooLong.
const MaxEmailLength = 254

// EmailTooLong reports whether email exceeds MaxEmailLength (RFC 5321 forward-path bound, byte length). It lives beside the constant so the one cap is enforced in a single predicate by every consumer — syntactic validation, the login oversized-input gate, and the rate limiter's bucket-key derivation.
func EmailTooLong(email string) bool {
	return len(email) > MaxEmailLength
}

// ValidateEmail requires a normalized bare RFC 5322 address with a dotted domain and no leading or trailing label hyphens.
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

// NormalizeEmail trims and case-folds addresses consistently with login lookup and rate-limit keys; it does not validate them.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
