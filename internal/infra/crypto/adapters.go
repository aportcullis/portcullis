package crypto

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// This file adapts the crypto primitives to the small interfaces the application
// layer depends on (auth.PasswordHasher, auth.CSRFProtector,
// connection.CredentialCodec). The adapters satisfy those interfaces
// structurally, so this package does not import the app layer; the composition
// root (cmd/portcullis) wires them in.

// Argon2Hasher hashes and verifies passwords with a fixed Argon2id profile. A
// semaphore caps concurrent hashes so a login flood can't exhaust memory: each
// Argon2id op costs Memory bytes, so at most maxConcurrent × Memory is in flight
// (excess callers block until a slot frees).
type Argon2Hasher struct {
	params Argon2Params
	sem    chan struct{}
}

// NewArgon2Hasher builds a hasher with the given profile (defaulting to
// DefaultArgon2Params for the zero value) and a cap on concurrent hashes
// (coerced to at least 1).
func NewArgon2Hasher(p Argon2Params, maxConcurrent int) *Argon2Hasher {
	if p == (Argon2Params{}) {
		p = DefaultArgon2Params
	}
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Argon2Hasher{params: p, sem: make(chan struct{}, maxConcurrent)}
}

func (h *Argon2Hasher) Hash(ctx context.Context, password string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer func() { <-h.sem }()
	return HashPassword(password, h.params)
}

func (h *Argon2Hasher) Verify(ctx context.Context, password, encoded string) (ok, needsRehash bool, err error) {
	if err := h.acquire(ctx); err != nil {
		return false, false, err
	}
	defer func() { <-h.sem }()
	return VerifyPassword(password, encoded, h.params)
}

// acquire takes a concurrency slot, or returns the context error if the caller
// is cancelled while every slot is busy (so a login flood can't pile up blocked
// goroutines waiting on hashes that will never start).
func (h *Argon2Hasher) acquire(ctx context.Context) error {
	// Check cancellation first: with a free slot AND a cancelled ctx both ready, a
	// bare select would pick either at random, so an already-cancelled request
	// could still start a hash. This makes cancellation win deterministically.
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case h.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CSRFProtector issues and verifies session-bound CSRF tokens. A token is
// base64url(HMAC(session_token || nonce)) + "." + base64url(nonce) — nonce
// suffixed; the random nonce is the anti-collision value OWASP recommends, and the
// keyring HMAC binds the token to the session token (ADR-0006). It satisfies
// auth.CSRFProtector structurally.
type CSRFProtector struct {
	kr *Keyring
}

// NewCSRFProtector builds a protector over the keyring.
func NewCSRFProtector(kr *Keyring) *CSRFProtector {
	return &CSRFProtector{kr: kr}
}

// Issue returns a fresh token bound to the session token (a new nonce each call).
// The token is base64url(mac) + "." + base64url(nonce) — nonce suffixed (ADR-0006).
func (c *CSRFProtector) Issue(sessionToken string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	mac, err := c.mac(sessionToken, nonce)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(mac) + "." + enc.EncodeToString(nonce), nil
}

// Verify reports whether token is a valid CSRF token for the session token.
func (c *CSRFProtector) Verify(sessionToken, token string) bool {
	mb, nb, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	enc := base64.RawURLEncoding
	got, err := enc.DecodeString(mb)
	if err != nil {
		return false
	}
	nonce, err := enc.DecodeString(nb)
	if err != nil {
		return false
	}
	want, err := c.mac(sessionToken, nonce)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(want, got) == 1
}

// mac computes HMAC(session_token || nonce), namespaced to avoid collisions with
// other keyring digests.
func (c *CSRFProtector) mac(sessionToken string, nonce []byte) ([]byte, error) {
	msg := append([]byte("csrf:"+sessionToken+":"), nonce...)
	d, err := c.kr.Digest(msg)
	if err != nil {
		return nil, err
	}
	return d.Sum, nil
}

// ConnectionCredentialCodec seals a connection's database login pair into the
// ADR-0003 envelope under the canonical AAD (record type
// connection_credential + organization + connection id) and opens it back. It
// satisfies the connection app service's CredentialCodec port structurally.
// The plaintext is versioned JSON so fields can be added compatibly
// (ADR-0014).
type ConnectionCredentialCodec struct {
	kr *Keyring
}

// NewConnectionCredentialCodec builds a codec over the keyring.
func NewConnectionCredentialCodec(kr *Keyring) *ConnectionCredentialCodec {
	return &ConnectionCredentialCodec{kr: kr}
}

// connectionCredentialJSON is the sealed plaintext layout, version 1.
type connectionCredentialJSON struct {
	V        int    `json:"v"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// Seal envelope-encrypts the credential bound to (org, id) — a sealed value
// cannot be replayed onto another connection or organization.
func (c *ConnectionCredentialCodec) Seal(org identity.OrganizationID, id connection.ConnectionID, cred connection.Credential) (connection.SealedCredential, error) {
	plaintext, err := json.Marshal(connectionCredentialJSON{
		V: connectionCredentialVersion, User: cred.User, Password: cred.Password,
	})
	if err != nil {
		return connection.SealedCredential{}, fmt.Errorf("encode credential: %w", err)
	}
	blob, err := c.kr.Seal(plaintext, AAD(RecordTypeConnectionCredential, string(org), string(id)))
	if err != nil {
		return connection.SealedCredential{}, err
	}
	return connection.SealedCredential{
		KeyVersion: uint32(blob.KeyVersion),
		WrappedDEK: blob.WrappedDEK,
		Nonce:      blob.Nonce,
		Ciphertext: blob.Ciphertext,
	}, nil
}

// Open decrypts a sealed credential. A mismatched org or connection id fails
// closed with ErrDecrypt (both envelope layers authenticate the AAD).
func (c *ConnectionCredentialCodec) Open(org identity.OrganizationID, id connection.ConnectionID, sealed connection.SealedCredential) (connection.Credential, error) {
	blob := Blob{
		KeyVersion: KeyVersion(sealed.KeyVersion),
		WrappedDEK: sealed.WrappedDEK,
		Nonce:      sealed.Nonce,
		Ciphertext: sealed.Ciphertext,
	}
	plaintext, err := c.kr.Open(blob, AAD(RecordTypeConnectionCredential, string(org), string(id)))
	if err != nil {
		return connection.Credential{}, err
	}
	var v connectionCredentialJSON
	if err := json.Unmarshal(plaintext, &v); err != nil {
		return connection.Credential{}, fmt.Errorf("decode credential: %w", err)
	}
	if v.V != connectionCredentialVersion {
		return connection.Credential{}, fmt.Errorf("unsupported credential version %d", v.V)
	}
	return connection.Credential{User: v.User, Password: v.Password}, nil
}
