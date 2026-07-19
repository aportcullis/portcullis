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
	// MigrateDatabaseURL is the owner DSN migrations run with. The recommended
	// production shape is a one-shot `portcullis migrate` process/container that
	// is the ONLY place this is set — the serving process then never holds owner
	// credentials (ADR-0009). When set on the server, migrations still run at
	// startup (compatibility); when empty, OwnerDSN falls back to DatabaseURL
	// for the migrate command and single-role dev.
	MigrateDatabaseURL string `mapstructure:"migrate_database_url"`
	// RuntimeRole names the least-privilege DB role the migrations create and
	// grant (ADR-0009). Roles are cluster-wide: give each install on a SHARED
	// PostgreSQL cluster its own name, or their privileges merge.
	RuntimeRole string `mapstructure:"runtime_role"`
	// AllowPrivilegedRuntime downgrades the runtime-connection verification
	// failure to a warning, so a single-role dev setup (DATABASE_URL = the schema
	// owner) can still boot. INSECURE — never set in production (ADR-0009). It
	// has NO effect on startup migration — that is StartupMigrate's job.
	AllowPrivilegedRuntime bool `mapstructure:"allow_privileged_runtime"`
	// StartupMigrate makes `portcullis serve` apply migrations itself before
	// serving: "true", "false", or empty (= unset: defaults to "a dedicated
	// owner DSN is configured on this process", the pre-one-shot deployment
	// shape). A tri-state string because "explicitly disabled" and "unset"
	// must stay distinguishable. Deliberately decoupled from
	// AllowPrivilegedRuntime — a security-debug flag must not silently change
	// who migrates the schema (self-review F6). Single-role dev sets it
	// explicitly (see .env.example and web/e2e/server.sh).
	StartupMigrate string `mapstructure:"startup_migrate"`
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
	// LoginBackoffThreshold is how many consecutive failed password attempts lock
	// an account (ADR-0006 Parameters; default 5). The lockout window starts at
	// LoginBackoffBase (default 1m) and doubles per further failure up to
	// LoginBackoffCap (default 15m), with ±20% jitter on the expiry.
	LoginBackoffThreshold int           `mapstructure:"login_backoff_threshold"`
	LoginBackoffBase      time.Duration `mapstructure:"login_backoff_base"`
	LoginBackoffCap       time.Duration `mapstructure:"login_backoff_cap"`
	// ConnectionTestTimeout bounds one target-database connection test — the
	// dial, TLS handshake, authentication, and ping (ADR-0014; default 10s,
	// range [1s, 60s]).
	ConnectionTestTimeout time.Duration `mapstructure:"connection_test_timeout"`
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
func (c Config) GoogleEnabled() bool { return strings.TrimSpace(c.GoogleClientID) != "" }

// OwnerDSN is the DSN migrations run with: the dedicated owner DSN when set,
// else the runtime DSN (single-role dev — the two are the same login).
func (c Config) OwnerDSN() string {
	if c.MigrateDatabaseURL != "" {
		return c.MigrateDatabaseURL
	}
	return c.DatabaseURL
}

// StartupMigrationEnabled reports whether `portcullis serve` should apply
// migrations itself before serving: the explicit STARTUP_MIGRATE setting wins;
// unset defaults to "a dedicated owner DSN is configured on this process"
// (compatibility with the pre-one-shot deployment shape). The recommended
// production shape leaves both unset and runs `portcullis migrate` as a
// one-shot step, so the serving process never holds owner credentials.
func (c Config) StartupMigrationEnabled() bool {
	switch c.StartupMigrate {
	case "true":
		return true
	case "false":
		return false
	}
	return c.MigrateDatabaseURL != ""
}

// ResolveGoogleClientSecret returns the client secret from whichever source is
// configured, trimming trailing whitespace from a mounted file (as the master
// key file is handled). Load guarantees exactly one source when Google is
// enabled.
func (c Config) ResolveGoogleClientSecret() (string, error) {
	secretFile := strings.TrimSpace(c.GoogleClientSecretFile)
	if secretFile == "" {
		return strings.TrimSpace(c.GoogleClientSecret), nil
	}
	b, err := os.ReadFile(secretFile)
	if err != nil {
		return "", fmt.Errorf("read google_client_secret_file: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}
