package connection

import (
	"strings"
	"unicode"
)

// Byte-length bounds for credential fields. The user shares the host bound;
// the password bound is generous — it only guards against absurd payloads, the
// transport request cap does the real work.
const (
	maxCredentialUserLength     = 255
	maxCredentialPasswordLength = 1024
)

// Credential is the plaintext database login pair. It exists in memory only,
// between the API boundary and the seal/dial call — it is never persisted,
// logged, or returned (PRD §7.2, §8.1). No String method on purpose.
type Credential struct {
	User     string
	Password string
}

// NewCredential validates the pair. The user is required; the password may be
// empty — peer/trust-authenticated targets exist. Password content is not
// restricted (any byte is a legal password character), only bounded.
func NewCredential(user, password string) (Credential, error) {
	if strings.TrimSpace(user) == "" || len(user) > maxCredentialUserLength {
		return Credential{}, ErrInvalidCredential
	}
	if strings.ContainsFunc(user, unicode.IsControl) {
		return Credential{}, ErrInvalidCredential
	}
	if len(password) > maxCredentialPasswordLength {
		return Credential{}, ErrInvalidCredential
	}
	return Credential{User: user, Password: password}, nil
}

// SealedCredential is the AEAD envelope of a Credential as it rests in the
// database — the ADR-0003 Blob shape mirrored into the domain so application
// and infrastructure ports never name the crypto package's types. KeyVersion
// stays addressable for the eager key-rotation batch (ADR-0003).
type SealedCredential struct {
	KeyVersion uint32
	WrappedDEK []byte
	Nonce      []byte
	Ciphertext []byte
}
