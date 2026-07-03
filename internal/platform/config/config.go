// Package config loads runtime configuration from the environment via viper,
// binding env vars (prefixed PORTCULLIS_) onto a typed struct.
package config

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// runtimeRolePattern is the shape runtime_role must have: it becomes a SQL
// identifier in migrations (PostgreSQL caps identifiers at 63 bytes). The same
// check runs again in postgres.Migrate (defense in depth).
var runtimeRolePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// Config is the typed runtime configuration. Fields map to env vars as
// PORTCULLIS_<FIELD>, with "." in nested keys replaced by "_".
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

	// trustedProxyNets is TrustedProxies parsed to networks, populated by Load.
	trustedProxyNets []*net.IPNet
}

// TrustedProxyNets returns the parsed trusted-proxy networks (see TrustedProxies).
func (c Config) TrustedProxyNets() []*net.IPNet { return c.trustedProxyNets }

// Load reads configuration from the environment and applies defaults.
func Load() (Config, error) {
	v := viper.NewWithOptions(
		viper.ExperimentalBindStruct(),
		viper.EnvKeyReplacer(strings.NewReplacer(".", "_")),
		viper.WithDecodeHook(mapstructure.ComposeDecodeHookFunc(
			mapstructure.TextUnmarshallerHookFunc(),
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		)),
	)

	v.SetEnvPrefix("PORTCULLIS")
	v.AutomaticEnv()

	v.SetDefault("addr", ":8080")
	v.SetDefault("drain_delay", time.Duration(0))
	v.SetDefault("shutdown_timeout", 15*time.Second)
	v.SetDefault("log_level", "info")
	v.SetDefault("log_format", "json")
	v.SetDefault("argon2_max_concurrent", 2)
	v.SetDefault("runtime_role", "portcullis_runtime")

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}

	// Accept a bare port ("8080") as well as "host:port" (":8080").
	if cfg.Addr != "" && !strings.Contains(cfg.Addr, ":") {
		cfg.Addr = ":" + cfg.Addr
	}

	// No dedicated owner DSN ⇒ migrate with the runtime DSN (dev single-role).
	if cfg.MigrateDatabaseURL == "" {
		cfg.MigrateDatabaseURL = cfg.DatabaseURL
	}

	// The runtime role name is spliced into migration SQL as an identifier —
	// fail fast on anything that isn't a plain lowercase identifier.
	if !runtimeRolePattern.MatchString(cfg.RuntimeRole) {
		return Config{}, fmt.Errorf("invalid runtime_role %q: must match %s", cfg.RuntimeRole, runtimeRolePattern)
	}

	// Parse trusted-proxy CIDRs once at startup so a typo fails fast rather than
	// silently disabling proxy-aware rate limiting.
	for _, c := range cfg.TrustedProxies {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		_, network, err := net.ParseCIDR(c)
		if err != nil {
			return Config{}, fmt.Errorf("invalid trusted_proxies entry %q: %w", c, err)
		}
		cfg.trustedProxyNets = append(cfg.trustedProxyNets, network)
	}
	return cfg, nil
}
