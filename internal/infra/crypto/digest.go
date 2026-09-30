package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
)

// Digest computes a keyed HMAC-SHA-256 over data using the active version's payload-integrity key. A keyed MAC (not a plain hash) is used so an attacker who exfiltrates long-retained audit rows cannot brute-force low-entropy values by hashing guesses.
func (k *Keyring) Digest(data []byte) (Digest, error) {
	key, err := k.derive(k.active, infoPayloadIntegrity)
	if err != nil {
		return Digest{}, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return Digest{KeyVersion: k.active, Sum: mac.Sum(nil)}, nil
}

// VerifyDigest recomputes the tag for data under d.KeyVersion and compares it in constant time.
func (k *Keyring) VerifyDigest(data []byte, d Digest) (bool, error) {
	key, err := k.derive(d.KeyVersion, infoPayloadIntegrity)
	if err != nil {
		return false, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return subtle.ConstantTimeCompare(mac.Sum(nil), d.Sum) == 1, nil
}
