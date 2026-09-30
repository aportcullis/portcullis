// Package oidctest is a test-only fake OIDC provider (Google stand-in): a httptest server exposing discovery, JWKS, and a PKCE-checking token endpoint, so the googleoidc adapter and the callback e2e flow can run against a real HTTP issuer without the network. It lives beside dbtest as shared test infrastructure — never imported by production code.
package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

const keyID = "oidctest-key-1"

// New starts the fake issuer and registers its shutdown with t.Cleanup.
func New(t *testing.T) *Issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("oidctest: generate RSA key: %v", err)
	}
	i := &Issuer{key: key, codes: map[string]CodeOptions{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", i.discovery)
	mux.HandleFunc("GET /jwks", i.jwks)
	mux.HandleFunc("POST /token", i.token)
	i.srv = httptest.NewServer(mux)
	t.Cleanup(i.srv.Close)
	return i
}

// URL is the issuer identifier (and base URL) of the fake provider.
func (i *Issuer) URL() string { return i.srv.URL }

// MintCode registers a single-use authorization code bound to opts, as if the user had just consented at the provider.
func (i *Issuer) MintCode(opts CodeOptions) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.seq++
	code := "code-" + strconv.Itoa(i.seq)
	i.codes[code] = opts
	return code
}

func (i *Issuer) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                i.srv.URL,
		"authorization_endpoint":                i.srv.URL + "/authorize",
		"token_endpoint":                        i.srv.URL + "/token",
		"jwks_uri":                              i.srv.URL + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (i *Issuer) jwks(w http.ResponseWriter, _ *http.Request) {
	pub := i.key.Public().(*rsa.PublicKey)
	writeJSON(w, map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"kid": keyID,
			"use": "sig",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}},
	})
}

// token redeems a code: single use, and the presented code_verifier must S256 to the challenge captured with the code.
func (i *Issuer) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		tokenError(w, "invalid_request")
		return
	}
	i.mu.Lock()
	opts, ok := i.codes[r.PostFormValue("code")]
	delete(i.codes, r.PostFormValue("code"))
	i.mu.Unlock()
	if !ok {
		tokenError(w, "invalid_grant")
		return
	}
	sum := sha256.Sum256([]byte(r.PostFormValue("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != opts.Challenge {
		tokenError(w, "invalid_grant")
		return
	}

	resp := map[string]any{"access_token": "fake-access-token", "token_type": "Bearer", "expires_in": 3600}
	if !opts.OmitIDToken {
		idToken, err := i.signIDToken(opts)
		if err != nil {
			tokenError(w, "server_error")
			return
		}
		resp["id_token"] = idToken
	}
	writeJSON(w, resp)
}

func (i *Issuer) signIDToken(opts CodeOptions) (string, error) {
	expiresIn := opts.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 5 * time.Minute
	}
	now := time.Now()
	claims := map[string]any{
		"iss":            i.srv.URL,
		"aud":            opts.Audience,
		"sub":            opts.Subject,
		"iat":            now.Unix(),
		"exp":            now.Add(expiresIn).Unix(),
		"nonce":          opts.Nonce,
		"email":          opts.Email,
		"email_verified": opts.EmailVerified,
	}
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": keyID}
	signingInput := encodeSegment(header) + "." + encodeSegment(claims)

	key := i.key
	if opts.BadSignature {
		rogue, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return "", err
		}
		key = rogue
	}
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func encodeSegment(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("oidctest: marshal jwt segment: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func tokenError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
