// Package config loads runtime configuration from the environment via viper, binding env vars (prefixed PORTCULLIS_) onto a typed struct.
package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/setting"
	"github.com/aportcullis/portcullis/internal/platform/logging"
)

// Load reads configuration from the environment and applies defaults.
func Load() (Config, error) {
	v := NewEnvironmentLoader("PORTCULLIS")

	v.SetDefault("addr", ":8080")
	v.SetDefault("drain_delay", time.Duration(0))
	v.SetDefault("shutdown_timeout", 15*time.Second)
	v.SetDefault("shutdown_interrupt_timeout", 5*time.Second)
	v.SetDefault("log_format", "json")
	v.SetDefault("argon2_max_concurrent", 2)
	v.SetDefault("runtime_role", "portcullis_runtime")
	v.SetDefault("database_max_conns", defaultDatabaseMaxConns)
	v.SetDefault("database_acquire_timeout", defaultDatabaseAcquireTimeout)
	v.SetDefault("database_statement_timeout", defaultDatabaseStatementTimeout)
	v.SetDefault("database_lock_timeout", defaultDatabaseLockTimeout)
	v.SetDefault("database_idle_in_transaction_timeout", defaultDatabaseIdleInTransactionTimeout)
	// Tier-C tunable defaults come from the settings registry (ADR-0017) — one source for the env seed here and the DB store's fallback.
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
	normalizeBootstrapAdminConfig(&cfg)

	// Accept a bare port ("8080") as well as "host:port" (":8080"). An explicitly empty address would bind a random port, so reject it.
	if cfg.Addr == "" {
		return Config{}, fmt.Errorf("addr must not be empty")
	}
	if !strings.Contains(cfg.Addr, ":") {
		cfg.Addr = ":" + cfg.Addr
	}

	// Tri-state enum: reject anything but true/false/unset instead of silently treating a typo ("ture") as unset (same fail-fast stance as log_level).
	cfg.StartupMigrate = strings.ToLower(strings.TrimSpace(cfg.StartupMigrate))
	if cfg.StartupMigrate != "" && cfg.StartupMigrate != "true" && cfg.StartupMigrate != "false" {
		return Config{}, fmt.Errorf("invalid startup_migrate %q: want true, false, or unset", cfg.StartupMigrate)
	}

	// The runtime role name is spliced into migration SQL as an identifier — fail fast on anything that isn't a plain lowercase identifier.
	if !runtimeRolePattern.MatchString(cfg.RuntimeRole) {
		return Config{}, fmt.Errorf("invalid runtime_role %q: must match %s", cfg.RuntimeRole, runtimeRolePattern)
	}

	// Validate and normalize logging values against the logging package’s vocabulary.
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	if !logging.ValidFormat(cfg.LogFormat) {
		return Config{}, fmt.Errorf("invalid log_format %q: want json or text", cfg.LogFormat)
	}

	// Bounded numeric/duration config: a nonsensical value must fail startup, not degrade silently (a huge Argon2 cap = OOM; a negative timeout = instant expiry).
	if cfg.Argon2MaxConcurrent < 1 || cfg.Argon2MaxConcurrent > maxArgon2Concurrent {
		return Config{}, fmt.Errorf("argon2_max_concurrent %d out of range [1, %d]", cfg.Argon2MaxConcurrent, maxArgon2Concurrent)
	}
	// Tier-C tunables validate through their registry descriptors (ADR-0017): the SAME bounds gate the env seed here and every settings-store write/read, so the two paths cannot drift.
	for _, tc := range []struct {
		key  setting.Key
		text string
	}{
		{setting.KeyLogLevel, cfg.LogLevel},
		{setting.KeyLoginBackoffThreshold, strconv.Itoa(cfg.LoginBackoffThreshold)},
		{setting.KeyLoginBackoffBase, cfg.LoginBackoffBase.String()},
		{setting.KeyLoginBackoffCap, cfg.LoginBackoffCap.String()},
		{setting.KeyConnectionTestTimeout, cfg.ConnectionTestTimeout.String()},
		{setting.KeyApprovalValidity, cfg.ApprovalValidity.String()},
		{setting.KeyExecutionLockTimeout, cfg.ExecutionLockTimeout.String()},
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
	if err := validateDatabasePool(cfg); err != nil {
		return Config{}, err
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
	// Execution interruption is carved out of shutdown_timeout, so drain_delay + shutdown_timeout stays the whole shutdown budget (ADR-0010).
	if cfg.ShutdownInterruptTimeout <= 0 || cfg.ShutdownInterruptTimeout >= cfg.ShutdownTimeout {
		return Config{}, fmt.Errorf("shutdown_interrupt_timeout %s must be positive and shorter than shutdown_timeout %s", cfg.ShutdownInterruptTimeout, cfg.ShutdownTimeout)
	}

	// Google login is all-or-nothing (ADR-0007): everything unset ⇒ disabled (valid); a partial setup fails startup rather than surfacing on first use.
	if err := validateGoogleConfig(cfg); err != nil {
		return Config{}, err
	}

	// The config-based bootstrap admin is all-or-nothing too (ADR-0006): a half-configured admin must fail startup, not silently skip — the operator believes an admin will exist.
	if err := validateBootstrapAdminConfig(cfg); err != nil {
		return Config{}, err
	}

	// Parse trusted-proxy CIDRs once at startup so a typo fails fast rather than silently disabling proxy-aware rate limiting.
	for _, c := range cfg.TrustedProxies {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		_, network, err := net.ParseCIDR(c)
		if err != nil {
			return Config{}, fmt.Errorf("invalid trusted_proxies entry %q: %w", c, err)
		}
		// A /0 ("trust every proxy") makes X-Forwarded-For fully spoofable, which defeats per-IP rate limiting entirely — almost certainly a misconfig.
		if ones, _ := network.Mask.Size(); ones == 0 {
			return Config{}, fmt.Errorf("trusted_proxies entry %q trusts the entire address space; X-Forwarded-For would be fully spoofable", c)
		}
		cfg.trustedProxyNets = append(cfg.trustedProxyNets, network)
	}
	return cfg, nil
}

// validateDatabasePool enforces the metadata pool bounds and keeps the lock wait within the statement bound.
func validateDatabasePool(cfg Config) error {
	if cfg.DatabaseMaxConns < minDatabaseMaxConns || cfg.DatabaseMaxConns > maxDatabaseMaxConns {
		return fmt.Errorf("database_max_conns %d out of range [%d, %d]", cfg.DatabaseMaxConns, minDatabaseMaxConns, maxDatabaseMaxConns)
	}
	for _, bound := range []struct {
		key           string
		value, lo, hi time.Duration
	}{
		{"database_acquire_timeout", cfg.DatabaseAcquireTimeout, minDatabaseAcquireTimeout, maxDatabaseAcquireTimeout},
		{"database_statement_timeout", cfg.DatabaseStatementTimeout, minDatabaseStatementTimeout, maxDatabaseStatementTimeout},
		{"database_lock_timeout", cfg.DatabaseLockTimeout, minDatabaseLockTimeout, maxDatabaseLockTimeout},
		{"database_idle_in_transaction_timeout", cfg.DatabaseIdleInTransactionTimeout, minDatabaseIdleInTransactionTimeout, maxDatabaseIdleInTransactionTimeout},
	} {
		if bound.value < bound.lo || bound.value > bound.hi {
			return fmt.Errorf("%s %s out of range [%s, %s]", bound.key, bound.value, bound.lo, bound.hi)
		}
	}
	if cfg.DatabaseLockTimeout > cfg.DatabaseStatementTimeout {
		return fmt.Errorf("database_lock_timeout %s exceeds database_statement_timeout %s", cfg.DatabaseLockTimeout, cfg.DatabaseStatementTimeout)
	}
	return nil
}

// normalizeGoogleConfig gives all Google-login consumers one canonical view of environment values. In particular, whitespace-only client IDs must not pass validation as disabled and later enable routes with an invalid raw value.
func normalizeGoogleConfig(cfg *Config) {
	cfg.GoogleClientID = strings.TrimSpace(cfg.GoogleClientID)
	cfg.GoogleClientSecret = strings.TrimSpace(cfg.GoogleClientSecret)
	cfg.GoogleClientSecretFile = strings.TrimSpace(cfg.GoogleClientSecretFile)
	cfg.GoogleRedirectURL = strings.TrimSpace(cfg.GoogleRedirectURL)
}

// normalizeBootstrapAdminConfig canonicalizes the bootstrap-admin values so whitespace-only input cannot pass as "unset", and applies the display-name default once the feature is on (the auth service requires a non-empty name).
func normalizeBootstrapAdminConfig(cfg *Config) {
	cfg.BootstrapAdminEmail = strings.TrimSpace(cfg.BootstrapAdminEmail)
	cfg.BootstrapAdminPasswordFile = strings.TrimSpace(cfg.BootstrapAdminPasswordFile)
	cfg.BootstrapAdminDisplayName = strings.TrimSpace(cfg.BootstrapAdminDisplayName)
	if cfg.BootstrapAdminEmail != "" && cfg.BootstrapAdminDisplayName == "" {
		cfg.BootstrapAdminDisplayName = "Admin"
	}
}

// validateBootstrapAdminConfig requires an email and exactly one nonempty password source, or no bootstrap configuration. The auth service owns password policy.
func validateBootstrapAdminConfig(cfg Config) error {
	hasPassword := strings.TrimSpace(cfg.BootstrapAdminPassword) != "" || cfg.BootstrapAdminPasswordFile != ""
	if cfg.BootstrapAdminEmail == "" {
		if hasPassword || cfg.BootstrapAdminDisplayName != "" {
			return fmt.Errorf("bootstrap admin is partially configured: bootstrap_admin_email and a password (bootstrap_admin_password or bootstrap_admin_password_file) must both be set to enable it, or all bootstrap_admin_* unset to disable it")
		}
		return nil // feature disabled
	}
	if strings.TrimSpace(cfg.BootstrapAdminPassword) != "" && cfg.BootstrapAdminPasswordFile != "" {
		return fmt.Errorf("set either bootstrap_admin_password or bootstrap_admin_password_file, not both")
	}
	if !hasPassword {
		return fmt.Errorf("bootstrap admin is partially configured: bootstrap_admin_email is set but no password source is")
	}
	password, err := cfg.ResolveBootstrapAdminPassword()
	if err != nil {
		return err
	}
	// Blank means blank: a mounted file holding only whitespace is an operator mistake, not a secret. The check trims only to DECIDE — the value itself is never rewritten, because login verifies exactly what the operator types (ResolveBootstrapAdminPassword, NIST SP 800-63B).
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("bootstrap admin is configured with an empty password")
	}
	return nil
}

// validateGoogleConfig enforces the all-or-nothing Google login setup: the client id, exactly one secret source, and a well-formed absolute redirect URL must all be present, or all absent (ADR-0007).
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
	// A mounted secret can exist but be empty (for example, from a wrongly populated Kubernetes Secret). Read it while loading configuration so the server fails before advertising a Google login route that can never finish.
	secret, err := cfg.ResolveGoogleClientSecret()
	if err != nil {
		return err
	}
	if secret == "" {
		return fmt.Errorf("google login is configured with an empty client secret")
	}
	// Validate Google’s registered redirect URL at boot, allowing HTTP only for loopback and requiring the mounted callback path. Error messages omit the URL to avoid leaking userinfo.
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

// isLoopbackHost reports whether host is the local machine by name or address — the only shape Google exempts from the HTTPS requirement.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
