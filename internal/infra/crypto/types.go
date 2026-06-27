package crypto

// KeyVersion identifies which master key produced a ciphertext or digest, so
// keys can be rotated while old data stays decryptable.
type KeyVersion uint32

// Blob is an envelope-encrypted value: the data is sealed with a per-record DEK,
// and the DEK is wrapped under a key derived from the master key version.
type Blob struct {
	KeyVersion KeyVersion
	WrappedDEK []byte // nonce-prefixed, AEAD-wrapped data-encryption key
	Nonce      []byte // nonce for the data ciphertext
	Ciphertext []byte // AES-256-GCM ciphertext (includes tag)
}

// Digest is a keyed HMAC tag plus the key version used to produce it.
type Digest struct {
	KeyVersion KeyVersion
	Sum        []byte
}

// Argon2Params is the password-hashing cost profile. It is encoded into every
// hash so a stored hash can be re-hashed when the active profile changes.
type Argon2Params struct {
	Memory  uint32 // KiB
	Time    uint32
	Threads uint8
	KeyLen  uint32
	SaltLen uint32
}
