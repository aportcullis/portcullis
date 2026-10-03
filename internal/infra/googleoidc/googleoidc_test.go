package googleoidc_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/infra/googleoidc"
	"github.com/aportcullis/portcullis/internal/infra/oidctest"
)

const (
	clientID    = "client-1"
	redirectURL = "https://portcullis.example/auth/google/callback"
	verifier    = "test-verifier-0123456789abcdefghijklmnopqrstuv"
)

func newClient(t *testing.T) (*googleoidc.Client, *oidctest.Issuer) {
	t.Helper()
	issuer := oidctest.New(t)
	c, err := googleoidc.New(context.Background(), issuer.URL(), clientID, "secret", redirectURL)
	if err != nil {
		t.Fatalf("googleoidc.New: %v", err)
	}
	return c, issuer
}

func challenge() string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestNewFailsWhenDiscoveryUnreachable(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// A closed port: discovery must fail construction (fail-fast at boot).
	if _, err := googleoidc.New(ctx, "http://127.0.0.1:1", clientID, "secret", redirectURL); err == nil {
		t.Error("New with unreachable issuer = nil error, want failure")
	}
}

func TestAuthCodeURLCarriesFlowParameters(t *testing.T) {
	t.Parallel()
	c, issuer := newClient(t)

	raw := c.AuthCodeURL("state-1", "nonce-1", verifier)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse auth URL %q: %v", raw, err)
	}
	q := u.Query()
	if got := u.Scheme + "://" + u.Host; issuer.URL() != got {
		t.Errorf("auth URL host = %q, want the issuer %q", got, issuer.URL())
	}
	if q.Get("client_id") != clientID || q.Get("redirect_uri") != redirectURL {
		t.Errorf("client_id/redirect_uri = %q/%q", q.Get("client_id"), q.Get("redirect_uri"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", q.Get("response_type"))
	}
	if q.Get("state") != "state-1" || q.Get("nonce") != "nonce-1" {
		t.Errorf("state/nonce = %q/%q", q.Get("state"), q.Get("nonce"))
	}
	// PKCE: the URL must carry the S256 challenge derived from OUR verifier — plain is prohibited (ADR-0007).
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("code_challenge") != challenge() {
		t.Errorf("code_challenge = %q, want S256(verifier) = %q", q.Get("code_challenge"), challenge())
	}

	if got := q.Get("scope"); got != "openid email profile" {
		t.Errorf("scope = %q, want %q", got, "openid email profile")
	}
	if q.Has("access_type") {
		t.Errorf("auth URL requests access_type=%q; offline access is prohibited", q.Get("access_type"))
	}
}

func TestExchangeReturnsVerifiedClaims(t *testing.T) {
	t.Parallel()
	c, issuer := newClient(t)
	code := issuer.MintCode(oidctest.CodeOptions{
		Challenge: challenge(), Nonce: "nonce-1", Audience: clientID,
		Subject: "sub-1", Email: "user@example.com", EmailVerified: true,
	})

	claims, err := c.Exchange(context.Background(), code, verifier)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if claims.Issuer != issuer.URL() || claims.Subject != "sub-1" {
		t.Errorf("claims identity = %q/%q", claims.Issuer, claims.Subject)
	}
	if claims.Email != "user@example.com" || !claims.EmailVerified {
		t.Errorf("claims email = %q (verified %v)", claims.Email, claims.EmailVerified)
	}

	if claims.Nonce != "nonce-1" {
		t.Errorf("claims nonce = %q, want nonce-1", claims.Nonce)
	}
}

func TestExchangeRejectsProtocolViolations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		opts oidctest.CodeOptions
	}{
		{"wrong audience", oidctest.CodeOptions{Challenge: challenge(), Nonce: "n", Audience: "another-client", Subject: "sub-1"}},
		{"expired token", oidctest.CodeOptions{Challenge: challenge(), Nonce: "n", Audience: clientID, Subject: "sub-1", ExpiresIn: -time.Minute}},
		{"bad signature", oidctest.CodeOptions{Challenge: challenge(), Nonce: "n", Audience: clientID, Subject: "sub-1", BadSignature: true}},
		{"missing id_token", oidctest.CodeOptions{Challenge: challenge(), Nonce: "n", Audience: clientID, Subject: "sub-1", OmitIDToken: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, issuer := newClient(t)
			code := issuer.MintCode(tc.opts)
			if _, err := c.Exchange(context.Background(), code, verifier); err == nil {
				t.Errorf("Exchange(%s) = nil error, want rejection", tc.name)
			}
		})
	}

	t.Run("wrong verifier", func(t *testing.T) {
		t.Parallel()
		c, issuer := newClient(t)
		code := issuer.MintCode(oidctest.CodeOptions{Challenge: challenge(), Nonce: "n", Audience: clientID, Subject: "sub-1"})
		wrong := "another-verifier-0123456789abcdefghijklmnopqrs"
		if _, err := c.Exchange(context.Background(), code, wrong); err == nil {
			t.Error("Exchange with a mismatched PKCE verifier must fail")
		}
	})

	t.Run("unknown code", func(t *testing.T) {
		t.Parallel()
		c, _ := newClient(t)
		if _, err := c.Exchange(context.Background(), "never-minted", verifier); err == nil {
			t.Error("Exchange with an unknown code must fail")
		}
	})
}

func TestExchangeErrorOmitsSecrets(t *testing.T) {
	t.Parallel()
	c, issuer := newClient(t)
	code := issuer.MintCode(oidctest.CodeOptions{Challenge: challenge(), Nonce: "n", Audience: "another-client", Subject: "sub-1"})
	_, err := c.Exchange(context.Background(), code, verifier)
	if err == nil {
		t.Fatal("want error")
	}
	for _, secret := range []string{code, verifier} {
		if msg := err.Error(); strings.Contains(msg, secret) {
			t.Errorf("error message leaks %q: %s", secret, msg)
		}
	}
}

func TestExchangeDistinguishesCurrentEmailOwnershipFromHistoricalVerification(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name, email, domain     string
		verified, authoritative bool
	}{
		{"Gmail", "user@gmail.com", "", true, true},
		{"case folded Gmail", "User@GMAIL.COM", "", true, true},
		{"third party", "user@example.com", "", true, false},
		{"Gmail suffix spoof", "user@gmail.com.example.com", "", true, false},
		{"Workspace", "user@example.com", "example.com", true, true},
		{"unverified Workspace", "user@example.com", "example.com", false, false},
		{"unverified Gmail", "user@gmail.com", "", false, false},
		{"empty email", "", "example.com", true, false},
		{"blank hosted domain", "user@example.com", " ", true, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			client, issuer := newClient(t)
			code := issuer.MintCode(oidctest.CodeOptions{Challenge: challenge(), Nonce: "nonce", Audience: clientID, Subject: "subject", Email: scenario.email, EmailVerified: scenario.verified, HostedDomain: scenario.domain})
			claims, err := client.Exchange(context.Background(), code, verifier)
			if err != nil {
				t.Fatal(err)
			}
			if claims.EmailAuthoritative != scenario.authoritative {
				t.Fatalf("current ownership=%v, want %v", claims.EmailAuthoritative, scenario.authoritative)
			}
		})
	}
}
