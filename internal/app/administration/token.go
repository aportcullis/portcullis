package administration

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// setupTokenBytes is the CSPRNG entropy of a password setup token (ADR-0053).
const setupTokenBytes = 32

// newPasswordSetupIssue mints a base64url setup token and the digest-only issue the store persists.
func newPasswordSetupIssue() (string, identity.PasswordSetupIssue, error) {
	raw := make([]byte, setupTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", identity.PasswordSetupIssue{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	return token, identity.PasswordSetupIssue{TokenHash: digest[:], Validity: identity.PasswordSetupValidity}, nil
}
