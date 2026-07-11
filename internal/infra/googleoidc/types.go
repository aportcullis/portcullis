package googleoidc

import (
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Client is the Google OIDC adapter behind the auth service's OIDCProvider
// port. Its constructor and methods live in googleoidc.go (file-split
// convention).
type Client struct {
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}
