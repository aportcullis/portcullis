package crypto

import (
	"encoding/base64"
	"encoding/json"

	"github.com/google/uuid"
)

// CookieCodec seals a small payload into a cookie-safe string and opens it
// back, binding each value to a fresh flow id via the canonical AAD
// (ADR-0003). It is the stateless-record counterpart of a sealed table row:
// the flow id plays the record id, carried in cleartext ahead of the
// ciphertext because Open must rebuild the AAD before authenticating —
// tampering with it fails the AEAD open.
//
// Wire format: <flow-id-uuid> "." base64url(JSON(Blob)), both cookie-safe.
type CookieCodec struct {
	kr         *Keyring
	recordType string
	orgID      string
}

// NewOIDCPendingCodec builds the codec for the OIDC pending-auth cookie
// (ADR-0007), scoped to the single organization's id.
func NewOIDCPendingCodec(kr *Keyring, organizationID string) *CookieCodec {
	return &CookieCodec{kr: kr, recordType: RecordTypeOIDCPending, orgID: organizationID}
}

// Seal encrypts plaintext under a freshly minted flow id and returns the
// cookie value.
func (c *CookieCodec) Seal(plaintext []byte) (string, error) {
	flowID, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	id := flowID.String()
	blob, err := c.kr.Seal(plaintext, AAD(c.recordType, c.orgID, id))
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(blob)
	if err != nil {
		return "", err
	}
	return id + "." + base64.RawURLEncoding.EncodeToString(encoded), nil
}

// Open decrypts a cookie value produced by Seal. Every malformation — missing
// separator, non-UUID flow id, bad base64, bad JSON, or a failed
// authentication — returns ErrDecrypt with no further detail (fail closed).
func (c *CookieCodec) Open(value string) ([]byte, error) {
	id, body, ok := splitCookieValue(value)
	if !ok {
		return nil, ErrDecrypt
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, ErrDecrypt
	}
	var blob Blob
	if err := json.Unmarshal(raw, &blob); err != nil {
		return nil, ErrDecrypt
	}
	pt, err := c.kr.Open(blob, AAD(c.recordType, c.orgID, id))
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

// splitCookieValue separates and validates the flow-id prefix. The id is
// re-canonicalized through uuid.Parse so an alternate encoding of the same
// UUID can't produce a second valid AAD for one ciphertext.
func splitCookieValue(value string) (id, body string, ok bool) {
	const uuidLen = 36
	if len(value) < uuidLen+2 || value[uuidLen] != '.' {
		return "", "", false
	}
	parsed, err := uuid.Parse(value[:uuidLen])
	if err != nil {
		return "", "", false
	}
	return parsed.String(), value[uuidLen+1:], true
}
