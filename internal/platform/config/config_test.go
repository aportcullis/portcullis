package config_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/platform/config"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.DrainDelay != 0 {
		t.Errorf("DrainDelay = %v, want 0", cfg.DrainDelay)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 15s", cfg.ShutdownTimeout)
	}
	if cfg.LogLevel != "info" || cfg.LogFormat != "json" {
		t.Errorf("LogLevel/Format = %q/%q, want info/json", cfg.LogLevel, cfg.LogFormat)
	}
	// Progressive-backoff defaults are the ADR-0006 pinned parameters.
	if cfg.LoginBackoffThreshold != 5 || cfg.LoginBackoffBase != time.Minute || cfg.LoginBackoffCap != 15*time.Minute {
		t.Errorf("login backoff = (%d, %s, %s), want (5, 1m, 15m)", cfg.LoginBackoffThreshold, cfg.LoginBackoffBase, cfg.LoginBackoffCap)
	}
	// Connection-test timeout default is the ADR-0014 pinned value.
	if cfg.ConnectionTestTimeout != 10*time.Second {
		t.Errorf("ConnectionTestTimeout = %v, want 10s", cfg.ConnectionTestTimeout)
	}
}

func TestAllowPrivilegedRuntimeFlag(t *testing.T) {
	// Insecure dev mode must be an explicit opt-in, never the default.
	if cfg, err := config.Load(); err != nil || cfg.AllowPrivilegedRuntime {
		t.Errorf("AllowPrivilegedRuntime default = %t, %v; want false", cfg.AllowPrivilegedRuntime, err)
	}
	t.Setenv("PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME", "true")
	if cfg, err := config.Load(); err != nil || !cfg.AllowPrivilegedRuntime {
		t.Errorf("AllowPrivilegedRuntime = %t, %v; want true", cfg.AllowPrivilegedRuntime, err)
	}
}

func TestRuntimeRoleValidation(t *testing.T) {
	// Default is the standard role name.
	if cfg, err := config.Load(); err != nil || cfg.RuntimeRole != "portcullis_runtime" {
		t.Errorf("default RuntimeRole = %q, %v; want portcullis_runtime", cfg.RuntimeRole, err)
	}

	// A custom identifier is accepted.
	t.Setenv("PORTCULLIS_RUNTIME_ROLE", "pc_install_a")
	if cfg, err := config.Load(); err != nil || cfg.RuntimeRole != "pc_install_a" {
		t.Errorf("custom RuntimeRole = %q, %v", cfg.RuntimeRole, err)
	}

	// Anything that is not a plain lowercase identifier must fail fast: the name
	// is spliced into migration SQL.
	for _, bad := range []string{"role; drop table users--", "Role", "1role", "a b"} {
		t.Setenv("PORTCULLIS_RUNTIME_ROLE", bad)
		if _, err := config.Load(); err == nil {
			t.Errorf("RuntimeRole %q should be rejected", bad)
		}
	}
}

// Each env var's invalid/edge values must fail startup rather than degrade
// silently — a misconfiguration should never quietly weaken the running server.
func TestRejectsInvalidEnvValues(t *testing.T) {
	cases := []struct {
		name, env, val string
	}{
		{"unknown log level", "PORTCULLIS_LOG_LEVEL", "erro"},
		{"unknown log format", "PORTCULLIS_LOG_FORMAT", "yaml"},
		{"argon2 zero", "PORTCULLIS_ARGON2_MAX_CONCURRENT", "0"},
		{"argon2 negative", "PORTCULLIS_ARGON2_MAX_CONCURRENT", "-1"},
		{"argon2 absurd", "PORTCULLIS_ARGON2_MAX_CONCURRENT", "100000"},
		{"backoff threshold zero", "PORTCULLIS_LOGIN_BACKOFF_THRESHOLD", "0"},
		{"backoff threshold negative", "PORTCULLIS_LOGIN_BACKOFF_THRESHOLD", "-3"},
		{"backoff base zero", "PORTCULLIS_LOGIN_BACKOFF_BASE", "0s"},
		{"backoff base negative", "PORTCULLIS_LOGIN_BACKOFF_BASE", "-1m"},
		{"backoff cap below base", "PORTCULLIS_LOGIN_BACKOFF_CAP", "30s"},
		{"backoff cap absurd", "PORTCULLIS_LOGIN_BACKOFF_CAP", "25h"},
		{"backoff threshold absurd", "PORTCULLIS_LOGIN_BACKOFF_THRESHOLD", "1001"},
		{"connection test timeout zero", "PORTCULLIS_CONNECTION_TEST_TIMEOUT", "0s"},
		{"connection test timeout below floor", "PORTCULLIS_CONNECTION_TEST_TIMEOUT", "500ms"},
		{"connection test timeout absurd", "PORTCULLIS_CONNECTION_TEST_TIMEOUT", "2m"},
		{"negative drain delay", "PORTCULLIS_DRAIN_DELAY", "-5s"},
		{"zero shutdown timeout", "PORTCULLIS_SHUTDOWN_TIMEOUT", "0s"},
		{"negative shutdown timeout", "PORTCULLIS_SHUTDOWN_TIMEOUT", "-1s"},
		{"trust-all proxies v4", "PORTCULLIS_TRUSTED_PROXIES", "0.0.0.0/0"},
		{"trust-all proxies v6", "PORTCULLIS_TRUSTED_PROXIES", "::/0"},
		{"malformed proxy cidr", "PORTCULLIS_TRUSTED_PROXIES", "not-a-cidr"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.env, tc.val)
			if _, err := config.Load(); err == nil {
				t.Errorf("%s=%q should be rejected", tc.env, tc.val)
			}
		})
	}
}

