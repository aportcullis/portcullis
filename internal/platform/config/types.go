package config

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/platform/publicorigin"
)

// Config is the typed runtime configuration. Fields map to env vars as PORTCULLIS_<FIELD>, with "." in nested keys replaced by "_". Load (config.go) parses and validates it.
type Config struct {
	// Addr is the HTTP listen address. A bare port like "8080" is accepted and normalized to ":8080" (all interfaces).
	Addr string `mapstructure:"addr"`
	// DatabaseURL is the metadata PostgreSQL DSN the server runs with. In production this should be a login user holding only the portcullis_runtime role (ADR-0009), not the schema owner.
	DatabaseURL string `mapstructure:"database_url"`
	// DatabaseMaxConns caps metadata pool connections (default 16, range [2, 200]); boot-only, so it is not a settings-store tunable (ADR-0017).
	DatabaseMaxConns int32 `mapstructure:"database_max_conns"`
	// DatabaseAcquireTimeout bounds waiting for a pooled metadata connection, including dialing a new one (default 10s, range [100ms, 1m]).
	DatabaseAcquireTimeout time.Duration `mapstructure:"database_acquire_timeout"`
	// DatabaseStatementTimeout is the metadata session statement_timeout (default 30s, range [1s, 10m]).
	DatabaseStatementTimeout time.Duration `mapstructure:"database_statement_timeout"`
	// DatabaseLockTimeout is the metadata session lock_timeout (default 10s, range [100ms, 5m], never above the statement timeout).
	DatabaseLockTimeout time.Duration `mapstructure:"database_lock_timeout"`
	// DatabaseIdleInTransactionTimeout terminates a metadata session idle inside a transaction (default 1m, range [1s, 1h]).
	DatabaseIdleInTransactionTimeout time.Duration `mapstructure:"database_idle_in_transaction_timeout"`
	// MigrateDatabaseURL supplies owner credentials for migration; an empty value falls back to DatabaseURL. Set it only on the separate migrate process in production.
	MigrateDatabaseURL string `mapstructure:"migrate_database_url"`
	// RuntimeRole names the least-privilege DB role the migrations create and grant (ADR-0009). Roles are cluster-wide: give each install on a SHARED PostgreSQL cluster its own name, or their privileges merge.
	RuntimeRole string `mapstructure:"runtime_role"`
	// AllowPrivilegedRuntime downgrades the runtime-connection verification failure to a warning, so a single-role dev setup (DATABASE_URL = the schema owner) can still boot. INSECURE — never set in production (ADR-0009). It has NO effect on startup migration — that is StartupMigrate's job.
	AllowPrivilegedRuntime bool `mapstructure:"allow_privileged_runtime"`
	// StartupMigrate is true, false, or unset; unset enables migration when a dedicated owner DSN exists. AllowPrivilegedRuntime does not affect this decision.
	StartupMigrate string `mapstructure:"startup_migrate"`
	// DrainDelay is how long readiness reports "draining" on shutdown before connections close, giving Kubernetes time to deregister the pod.
	DrainDelay time.Duration `mapstructure:"drain_delay"`
	// ShutdownTimeout bounds the graceful drain of in-flight requests.
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	// ShutdownInterruptTimeout is the part of ShutdownTimeout reserved for interrupting executions that outlive the HTTP drain and recording their outcomes.
	ShutdownInterruptTimeout time.Duration `mapstructure:"shutdown_interrupt_timeout"`
	// LogLevel is one of debug, info, warn, error.
	LogLevel string `mapstructure:"log_level"`
	// LogFormat is "json" (default) or "text".
	LogFormat string `mapstructure:"log_format"`
	// MasterKey is the base64 encoding of a 32-byte AEAD master key.
	MasterKey string `mapstructure:"master_key"`
	// MasterKeyFile points to a file whose contents are the base64 master key (preferred in production via a mounted secret).
	MasterKeyFile string `mapstructure:"master_key_file"`
	// MasterKeyPrevious retains versioned historical keys for decryption and digest verification.
	MasterKeyPrevious     string `mapstructure:"master_key_previous"`
	MasterKeyPreviousFile string `mapstructure:"master_key_previous_file"`
	// Argon2MaxConcurrent caps concurrent Argon2id hashes. Each hash costs ~64 MiB, so this bounds hashing memory independently of core count (defaults to 2).
	Argon2MaxConcurrent int `mapstructure:"argon2_max_concurrent"`
	// LoginBackoffThreshold is how many consecutive failed password attempts lock an account (ADR-0006 Parameters; default 5). The lockout window starts at LoginBackoffBase (default 1m) and doubles per further failure up to LoginBackoffCap (default 15m), with ±20% jitter on the expiry.
	LoginBackoffThreshold int           `mapstructure:"login_backoff_threshold"`
	LoginBackoffBase      time.Duration `mapstructure:"login_backoff_base"`
	LoginBackoffCap       time.Duration `mapstructure:"login_backoff_cap"`
	// ConnectionTestTimeout bounds one target-database connection test — the dial, TLS handshake, authentication, and ping (ADR-0014; default 10s, range [1s, 60s]).
	ConnectionTestTimeout time.Duration `mapstructure:"connection_test_timeout"`
	// ApprovalValidity is how long an approved access request stays executable, measured from the Nth approval or the system auto-approval (PRD §4.3; ADR-0018; default 24h, range [15m, 168h]).
	ApprovalValidity time.Duration `mapstructure:"approval_validity"`
	// ExecutionLockTimeout bounds each lock wait of a governed target execution (ADR-0021; default 5s, range [1s, 60s]).
	ExecutionLockTimeout time.Duration `mapstructure:"execution_lock_timeout"`
	// TrustedProxies is a comma-separated list of CIDRs whose requests carry a real client IP in X-Forwarded-For (used for rate-limit keying). Empty (default) means the direct peer IP is trusted — the correct setting for direct exposure.
	TrustedProxies []string `mapstructure:"trusted_proxies"`
	// ConnectionAllowedCIDRs is a comma-separated list of CIDRs connection tests and governed executions may dial; empty (default) permits every address except loopback (ADR-0051).
	ConnectionAllowedCIDRs []string `mapstructure:"connection_allowed_cidrs"`
	// ConnectionDeniedCIDRs is a comma-separated list of CIDRs that are refused even inside the allow list; link-local, metadata, unspecified, multicast and broadcast addresses are always refused (ADR-0051).
	ConnectionDeniedCIDRs []string `mapstructure:"connection_denied_cidrs"`
	// PublicOrigins is a comma-separated list of absolute browser origins (https://portcullis.example.com) this installation is served under. Requests whose Host names none of them are refused; empty (default) admits only loopback hosts, which suits the local demo but not a shared deployment (ADR-0052).
	PublicOrigins []string `mapstructure:"public_origins"`

	// Google login (OIDC, ADR-0007). All three unset ⇒ the feature is disabled and password login is unaffected; a partial setup fails startup. GoogleClientID is the OAuth client id from the Google Cloud console.
	GoogleClientID string `mapstructure:"google_client_id"`
	// GoogleClientSecret is the client secret, inline.
	GoogleClientSecret string `mapstructure:"google_client_secret"`
	// GoogleClientSecretFile points to a file whose contents are the client secret (preferred in production via a mounted secret, as the master key is).
	GoogleClientSecretFile string `mapstructure:"google_client_secret_file"`
	// GoogleRedirectURL is the absolute callback URL registered with Google — https://<host>/auth/google/callback (http allowed for localhost dev).
	GoogleRedirectURL string `mapstructure:"google_redirect_url"`

	// Bootstrap requires an email and one password source. Startup creates the first admin or skips an existing installation; invalid credentials fail startup (ADR-0006).
	BootstrapAdminEmail string `mapstructure:"bootstrap_admin_email"`
	// BootstrapAdminPassword is the initial password, inline.
	BootstrapAdminPassword string `mapstructure:"bootstrap_admin_password"`
	// BootstrapAdminPasswordFile points to a file whose contents are the initial password (preferred in production via a mounted secret, as the master key is).
	BootstrapAdminPasswordFile string `mapstructure:"bootstrap_admin_password_file"`
	// BootstrapAdminDisplayName is the admin's display name (default "Admin").
	BootstrapAdminDisplayName string `mapstructure:"bootstrap_admin_display_name"`
	// SetupTokenFile, when set, receives the first-run setup token (mode 0600) instead of the log; the token gates the interactive /bootstrap form while no user exists (ADR-0052).
	SetupTokenFile string `mapstructure:"setup_token_file"`

	// trustedProxyNets is TrustedProxies parsed to networks, populated by Load.
	trustedProxyNets []*net.IPNet
	// connectionDestinations is the policy parsed from the connection CIDR lists, populated by Load.
	connectionDestinations connection.DestinationPolicy
	// publicOriginPolicy is PublicOrigins parsed and normalized, populated by Load.
	publicOriginPolicy publicorigin.Policy
}

