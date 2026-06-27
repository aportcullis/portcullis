package crypto

import "errors"

var (
	// ErrNoMasterKey means neither a key value nor a key file was provided.
	ErrNoMasterKey = errors.New("crypto: no master key configured")
	// ErrMultipleKeySources means both a key value and a key file were provided.
	ErrMultipleKeySources = errors.New("crypto: set either the master key or the key file, not both")
	// ErrBadMasterKey means the key did not decode to exactly 32 bytes.
	ErrBadMasterKey = errors.New("crypto: master key must be base64 of 32 bytes")
	// ErrUnknownKeyVersion means a blob/digest references an unloaded key version.
	ErrUnknownKeyVersion = errors.New("crypto: unknown key version")
	// ErrDecrypt is returned for any AEAD open failure (authentication included).
	ErrDecrypt = errors.New("crypto: decryption failed")
	// ErrBadHash means a password hash string is malformed or unsupported.
	ErrBadHash = errors.New("crypto: malformed password hash")
	// ErrInvalidParams means the Argon2 cost parameters are out of the safe range.
	ErrInvalidParams = errors.New("crypto: invalid argon2 parameters")
)