// Valid non-default values across every environment-tunable field load cleanly.
func TestAcceptsValidEnvValues(t *testing.T) {
	t.Setenv("PORTCULLIS_LOG_LEVEL", "error")
	t.Setenv("PORTCULLIS_LOG_FORMAT", "text")
	t.Setenv("PORTCULLIS_ARGON2_MAX_CONCURRENT", "8")
	t.Setenv("PORTCULLIS_TRUSTED_PROXIES", "10.0.0.0/8, 192.168.0.0/16")
	t.Setenv("PORTCULLIS_MIGRATE_DATABASE_URL", "postgres://owner@localhost/db")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != "error" || cfg.LogFormat != "text" {
		t.Errorf("log level/format = %q/%q", cfg.LogLevel, cfg.LogFormat)
	}
	if cfg.Argon2MaxConcurrent != 8 {
		t.Errorf("Argon2MaxConcurrent = %d, want 8", cfg.Argon2MaxConcurrent)
	}
	if got := cfg.TrustedProxyNets(); len(got) != 2 {
		t.Errorf("TrustedProxyNets = %d, want 2", len(got))
	}
	if cfg.MigrateDatabaseURL == "" || cfg.MigrateDatabaseURL == cfg.DatabaseURL {
		t.Errorf("MigrateDatabaseURL should be the explicit owner DSN, got %q", cfg.MigrateDatabaseURL)
	}
}

// Logging validation delegates to the logging package, which parses
// case-insensitively — an operator's LOG_LEVEL=ERROR must load, not fail
// startup while the logger itself would honor it.
func TestLogConfigIsCaseInsensitive(t *testing.T) {
	t.Setenv("PORTCULLIS_LOG_LEVEL", "ERROR")
	t.Setenv("PORTCULLIS_LOG_FORMAT", "TEXT")
	if _, err := config.Load(); err != nil {
		t.Errorf("uppercase log level/format should be accepted: %v", err)
	}
}

// With no explicit owner DSN, migrations reuse the runtime DSN (single-role dev).
func TestMigrateDatabaseURLFallsBackToRuntime(t *testing.T) {
	t.Setenv("PORTCULLIS_DATABASE_URL", "postgres://app@localhost/db")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MigrateDatabaseURL != cfg.DatabaseURL {
		t.Errorf("MigrateDatabaseURL = %q, want fallback to DatabaseURL %q", cfg.MigrateDatabaseURL, cfg.DatabaseURL)
	}
}

func TestBarePortIsNormalized(t *testing.T) {
	t.Setenv("PORTCULLIS_ADDR", "8080")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080 (bare port should be normalized)", cfg.Addr)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("PORTCULLIS_ADDR", ":9090")
	t.Setenv("PORTCULLIS_DRAIN_DELAY", "3s")
	t.Setenv("PORTCULLIS_SHUTDOWN_TIMEOUT", "20s")
	t.Setenv("PORTCULLIS_LOG_LEVEL", "debug")
	t.Setenv("PORTCULLIS_MASTER_KEY", "dGVzdC1rZXk=")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MasterKey != "dGVzdC1rZXk=" {
		t.Errorf("MasterKey = %q, want it from env", cfg.MasterKey)
	}
	if cfg.Addr != ":9090" {
		t.Errorf("Addr = %q, want :9090", cfg.Addr)
	}
	if cfg.DrainDelay != 3*time.Second {
		t.Errorf("DrainDelay = %v, want 3s", cfg.DrainDelay)
	}
	if cfg.ShutdownTimeout != 20*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 20s", cfg.ShutdownTimeout)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
}

