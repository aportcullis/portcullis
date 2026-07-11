package config

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// Config is the typed runtime configuration. Fields map to env vars as
// PORTCULLIS_<FIELD>, with "." in nested keys replaced by "_". Load (config.go)
// parses and validates it.
type Config struct {
	// Addr is the HTTP listen address. A bare port like "8080" is accepted and
	// normalized to ":8080" (all interfaces).
	Addr string `mapstructure:"addr"`
	// DatabaseURL is the metadata PostgreSQL DSN the server runs with. In
	// production this should be a login user holding only the portcullis_runtime
	// role (ADR-0009), not the schema owner.
	DatabaseURL string `mapstructure:"database_url"`
	// MigrateDatabaseURL is the owner DSN used only to run migrations at startup.
	// Empty means DatabaseURL is used for both (single-role dev setup).
	MigrateDatabaseURL string `mapstructure:"migrate_database_url"`
	// RuntimeRole names the least-privilege DB role the migrations create and
	// grant (ADR-0009). Roles are cluster-wide: give each install on a SHARED
	// PostgreSQL cluster its own name, or their privileges merge.
	RuntimeRole string `mapstructure:"runtime_role"`
	// AllowPrivilegedRuntime downgrades the runtime-connection verification
	// failure to a warning, so a single-role dev setup (DATABASE_URL = the schema
	// owner) can still boot. INSECURE — never set in production (ADR-0009).
	AllowPrivilegedRuntime bool `mapstructure:"allow_privileged_runtime"`
	// DrainDelay is how long readiness reports "draining" on shutdown before
	// connections close, giving Kubernetes time to deregister the pod.
	DrainDelay time.Duration `mapstructure:"drain_delay"`
	// ShutdownTimeout bounds the graceful drain of in-flight requests.
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	// LogLevel is one of debug, info, warn, error.
	LogLevel string `mapstructure:"log_level"`
	// LogFormat is "json" (default) or "text".
	LogFormat string `mapstructure:"log_format"`
	// MasterKey is the base64 encoding of a 32-byte AEAD master key.
	MasterKey string `mapstructure:"master_key"`
	// MasterKeyFile points to a file whose contents are the base64 master key
	// (preferred in production via a mounted secret).
	MasterKeyFile string `mapstructure:"master_key_file"`
	// Argon2MaxConcurrent caps concurrent Argon2id hashes. Each hash costs ~64 MiB,
	// so this bounds hashing memory independently of core count (defaults to 2).
	Argon2MaxConcurrent int `mapstructure:"argon2_max_concurrent"`
	// TrustedProxies is a comma-separated list of CIDRs whose requests carry a real
	// client IP in X-Forwarded-For (used for rate-limit keying). Empty (default)
	// means the direct peer IP is trusted — the correct setting for direct exposure.
	TrustedProxies []string `mapstructure:"trusted_proxies"`

	// Google login (OIDC, ADR-0007). All three unset ⇒ the feature is disabled
	// and password login is unaffected; a partial setup fails startup.
	// GoogleClientID is the OAuth client id from the Google Cloud console.
	GoogleClientID string `mapstructure:"google_client_id"`
	// GoogleClientSecret is the client secret, inline.
	GoogleClientSecret string `mapstructure:"google_client_secret"`
	// GoogleClientSecretFile points to a file whose contents are the client
	// secret (preferred in production via a mounted secret, as the master key is).
	GoogleClientSecretFile string `mapstructure:"google_client_secret_file"`
	// GoogleRedirectURL is the absolute callback URL registered with Google —
	// https://<host>/auth/google/callback (http allowed for localhost dev).
	GoogleRedirectURL string `mapstructure:"google_redirect_url"`

	// trustedProxyNets is TrustedProxies parsed to networks, populated by Load.
	trustedProxyNets []*net.IPNet
}

// TrustedProxyNets returns the parsed trusted-proxy networks (see TrustedProxies).
func (c Config) TrustedProxyNets() []*net.IPNet { return c.trustedProxyNets }

// GoogleEnabled reports whether Google login is configured. Load has already
// validated all-or-nothing, so the client id alone is decisive.
func (c Config) GoogleEnabled() bool { return c.GoogleClientID != "" }

// ResolveGoogleClientSecret returns the client secret from whichever source is
// configured, trimming trailing whitespace from a mounted file (as the master
// key file is handled). Load guarantees exactly one source when Google is
// enabled.
func (c Config) ResolveGoogleClientSecret() (string, error) {
	if c.GoogleClientSecretFile == "" {
		return c.GoogleClientSecret, nil
	}
	b, err := os.ReadFile(c.GoogleClientSecretFile)
	if err != nil {
		return "", fmt.Errorf("read google_client_secret_file: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}
