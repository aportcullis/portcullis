package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
)

// Seal envelope-encrypts plaintext: a fresh DEK encrypts the data, and the DEK
// is wrapped under the active version's dek-wrap key. associatedData (e.g. a
// record id + owner org id) is authenticated on both layers and must be
// supplied identically to Open.
func (k *Keyring) Seal(plaintext, associatedData []byte) (Blob, error) {
	wrapKey, err := k.derive(k.active, infoDEKWrap)
	if err != nil {
		return Blob{}, err
	}

	dek := make([]byte, dekLen)
	if _, err := rand.Read(dek); err != nil {
		return Blob{}, err
	}

	dataNonce, ciphertext, err := sealAEAD(dek, plaintext, associatedData)
	if err != nil {
		return Blob{}, err
	}
	wrapNonce, wrapped, err := sealAEAD(wrapKey, dek, associatedData)
	if err != nil {
		return Blob{}, err
	}

	return Blob{
		KeyVersion: k.active,
		WrappedDEK: append(wrapNonce, wrapped...),
		Nonce:      dataNonce,
		Ciphertext: ciphertext,
	}, nil
}

// Open reverses Seal. associatedData must match what was passed to Seal, or the
// authentication fails and ErrDecrypt is returned.
func (k *Keyring) Open(b Blob, associatedData []byte) ([]byte, error) {
	wrapKey, err := k.derive(b.KeyVersion, infoDEKWrap)
	if err != nil {
		return nil, err
	}

	ns := gcmNonceSize()
	if len(b.WrappedDEK) < ns {
		return nil, ErrDecrypt
	}
	wrapNonce, wrapped := b.WrappedDEK[:ns], b.WrappedDEK[ns:]

	dek, err := openAEAD(wrapKey, wrapNonce, wrapped, associatedData)
	if err != nil {
		return nil, err
	}
	return openAEAD(dek, b.Nonce, b.Ciphertext, associatedData)
}

func sealAEAD(key, plaintext, associatedData []byte) (nonce, ciphertext []byte, err error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, gcm.Seal(nil, nonce, plaintext, associatedData), nil
}

func openAEAD(key, nonce, ciphertext, associatedData []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, ErrDecrypt
	}
	pt, err := gcm.Open(nil, nonce, ciphertext, associatedData)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func gcmNonceSize() int {
	gcm, _ := newGCM(make([]byte, dekLen))
	return gcm.NonceSize()
}