func TestGoogleLoginConfig(t *testing.T) {
	// All unset (default): the feature is simply disabled — a valid state.
	if cfg, err := config.Load(); err != nil || cfg.GoogleEnabled() {
		t.Errorf("default GoogleEnabled = %t, %v; want false, nil", cfg.GoogleEnabled(), err)
	}

	// Fully configured: enabled, and the secret resolves.
	t.Setenv("PORTCULLIS_GOOGLE_CLIENT_ID", "client-1")
	t.Setenv("PORTCULLIS_GOOGLE_CLIENT_SECRET", "s3cret")
	t.Setenv("PORTCULLIS_GOOGLE_REDIRECT_URL", "https://portcullis.example/auth/google/callback")
	cfg, err := config.Load()
	if err != nil || !cfg.GoogleEnabled() {
		t.Fatalf("configured GoogleEnabled = %t, %v; want true, nil", cfg.GoogleEnabled(), err)
	}
	if secret, err := cfg.ResolveGoogleClientSecret(); err != nil || secret != "s3cret" {
		t.Errorf("ResolveGoogleClientSecret = %q, %v", secret, err)
	}
}

func TestGoogleLoginConfigRejectsPartialSetup(t *testing.T) {
	// Any subset without the rest is a misconfig that must fail startup — a
	// half-configured Google login would otherwise surface only on first use.
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"id only", map[string]string{"PORTCULLIS_GOOGLE_CLIENT_ID": "client-1"}},
		{"secret only", map[string]string{"PORTCULLIS_GOOGLE_CLIENT_SECRET": "s3cret"}},
		{"redirect only", map[string]string{"PORTCULLIS_GOOGLE_REDIRECT_URL": "https://x.example/cb"}},
		{"missing redirect", map[string]string{
			"PORTCULLIS_GOOGLE_CLIENT_ID":     "client-1",
			"PORTCULLIS_GOOGLE_CLIENT_SECRET": "s3cret",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if _, err := config.Load(); err == nil {
				t.Error("partial Google config must fail Load")
			}
		})
	}
}

