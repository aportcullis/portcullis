// Package config loads runtime configuration from the environment via viper,
// binding env vars (prefixed PORTCULLIS_) onto a typed struct.
package config

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"

	"github.com/aportcullis/portcullis/internal/platform/logging"
)

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

	// Accept a bare port ("8080") as well as "host:port" (":8080"). An explicitly
	// empty address would bind a random port, so reject it.
	if cfg.Addr == "" {
		return Config{}, fmt.Errorf("addr must not be empty")
	}
	if !strings.Contains(cfg.Addr, ":") {
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

	// Enumerated logging config: reject unknown values instead of silently
	// falling back, so an operator's intent isn't quietly overridden. The logging
	// package owns the valid vocabulary (a second copy here would drift).
	if _, ok := logging.ParseLevel(cfg.LogLevel); !ok {
		return Config{}, fmt.Errorf("invalid log_level %q: want one of debug, info, warn, error", cfg.LogLevel)
	}
	if !logging.ValidFormat(cfg.LogFormat) {
		return Config{}, fmt.Errorf("invalid log_format %q: want json or text", cfg.LogFormat)
	}

	// Bounded numeric/duration config: a nonsensical value must fail startup, not
	// degrade silently (a huge Argon2 cap = OOM; a negative timeout = instant
	// expiry).
	if cfg.Argon2MaxConcurrent < 1 || cfg.Argon2MaxConcurrent > maxArgon2Concurrent {
		return Config{}, fmt.Errorf("argon2_max_concurrent %d out of range [1, %d]", cfg.Argon2MaxConcurrent, maxArgon2Concurrent)
	}
	if cfg.DrainDelay < 0 {
		return Config{}, fmt.Errorf("drain_delay must not be negative, got %s", cfg.DrainDelay)
	}
	if cfg.DrainDelay > maxDrainDelay {
		return Config{}, fmt.Errorf("drain_delay %s exceeds the %s maximum — it delays shutdown before draining in-flight requests (ADR-0010)", cfg.DrainDelay, maxDrainDelay)
	}
	if cfg.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("shutdown_timeout must be positive, got %s", cfg.ShutdownTimeout)
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
		// A /0 ("trust every proxy") makes X-Forwarded-For fully spoofable, which
		// defeats per-IP rate limiting entirely — almost certainly a misconfig.
		if ones, _ := network.Mask.Size(); ones == 0 {
			return Config{}, fmt.Errorf("trusted_proxies entry %q trusts the entire address space; X-Forwarded-For would be fully spoofable", c)
		}
		cfg.trustedProxyNets = append(cfg.trustedProxyNets, network)
	}
	return cfg, nil
}
