package oidctest

import (
	"crypto/rsa"
	"net/http/httptest"
	"sync"
	"time"
)

// Issuer is a fake OIDC provider for tests: it serves the discovery document, a JWKS with one in-memory RSA key, and a token endpoint that redeems codes minted by MintCode. Codes are single-use and PKCE-checked (S256), so tests exercise the same protocol edges the real provider enforces.
type Issuer struct {
	srv *httptest.Server
	key *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]CodeOptions
	seq   int
}

// CodeOptions binds an authorization code to the flow that minted it and to the ID-token claims the token endpoint will sign. Zero-value knobs produce a valid token; the Bad*/Omit* switches script negative cases.
type CodeOptions struct {
	// Challenge is the S256 code challenge the redeeming code_verifier must hash to; redemption fails otherwise (RFC 7636).
	Challenge string
	// Nonce is echoed in the ID token.
	Nonce string

	Subject       string
	Email         string
	EmailVerified bool
	HostedDomain  string
	// Audience is the aud claim (the client id).
	Audience string
	// ExpiresIn offsets the token's exp from now (default 5 minutes; negative mints an already-expired token).
	ExpiresIn time.Duration

	// BadSignature signs the token with a key the JWKS does not serve.
	BadSignature bool
	// OmitIDToken returns a token response without an id_token field.
	OmitIDToken bool
}
