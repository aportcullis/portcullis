// Package crypto is the security spine: master-key loading, envelope encryption (AES-256-GCM), keyed payload digests (HMAC-SHA-256), and Argon2id password hashing. It is the only package that touches key material.
package crypto

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
)

// Keyring holds master keys by version and derives purpose-specific sub-keys. The active version is used for new writes; older versions stay loaded so existing data remains decryptable across rotation.
type Keyring struct {
	active KeyVersion
	keys   map[KeyVersion][]byte
}

// LoadKeyring loads the active master key from a base64 string or a file whose contents are base64. Exactly one source must be set; the key must be 32 bytes. It returns an error (so the caller can refuse to start) when absent or malformed.
func LoadKeyring(b64, file string) (*Keyring, error) {
	raw, err := readMasterKey(b64, file)
	if err != nil {
		return nil, err
	}
	if len(raw) != masterKeyLen {
		return nil, ErrBadMasterKey
	}
	return &Keyring{active: 1, keys: map[KeyVersion][]byte{1: raw}}, nil
}

// Active returns the key version used for new writes.
func (k *Keyring) Active() KeyVersion { return k.active }

func readMasterKey(b64, file string) ([]byte, error) {
	hasKey := strings.TrimSpace(b64) != ""
	hasFile := strings.TrimSpace(file) != ""
	switch {
	case hasKey && hasFile:
		// Refuse ambiguity: a stale env var silently overriding a mounted key file could re-encrypt with the wrong key and orphan existing data.
		return nil, ErrMultipleKeySources
	case hasKey:
		return decodeKey(b64)
	case hasFile:
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		return decodeKey(string(data))
	default:
		return nil, ErrNoMasterKey
	}
}

func decodeKey(s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, ErrBadMasterKey
	}
	return raw, nil
}

func (k *Keyring) master(v KeyVersion) ([]byte, error) {
	key, ok := k.keys[v]
	if !ok {
		return nil, ErrUnknownKeyVersion
	}
	return key, nil
}

// derive returns a sub-key for the given master version and purpose label.
func (k *Keyring) derive(v KeyVersion, info string) ([]byte, error) {
	master, err := k.master(v)
	if err != nil {
		return nil, err
	}
	return hkdf.Key(sha256.New, master, nil, info, subKeyLen)
}
