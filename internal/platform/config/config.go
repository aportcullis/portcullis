// Package config loads runtime configuration from the environment via viper,
// binding env vars (prefixed PORTCULLIS_) onto a typed struct.
package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"

	"github.com/aportcullis/portcullis/internal/domain/setting"
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
	v.SetDefault("log_format", "json")
	v.SetDefault("argon2_max_concurrent", 2)
	v.SetDefault("runtime_role", "portcullis_runtime")
	// Tier-C tunable defaults come from the settings registry (ADR-0017) —
	// one source for the env seed here and the DB store's fallback.
	for _, d := range setting.All() {
		switch d.Kind {
		case setting.KindDuration:
			v.SetDefault(string(d.Key), d.DefaultDuration())
		case setting.KindInt:
			v.SetDefault(string(d.Key), d.DefaultInt64())
		default:
			v.SetDefault(string(d.Key), d.Default)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}
	normalizeGoogleConfig(&cfg)

	// Accept a bare port ("8080") as well as "host:port" (":8080"). An explicitly
	// empty address would bind a random port, so reject it.
	if cfg.Addr == "" {
		return Config{}, fmt.Errorf("addr must not be empty")
	}
	if !strings.Contains(cfg.Addr, ":") {
		cfg.Addr = ":" + cfg.Addr
	}

	// Tri-state enum: reject anything but true/false/unset instead of silently
	// treating a typo ("ture") as unset (same fail-fast stance as log_level).
	cfg.StartupMigrate = strings.ToLower(strings.TrimSpace(cfg.StartupMigrate))
	if cfg.StartupMigrate != "" && cfg.StartupMigrate != "true" && cfg.StartupMigrate != "false" {
		return Config{}, fmt.Errorf("invalid startup_migrate %q: want true, false, or unset", cfg.StartupMigrate)
	}

	// The runtime role name is spliced into migration SQL as an identifier —
	// fail fast on anything that isn't a plain lowercase identifier.
	if !runtimeRolePattern.MatchString(cfg.RuntimeRole) {
		return Config{}, fmt.Errorf("invalid runtime_role %q: must match %s", cfg.RuntimeRole, runtimeRolePattern)
	}

	// Enumerated logging format: reject unknown values instead of silently
	// falling back, so an operator's intent isn't quietly overridden. The
	// logging package owns the format vocabulary; log_level validates through
	// its registry descriptor below (the level vocabularies are pinned
	// together by a logging white-box test). The env accepts any case
	// (ParseLevel's contract) — normalize here so the canonical lowercase
	// form is what the descriptor sees and what flows on.
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	if !logging.ValidFormat(cfg.LogFormat) {
		return Config{}, fmt.Errorf("invalid log_format %q: want json or text", cfg.LogFormat)
	}

	// Bounded numeric/duration config: a nonsensical value must fail startup, not
	// degrade silently (a huge Argon2 cap = OOM; a negative timeout = instant
	// expiry).
	if cfg.Argon2MaxConcurrent < 1 || cfg.Argon2MaxConcurrent > maxArgon2Concurrent {
		return Config{}, fmt.Errorf("argon2_max_concurrent %d out of range [1, %d]", cfg.Argon2MaxConcurrent, maxArgon2Concurrent)
	}
	// Tier-C tunables validate through their registry descriptors (ADR-0017):
	// the SAME bounds gate the env seed here and every settings-store
	// write/read, so the two paths cannot drift.
	for _, tc := range []struct {
		key  setting.Key
		text string
	}{
		{setting.KeyLogLevel, cfg.LogLevel},
		{setting.KeyLoginBackoffThreshold, strconv.Itoa(cfg.LoginBackoffThreshold)},
		{setting.KeyLoginBackoffBase, cfg.LoginBackoffBase.String()},
		{setting.KeyLoginBackoffCap, cfg.LoginBackoffCap.String()},
		{setting.KeyConnectionTestTimeout, cfg.ConnectionTestTimeout.String()},
	} {
		d, ok := setting.Lookup(tc.key)
		if !ok {
			return Config{}, fmt.Errorf("setting %q missing from the registry", tc.key)
		}
		if err := d.Validate(tc.text); err != nil {
			return Config{}, err
		}
	}
	if !setting.BackoffPairConsistent(cfg.LoginBackoffBase, cfg.LoginBackoffCap) {
		return Config{}, fmt.Errorf("login_backoff_cap %s below login_backoff_base %s", cfg.LoginBackoffCap, cfg.LoginBackoffBase)
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

	// Google login is all-or-nothing (ADR-0007): everything unset ⇒ disabled
	// (valid); a partial setup fails startup rather than surfacing on first use.
	if err := validateGoogleConfig(cfg); err != nil {
		return Config{}, err
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

// normalizeGoogleConfig gives all Google-login consumers one canonical view of
// environment values. In particular, whitespace-only client IDs must not pass
// validation as disabled and later enable routes with an invalid raw value.
func normalizeGoogleConfig(cfg *Config) {
	cfg.GoogleClientID = strings.TrimSpace(cfg.GoogleClientID)
	cfg.GoogleClientSecret = strings.TrimSpace(cfg.GoogleClientSecret)
	cfg.GoogleClientSecretFile = strings.TrimSpace(cfg.GoogleClientSecretFile)
	cfg.GoogleRedirectURL = strings.TrimSpace(cfg.GoogleRedirectURL)
}

// validateGoogleConfig enforces the all-or-nothing Google login setup: the
// client id, exactly one secret source, and a well-formed absolute redirect
// URL must all be present, or all absent (ADR-0007).
func validateGoogleConfig(cfg Config) error {
	clientID := cfg.GoogleClientID
	secretFile := cfg.GoogleClientSecretFile
	hasSecret := cfg.GoogleClientSecret != "" || secretFile != ""
	if clientID == "" && !hasSecret && cfg.GoogleRedirectURL == "" {
		return nil // feature disabled
	}
	if cfg.GoogleClientSecret != "" && secretFile != "" {
		return fmt.Errorf("set either google_client_secret or google_client_secret_file, not both")
	}
	if clientID == "" || !hasSecret || cfg.GoogleRedirectURL == "" {
		return fmt.Errorf("google login is partially configured: google_client_id, a client secret (google_client_secret or google_client_secret_file), and google_redirect_url must all be set to enable it, or all unset to disable it")
	}
	// A mounted secret can exist but be empty (for example, from a wrongly
	// populated Kubernetes Secret). Read it while loading configuration so the
	// server fails before advertising a Google login route that can never finish.
	secret, err := cfg.ResolveGoogleClientSecret()
	if err != nil {
		return err
	}
	if secret == "" {
		return fmt.Errorf("google login is configured with an empty client secret")
	}
	// The redirect URL is registered verbatim with Google; validate its shape so
	// a misconfiguration fails at boot, not at the consent screen (ADR-0007,
	// ADR-0010 fail-fast). The rules mirror Google's own registration rules
	// (web-verified): HTTPS with localhost/loopback as the only HTTP exception,
	// no fragment/userinfo, no raw non-loopback IP hosts — and the path must be
	// the one route this server actually mounts, or the authorization code would
	// land on the SPA/404. The messages name the key and the violated rule but
	// never echo the URL: a userinfo-shaped misconfiguration would put its
	// password into the startup log, and URL.Redacted() still keeps the username
	// (external review).
	u, err := url.Parse(cfg.GoogleRedirectURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("invalid google_redirect_url: must be an absolute http(s) URL")
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("invalid google_redirect_url: http is allowed only for localhost/loopback; production must use https (Google requirement)")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !ip.IsLoopback() {
		return fmt.Errorf("invalid google_redirect_url: raw IP hosts are not accepted by Google (loopback excepted); use a domain name")
	}
	if u.Path != GoogleCallbackPath {
		return fmt.Errorf("invalid google_redirect_url: path must be exactly %s — the only callback route this server mounts", GoogleCallbackPath)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("invalid google_redirect_url: query, fragment, and userinfo are not allowed (Google requirement)")
	}
	return nil
}

// isLoopbackHost reports whether host is the local machine by name or address —
// the only shape Google exempts from the HTTPS requirement.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
