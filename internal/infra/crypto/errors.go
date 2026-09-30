package crypto

import "errors"

var (
	ErrNoMasterKey = errors.New("crypto: no master key configured")

	ErrMultipleKeySources = errors.New("crypto: set either the master key or the key file, not both")

	ErrBadMasterKey = errors.New("crypto: master key must be base64 of 32 bytes")

	ErrUnknownKeyVersion = errors.New("crypto: unknown key version")
	// ErrDecrypt is returned for any AEAD open failure (authentication included).
	ErrDecrypt = errors.New("crypto: decryption failed")

	ErrBadHash = errors.New("crypto: malformed password hash")

	ErrInvalidParams = errors.New("crypto: invalid argon2 parameters")
)
