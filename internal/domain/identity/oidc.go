package identity

import "time"

// OIDCIdentity links a local user to an external OIDC subject (e.g. a Google
// account). The stable key is (Issuer, Subject); Email is informational and may
// change at the provider.
type OIDCIdentity struct {
	UserID    UserID
	Issuer    string
	Subject   string
	Email     string
	CreatedAt time.Time
}

// OIDCClaims is the verified content of an ID token, as the provider adapter
// hands it to the application: identity (Issuer, Subject), the email with its
// provider-asserted verification flag, and the Nonce echoed by the provider.
// Signature/iss/aud/exp are verified by the adapter before these exist; the
// nonce is NOT — comparing it against the pending value is the use case's job
// (ADR-0007: go-oidc leaves nonce validation to the caller).
type OIDCClaims struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Nonce         string
}
