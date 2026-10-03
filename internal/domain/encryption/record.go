// Package encryption describes versioned encrypted records without exposing key material.
package encryption

// Record is a stored envelope or result-set key wrapper to rotate.
type Record struct {
	OrganizationID string
	ID             string
	Kind           string
	KeyVersion     uint32
	WrappedDEK     []byte
	Nonce          []byte
	Ciphertext     []byte
}
