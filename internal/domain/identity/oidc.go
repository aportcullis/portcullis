package identity

import "time"

// OIDCIdentity links a local user to an external OIDC subject (e.g. a Google account). The stable key is (Issuer, Subject); Email is informational and may change at the provider.
type OIDCIdentity struct {
	UserID    UserID
	Issuer    string
	Subject   string
	Email     string
	CreatedAt time.Time
}

// OIDCClaims carries provider-verified identity and email claims; the service must verify Nonce against the pending flow (ADR-0007).
type OIDCClaims struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Nonce         string
}