// TrustedProxyNets returns the parsed trusted-proxy networks (see TrustedProxies).
func (c Config) TrustedProxyNets() []*net.IPNet { return c.trustedProxyNets }

// HTTPDrainTimeout is how long in-flight requests may drain before executions are interrupted.
func (c Config) HTTPDrainTimeout() time.Duration {
	return c.ShutdownTimeout - c.ShutdownInterruptTimeout
}

// ConnectionDestinationPolicy returns the parsed connection destination policy (see ConnectionAllowedCIDRs).
func (c Config) ConnectionDestinationPolicy() connection.DestinationPolicy {
	return c.connectionDestinations
}

// PublicOriginPolicy returns the parsed public-origin policy (see PublicOrigins).
func (c Config) PublicOriginPolicy() publicorigin.Policy { return c.publicOriginPolicy }

// GoogleEnabled reports whether Google login is configured. Load has already validated all-or-nothing, so the client id alone is decisive.
func (c Config) GoogleEnabled() bool { return strings.TrimSpace(c.GoogleClientID) != "" }

// OwnerDSN is the DSN migrations run with: the dedicated owner DSN when set, else the runtime DSN (single-role dev — the two are the same login).
func (c Config) OwnerDSN() string {
	if c.MigrateDatabaseURL != "" {
		return c.MigrateDatabaseURL
	}
	return c.DatabaseURL
}

// StartupMigrationEnabled honors explicit STARTUP_MIGRATE; unset enables migration only when a dedicated owner DSN is configured.
func (c Config) StartupMigrationEnabled() bool {
	switch c.StartupMigrate {
	case "true":
		return true
	case "false":
		return false
	}
	return c.MigrateDatabaseURL != ""
}

// BootstrapAdminEnabled reports whether a config-based bootstrap admin is configured. Load has already validated all-or-nothing, so the email alone is decisive.
func (c Config) BootstrapAdminEnabled() bool { return c.BootstrapAdminEmail != "" }

// ResolveBootstrapAdminPassword preserves password whitespace, removing exactly one trailing newline from file input.
func (c Config) ResolveBootstrapAdminPassword() (string, error) {
	file := strings.TrimSpace(c.BootstrapAdminPasswordFile)
	if file == "" {
		return c.BootstrapAdminPassword, nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read bootstrap_admin_password_file: %w", err)
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r"), nil
}

// ResolveGoogleClientSecret returns the client secret from whichever source is configured, trimming trailing whitespace from a mounted file (as the master key file is handled). Load guarantees exactly one source when Google is enabled.
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
