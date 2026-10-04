package identity

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// UserStatus is the lifecycle state of an account. These are fixed domain enums the code branches on (see Active), so they live in code; everything configurable — the permission catalog, roles, the OIDC provider — comes from the database or config, not constants.
type UserStatus string

// Account lifecycle states — mirrors the users.status check constraint.
const (
	StatusActive   UserStatus = "active"
	StatusDisabled UserStatus = "disabled"
)

// MaxDisplayNameLength bounds a stored account display name in Unicode code points so an unbounded value cannot bloat rows, logs or UI.
const MaxDisplayNameLength = 256

// User is an account.
type User struct {
	ID          UserID
	Email       string
	DisplayName string
	Status      UserStatus
	CreatedAt   time.Time
}

// Active reports whether the user may authenticate and act.
func (u User) Active() bool { return u.Status == StatusActive }

// ValidateDisplayName rejects empty or oversized names and Unicode Cc, Cf, Zl and Zp characters to prevent log injection and display spoofing.
func ValidateDisplayName(name string) error {
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > MaxDisplayNameLength || hasUnsafeDisplayRune(name) {
		return ErrInvalidDisplayName
	}
	return nil
}

// hasUnsafeDisplayRune reports whether text contains a control, format, line-separator or paragraph-separator character.
func hasUnsafeDisplayRune(text string) bool {
	for _, character := range text {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) || unicode.Is(unicode.Zl, character) || unicode.Is(unicode.Zp, character) {
			return true
		}
	}
	return false
}
