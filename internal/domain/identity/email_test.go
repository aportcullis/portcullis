package identity_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

func TestValidateEmail(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		email string
		valid bool
	}{
		{"simple", "admin@example.com", true},
		{"subdomain and multi-label tld", "a.b@sub.example.co.uk", true},
		{"empty", "", false},
		{"no at", "adminexample.com", false},
		{"double at", "a@@example.com", false},
		{"leading dot local", ".a@example.com", false},
		{"hyphen domain label", "a@-x.com", false},
		{"domain without dot", "a@b", false},
		{"space in address", "a b@example.com", false},
		{"display name form", "Admin <admin@example.com>", false},
		{"oversized", strings.Repeat("a", 250) + "@example.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := identity.ValidateEmail(tc.email)
			switch {
			case tc.valid && err != nil:
				t.Errorf("ValidateEmail(%q) = %v, want nil", tc.email, err)
			case !tc.valid && !errors.Is(err, identity.ErrInvalidEmail):
				t.Errorf("ValidateEmail(%q) = %v, want ErrInvalidEmail", tc.email, err)
			}
		})
	}
}
