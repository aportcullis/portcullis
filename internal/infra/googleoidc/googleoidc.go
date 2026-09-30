// Package googleoidc adapts Google sign-in (OIDC Authorization Code + PKCE, ADR-0007) to the auth service's OIDCProvider port. It is the only package that touches x/oauth2 and go-oidc; the app layer sees verified claims only.
package googleoidc

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// New discovers the issuer and builds the adapter. Discovery runs at construction so a misconfigured or unreachable provider fails the boot, not the first login (fail-fast, as with the keyring). The issuer is a parameter so tests can substitute a fake provider; production passes GoogleIssuer.
func New(ctx context.Context, issuerURL, clientID, clientSecret, redirectURL string) (*Client, error) {
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("googleoidc: provider discovery: %w", err)
	}
	return &Client{
		oauth: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     provider.Endpoint(),
			// Exactly the sign-in scopes (ADR-0007). No offline access is ever requested — sign-in needs the ID token once, never a refresh token.
			Scopes: []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
	}, nil
}

// AuthCodeURL builds the authorization URL for one flow: the S256 challenge derived from verifier (plain is prohibited — RFC 7636 / ADR-0007) plus the state and nonce minted by the service.
func (c *Client) AuthCodeURL(state, nonce, verifier string) string {
	return c.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce))
}

// Exchange redeems the code with the PKCE verifier and verifies the ID token (signature against the issuer's JWKS, iss, aud, exp). The nonce is returned unverified for the service to compare. Error messages never embed the code, verifier, or token material.
func (c *Client) Exchange(ctx context.Context, code, verifier string) (identity.OIDCClaims, error) {
	token, err := c.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return identity.OIDCClaims{}, fmt.Errorf("googleoidc: code exchange failed: %w", redactOAuthError(err))
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return identity.OIDCClaims{}, errors.New("googleoidc: token response has no id_token")
	}
	idToken, err := c.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return identity.OIDCClaims{}, fmt.Errorf("googleoidc: id token rejected: %w", err)
	}
	var extra struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idToken.Claims(&extra); err != nil {
		return identity.OIDCClaims{}, fmt.Errorf("googleoidc: parse claims: %w", err)
	}
	return identity.OIDCClaims{
		Issuer:        idToken.Issuer,
		Subject:       idToken.Subject,
		Email:         extra.Email,
		EmailVerified: extra.EmailVerified,
		Nonce:         idToken.Nonce,
	}, nil
}

// redactOAuthError strips the provider's response body from an exchange failure: oauth2.RetrieveError echoes it verbatim, and while it should never carry our code or verifier, the transcript of an auth failure is not ours to log (conventions/security.md). The status code alone identifies the failure class.
func redactOAuthError(err error) error {
	var rerr *oauth2.RetrieveError
	if errors.As(err, &rerr) {
		return fmt.Errorf("provider returned %s (%s)", rerr.Response.Status, rerr.ErrorCode)
	}
	return err
}
