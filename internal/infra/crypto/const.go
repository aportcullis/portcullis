package crypto

const (
	// masterKeyLen is the required master key size (AES-256 / HKDF input).
	masterKeyLen = 32
	// dekLen is the size of a per-record data-encryption key.
	dekLen = 32
	// subKeyLen is the size of HKDF-derived purpose keys.
	subKeyLen = 32
	// gcmNonceLen is the standard AES-GCM nonce size (ADR-0003 pins it): Seal
	// stores nonces of exactly this length and Open rejects anything else.
	gcmNonceLen = 12
)

// HKDF purpose labels derive distinct sub-keys from a master key version.
const (
	infoPayloadIntegrity = "portcullis/payload-integrity/v1"
	infoDEKWrap          = "portcullis/dek-wrap/v1"
)