func TestGoogleClientSecretFile(t *testing.T) {
	dir := t.TempDir()
	file := dir + "/google-secret"
	if err := os.WriteFile(file, []byte("file-s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORTCULLIS_GOOGLE_CLIENT_ID", "client-1")
	t.Setenv("PORTCULLIS_GOOGLE_CLIENT_SECRET_FILE", file)
	t.Setenv("PORTCULLIS_GOOGLE_REDIRECT_URL", "https://portcullis.example/auth/google/callback")

	cfg, err := config.Load()
	if err != nil || !cfg.GoogleEnabled() {
		t.Fatalf("file-secret GoogleEnabled = %t, %v; want true, nil", cfg.GoogleEnabled(), err)
	}
	// Trailing whitespace from the mounted file is trimmed (as the key file is).
	if secret, err := cfg.ResolveGoogleClientSecret(); err != nil || secret != "file-s3cret" {
		t.Errorf("ResolveGoogleClientSecret = %q, %v", secret, err)
	}

	// Both secret sources set: ambiguous, must fail (mirrors the master key).
	t.Setenv("PORTCULLIS_GOOGLE_CLIENT_SECRET", "inline-too")
	if _, err := config.Load(); err == nil {
		t.Error("both google_client_secret and google_client_secret_file must fail Load")
	}
}

func TestGoogleLoginConfigRejectsBlankSecret(t *testing.T) {
	t.Run("inline", func(t *testing.T) {
		t.Setenv("PORTCULLIS_GOOGLE_CLIENT_ID", "client-1")
		t.Setenv("PORTCULLIS_GOOGLE_CLIENT_SECRET", " \t ")
		t.Setenv("PORTCULLIS_GOOGLE_REDIRECT_URL", "https://portcullis.example/auth/google/callback")
		if _, err := config.Load(); err == nil {
			t.Fatal("Load accepted a blank inline Google client secret")
		}
	})

	t.Run("file", func(t *testing.T) {
		file := t.TempDir() + "/google-secret"
		if err := os.WriteFile(file, []byte("\n\t"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PORTCULLIS_GOOGLE_CLIENT_ID", "client-1")
		t.Setenv("PORTCULLIS_GOOGLE_CLIENT_SECRET_FILE", file)
		t.Setenv("PORTCULLIS_GOOGLE_REDIRECT_URL", "https://portcullis.example/auth/google/callback")
		if _, err := config.Load(); err == nil {
			t.Fatal("Load accepted a blank Google client secret file")
		}
	})
}

func TestGoogleLoginConfigNormalizesWhitespace(t *testing.T) {
	t.Run("blank client id disables the unset feature", func(t *testing.T) {
		t.Setenv("PORTCULLIS_GOOGLE_CLIENT_ID", " \t ")
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.GoogleEnabled() || cfg.GoogleClientID != "" {
			t.Errorf("blank client ID = enabled %t, value %q; want disabled and empty", cfg.GoogleEnabled(), cfg.GoogleClientID)
		}
	})

	t.Run("configured values are stored canonically", func(t *testing.T) {
		t.Setenv("PORTCULLIS_GOOGLE_CLIENT_ID", " client-1 ")
		t.Setenv("PORTCULLIS_GOOGLE_CLIENT_SECRET", " s3cret\t")
		t.Setenv("PORTCULLIS_GOOGLE_CLIENT_SECRET_FILE", " \t")
		t.Setenv("PORTCULLIS_GOOGLE_REDIRECT_URL", " https://portcullis.example/auth/google/callback ")
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.GoogleClientID != "client-1" || cfg.GoogleRedirectURL != "https://portcullis.example/auth/google/callback" {
			t.Errorf("Google config was not normalized: %+v", cfg)
		}
		if secret, err := cfg.ResolveGoogleClientSecret(); err != nil || secret != "s3cret" {
			t.Errorf("ResolveGoogleClientSecret = %q, %v; want s3cret, nil", secret, err)
		}
	})
}

func TestGoogleRedirectURLValidation(t *testing.T) {
	t.Setenv("PORTCULLIS_GOOGLE_CLIENT_ID", "client-1")
	t.Setenv("PORTCULLIS_GOOGLE_CLIENT_SECRET", "s3cret")

	// Google's registration rules (web-verified, ADR-0007): HTTPS required with
	// localhost/loopback as the only HTTP exception; no fragment, userinfo, or
	// raw public IP. The path must be the one route the server actually mounts.
	for _, bad := range []string{
		"not a url",
		"/auth/google/callback", // relative
		"ftp://x.example/cb",
		"http://production.example/auth/google/callback",  // http off-loopback
		"https://x.example/",                              // wrong path
		"https://x.example/callback",                      // wrong path
		"https://x.example/auth/google/callback?next=/x",  // query
		"https://x.example/auth/google/callback#frag",     // fragment
		"https://user:pw@x.example/auth/google/callback",  // userinfo
		"https://203.0.113.7:8443/auth/google/callback",   // raw public IP
		"http://192.168.1.10:8080/auth/google/callback",   // raw private IP, still not loopback
		"https://[2001:db8::1]:8443/auth/google/callback", // raw public IPv6
	} {
		t.Setenv("PORTCULLIS_GOOGLE_REDIRECT_URL", bad)
		if _, err := config.Load(); err == nil {
			t.Errorf("redirect URL %q must fail Load", bad)
		}
	}
	for _, good := range []string{
		"https://portcullis.example.com/auth/google/callback", // production shape
		"http://localhost:8080/auth/google/callback",          // loopback dev
		"http://127.0.0.1:8080/auth/google/callback",
		"http://[::1]:8080/auth/google/callback",
	} {
		t.Setenv("PORTCULLIS_GOOGLE_REDIRECT_URL", good)
		if _, err := config.Load(); err != nil {
			t.Errorf("redirect URL %q should load: %v", good, err)
		}
	}
}

// A misconfigured redirect URL can carry userinfo credentials, and the
// validation error goes straight to the startup log — it must name the key and
// the violated rule, never echo the URL. URL.Redacted() is not enough: it
// masks only the password, keeping the username (web-verified, external
// review).
func TestGoogleRedirectURLErrorsOmitTheURL(t *testing.T) {
	t.Setenv("PORTCULLIS_GOOGLE_CLIENT_ID", "client-1")
	t.Setenv("PORTCULLIS_GOOGLE_CLIENT_SECRET", "s3cret")
	t.Setenv("PORTCULLIS_GOOGLE_REDIRECT_URL", "https://leaked-user:leaked-pw@x.example/auth/google/callback")

	_, err := config.Load()
	if err == nil {
		t.Fatal("userinfo redirect URL must fail Load")
	}
	msg := err.Error()
	if strings.Contains(msg, "leaked-pw") || strings.Contains(msg, "leaked-user") {
		t.Errorf("error leaks the URL's userinfo: %q", msg)
	}
	if !strings.Contains(msg, "google_redirect_url") {
		t.Errorf("error should name the offending key: %q", msg)
	}
}
