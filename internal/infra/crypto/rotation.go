package crypto

import "github.com/aportcullis/portcullis/internal/domain/encryption"

// ActiveVersion returns the current key version for batch admission.
func (k *Keyring) ActiveVersion() uint32 { return uint32(k.active) }

// RotateRecord authenticates a historical envelope and replaces its encryption version.
func (k *Keyring) RotateRecord(record encryption.Record) (encryption.Record, error) {
	aad := AAD(record.Kind, record.OrganizationID, record.ID)
	if record.Kind != "result_set" {
		plain, err := k.Open(Blob{KeyVersion: KeyVersion(record.KeyVersion), WrappedDEK: record.WrappedDEK, Nonce: record.Nonce, Ciphertext: record.Ciphertext}, aad)
		if err != nil {
			return encryption.Record{}, err
		}
		sealed, err := k.Seal(plain, aad)
		if err != nil {
			return encryption.Record{}, err
		}
		record.KeyVersion = uint32(sealed.KeyVersion)
		record.WrappedDEK = sealed.WrappedDEK
		record.Nonce = sealed.Nonce
		record.Ciphertext = sealed.Ciphertext
		return record, nil
	}
	key, err := k.derive(KeyVersion(record.KeyVersion), infoDEKWrap)
	if err != nil {
		return encryption.Record{}, err
	}
	if len(record.WrappedDEK) < gcmNonceLen {
		return encryption.Record{}, ErrDecrypt
	}
	dek, err := openAEAD(key, record.WrappedDEK[:gcmNonceLen], record.WrappedDEK[gcmNonceLen:], aad)
	if err != nil {
		return encryption.Record{}, err
	}
	active, err := k.derive(k.active, infoDEKWrap)
	if err != nil {
		return encryption.Record{}, err
	}
	nonce, sealed, err := sealAEAD(active, dek, aad)
	if err != nil {
		return encryption.Record{}, err
	}
	record.KeyVersion = uint32(k.active)
	record.WrappedDEK = append(nonce, sealed...)
	return record, nil
}
