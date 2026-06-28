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
