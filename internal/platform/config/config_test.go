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

	if cfg.LoginBackoffThreshold != 5 || cfg.LoginBackoffBase != time.Minute || cfg.LoginBackoffCap != 15*time.Minute {
		t.Errorf("login backoff = (%d, %s, %s), want (5, 1m, 15m)", cfg.LoginBackoffThreshold, cfg.LoginBackoffBase, cfg.LoginBackoffCap)
	}

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

	if cfg, err := config.Load(); err != nil || cfg.RuntimeRole != "portcullis_runtime" {
		t.Errorf("default RuntimeRole = %q, %v; want portcullis_runtime", cfg.RuntimeRole, err)
	}

	t.Setenv("PORTCULLIS_RUNTIME_ROLE", "pc_install_a")
	if cfg, err := config.Load(); err != nil || cfg.RuntimeRole != "pc_install_a" {
		t.Errorf("custom RuntimeRole = %q, %v", cfg.RuntimeRole, err)
	}

	// Anything that is not a plain lowercase identifier must fail fast: the name is spliced into migration SQL.
	for _, bad := range []string{"role; drop table users--", "Role", "1role", "a b"} {
		t.Setenv("PORTCULLIS_RUNTIME_ROLE", bad)
		if _, err := config.Load(); err == nil {
			t.Errorf("RuntimeRole %q should be rejected", bad)
		}
	}
}

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
		{"execution lock timeout zero", "PORTCULLIS_EXECUTION_LOCK_TIMEOUT", "0s"},
		{"execution lock timeout negative", "PORTCULLIS_EXECUTION_LOCK_TIMEOUT", "-1s"},
		{"execution lock timeout below floor", "PORTCULLIS_EXECUTION_LOCK_TIMEOUT", "500ms"},
		{"execution lock timeout absurd", "PORTCULLIS_EXECUTION_LOCK_TIMEOUT", "2m"},
		{"execution lock timeout unitless", "PORTCULLIS_EXECUTION_LOCK_TIMEOUT", "5"},
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

func TestLogConfigIsCaseInsensitive(t *testing.T) {
	t.Setenv("PORTCULLIS_LOG_LEVEL", "ERROR")
	t.Setenv("PORTCULLIS_LOG_FORMAT", "TEXT")
	if _, err := config.Load(); err != nil {
		t.Errorf("uppercase log level/format should be accepted: %v", err)
	}
}

func TestStartupMigrationSemantics(t *testing.T) {
	t.Setenv("PORTCULLIS_DATABASE_URL", "postgres://app@localhost/db")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.StartupMigrationEnabled() {
		t.Error("nothing set: the server must not migrate at startup")
	}
	if got := cfg.OwnerDSN(); got != cfg.DatabaseURL {
		t.Errorf("OwnerDSN = %q, want fallback to DatabaseURL %q (migrate command, single-role)", got, cfg.DatabaseURL)
	}

	// The insecure-dev flag alone must NOT flip migration on (decoupled).
	t.Setenv("PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME", "true")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.StartupMigrationEnabled() {
		t.Error("ALLOW_PRIVILEGED_RUNTIME must not imply startup migration")
	}

	t.Setenv("PORTCULLIS_STARTUP_MIGRATE", "true")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.StartupMigrationEnabled() {
		t.Error("STARTUP_MIGRATE=true must enable startup migration")
	}

	t.Setenv("PORTCULLIS_STARTUP_MIGRATE", "")
	t.Setenv("PORTCULLIS_MIGRATE_DATABASE_URL", "postgres://owner@localhost/db")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.StartupMigrationEnabled() {
		t.Error("a server-held owner DSN should keep startup migration (compatibility)")
	}
	if got := cfg.OwnerDSN(); got != "postgres://owner@localhost/db" {
		t.Errorf("OwnerDSN = %q, want the explicit owner DSN", got)
	}

	// …but the explicit tri-state can turn it off even then.
	t.Setenv("PORTCULLIS_STARTUP_MIGRATE", "false")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.StartupMigrationEnabled() {
		t.Error("STARTUP_MIGRATE=false must win over a configured owner DSN")
	}

	t.Setenv("PORTCULLIS_STARTUP_MIGRATE", "ture")
	if _, err := config.Load(); err == nil {
		t.Error("invalid STARTUP_MIGRATE value must fail Load")
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

	if cfg, err := config.Load(); err != nil || cfg.GoogleEnabled() {
		t.Errorf("default GoogleEnabled = %t, %v; want false, nil", cfg.GoogleEnabled(), err)
	}

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
	// Any subset without the rest is a misconfig that must fail startup — a half-configured Google login would otherwise surface only on first use.
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

	// Google's registration rules (web-verified, ADR-0007): HTTPS required with localhost/loopback as the only HTTP exception; no fragment, userinfo, or raw public IP. The path must be the one route the server actually mounts.
	for _, bad := range []string{
		"not a url",
		"/auth/google/callback",
		"ftp://x.example/cb",
		"http://production.example/auth/google/callback",
		"https://x.example/",
		"https://x.example/callback",
		"https://x.example/auth/google/callback?next=/x",
		"https://x.example/auth/google/callback#frag",
		"https://user:pw@x.example/auth/google/callback",
		"https://203.0.113.7:8443/auth/google/callback",
		"http://192.168.1.10:8080/auth/google/callback",
		"https://[2001:db8::1]:8443/auth/google/callback",
	} {
		t.Setenv("PORTCULLIS_GOOGLE_REDIRECT_URL", bad)
		if _, err := config.Load(); err == nil {
			t.Errorf("redirect URL %q must fail Load", bad)
		}
	}
	for _, good := range []string{
		"https://portcullis.example.com/auth/google/callback",
		"http://localhost:8080/auth/google/callback",
		"http://127.0.0.1:8080/auth/google/callback",
		"http://[::1]:8080/auth/google/callback",
	} {
		t.Setenv("PORTCULLIS_GOOGLE_REDIRECT_URL", good)
		if _, err := config.Load(); err != nil {
			t.Errorf("redirect URL %q should load: %v", good, err)
		}
	}
}

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

func TestBootstrapAdminConfig(t *testing.T) {

	if cfg, err := config.Load(); err != nil || cfg.BootstrapAdminEnabled() {
		t.Errorf("default BootstrapAdminEnabled = %t, %v; want false, nil", cfg.BootstrapAdminEnabled(), err)
	}

	t.Setenv("PORTCULLIS_BOOTSTRAP_ADMIN_EMAIL", "root@example.com")
	t.Setenv("PORTCULLIS_BOOTSTRAP_ADMIN_PASSWORD", "correct-horse-battery")
	cfg, err := config.Load()
	if err != nil || !cfg.BootstrapAdminEnabled() {
		t.Fatalf("configured BootstrapAdminEnabled = %t, %v; want true, nil", cfg.BootstrapAdminEnabled(), err)
	}
	if pw, err := cfg.ResolveBootstrapAdminPassword(); err != nil || pw != "correct-horse-battery" {
		t.Errorf("ResolveBootstrapAdminPassword = %q, %v", pw, err)
	}
	if cfg.BootstrapAdminDisplayName != "Admin" {
		t.Errorf("BootstrapAdminDisplayName = %q, want the Admin default", cfg.BootstrapAdminDisplayName)
	}

	t.Setenv("PORTCULLIS_BOOTSTRAP_ADMIN_DISPLAY_NAME", "Root Operator")
	if cfg, err := config.Load(); err != nil || cfg.BootstrapAdminDisplayName != "Root Operator" {
		t.Errorf("BootstrapAdminDisplayName = %q, %v; want Root Operator", cfg.BootstrapAdminDisplayName, err)
	}
}

func TestBootstrapAdminPasswordFile(t *testing.T) {
	file := t.TempDir() + "/admin-password"
	if err := os.WriteFile(file, []byte("file-horse-battery\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORTCULLIS_BOOTSTRAP_ADMIN_EMAIL", "root@example.com")
	t.Setenv("PORTCULLIS_BOOTSTRAP_ADMIN_PASSWORD_FILE", file)

	cfg, err := config.Load()
	if err != nil || !cfg.BootstrapAdminEnabled() {
		t.Fatalf("file-password BootstrapAdminEnabled = %t, %v; want true, nil", cfg.BootstrapAdminEnabled(), err)
	}

	if pw, err := cfg.ResolveBootstrapAdminPassword(); err != nil || pw != "file-horse-battery" {
		t.Errorf("ResolveBootstrapAdminPassword = %q, %v", pw, err)
	}

	// Both password sources set: ambiguous, must fail (mirrors the master key).
	t.Setenv("PORTCULLIS_BOOTSTRAP_ADMIN_PASSWORD", "inline-too")
	if _, err := config.Load(); err == nil {
		t.Error("both bootstrap_admin_password and _password_file must fail Load")
	}
}

func TestBootstrapAdminPasswordIsNotRewritten(t *testing.T) {
	t.Setenv("PORTCULLIS_BOOTSTRAP_ADMIN_EMAIL", "root@example.com")

	inline := []struct {
		name  string
		value string
	}{
		{"leading space", " correct-horse-battery"},
		{"trailing space", "correct-horse-battery "},
		{"both ends", "  correct-horse-battery  "},
		{"internal spaces are ordinary characters", "correct horse battery staple"},
		{"trailing tab", "correct-horse-battery\t"},
	}
	for _, tc := range inline {
		t.Run("inline "+tc.name, func(t *testing.T) {
			t.Setenv("PORTCULLIS_BOOTSTRAP_ADMIN_PASSWORD", tc.value)
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			got, err := cfg.ResolveBootstrapAdminPassword()
			if err != nil || got != tc.value {
				t.Errorf("ResolveBootstrapAdminPassword = %q, %v; want the value verbatim %q", got, err, tc.value)
			}
		})
	}

	files := []struct {
		name     string
		contents string
		want     string
	}{
		{"one trailing newline is the editor's, not the operator's", "file-horse-battery\n", "file-horse-battery"},
		{"CRLF counts as one newline", "file-horse-battery\r\n", "file-horse-battery"},
		{"no trailing newline", "file-horse-battery", "file-horse-battery"},

		{"a second newline is content", "file-horse-battery\n\n", "file-horse-battery\n"},
		{"leading and internal spaces survive", "  file horse battery\n", "  file horse battery"},
		{"a trailing space before the newline survives", "file-horse-battery \n", "file-horse-battery "},
	}
	for _, tc := range files {
		t.Run("file "+tc.name, func(t *testing.T) {
			path := t.TempDir() + "/admin-password"
			if err := os.WriteFile(path, []byte(tc.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PORTCULLIS_BOOTSTRAP_ADMIN_PASSWORD", "")
			t.Setenv("PORTCULLIS_BOOTSTRAP_ADMIN_PASSWORD_FILE", path)
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			got, err := cfg.ResolveBootstrapAdminPassword()
			if err != nil || got != tc.want {
				t.Errorf("ResolveBootstrapAdminPassword = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestBootstrapAdminConfigRejectsPartialSetup(t *testing.T) {
	// A half-configured bootstrap admin must fail startup, not silently skip — the operator believes an admin will exist (fail-fast, master-key posture).
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"email only", map[string]string{"PORTCULLIS_BOOTSTRAP_ADMIN_EMAIL": "root@example.com"}},
		{"password only", map[string]string{"PORTCULLIS_BOOTSTRAP_ADMIN_PASSWORD": "correct-horse-battery"}},
		{"display name only", map[string]string{"PORTCULLIS_BOOTSTRAP_ADMIN_DISPLAY_NAME": "Root"}},
		{"blank password", map[string]string{
			"PORTCULLIS_BOOTSTRAP_ADMIN_EMAIL":    "root@example.com",
			"PORTCULLIS_BOOTSTRAP_ADMIN_PASSWORD": " \t ",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if _, err := config.Load(); err == nil {
				t.Error("partial bootstrap admin config must fail Load")
			}
		})
	}
}

func TestBootstrapAdminBlankPasswordFile(t *testing.T) {
	file := t.TempDir() + "/admin-password"
	if err := os.WriteFile(file, []byte("\n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORTCULLIS_BOOTSTRAP_ADMIN_EMAIL", "root@example.com")
	t.Setenv("PORTCULLIS_BOOTSTRAP_ADMIN_PASSWORD_FILE", file)
	if _, err := config.Load(); err == nil {
		t.Fatal("Load accepted a blank bootstrap admin password file")
	}
}

func TestApprovalValidity(t *testing.T) {

	cfg, err := config.Load()
	if err != nil || cfg.ApprovalValidity != 24*time.Hour {
		t.Fatalf("ApprovalValidity = %v, %v; want 24h", cfg.ApprovalValidity, err)
	}
	t.Setenv("PORTCULLIS_APPROVAL_VALIDITY", "15m")
	if cfg, err := config.Load(); err != nil || cfg.ApprovalValidity != 15*time.Minute {
		t.Errorf("ApprovalValidity = %v, %v; want 15m", cfg.ApprovalValidity, err)
	}

	t.Setenv("PORTCULLIS_APPROVAL_VALIDITY", "14m")
	if _, err := config.Load(); err == nil {
		t.Error("sub-15m approval_validity must fail Load")
	}
	t.Setenv("PORTCULLIS_APPROVAL_VALIDITY", "169h")
	if _, err := config.Load(); err == nil {
		t.Error("over-7d approval_validity must fail Load")
	}
}

func TestExecutionLockTimeoutAcceptsDefaultAndInRangeValues(t *testing.T) {
	cfg, err := config.Load()
	if err != nil || cfg.ExecutionLockTimeout != 5*time.Second {
		t.Fatalf("default ExecutionLockTimeout = %v, %v; want 5s", cfg.ExecutionLockTimeout, err)
	}
	for _, accepted := range []struct {
		envValue string
		want     time.Duration
	}{
		{"1s", time.Second},
		{"10s", 10 * time.Second},
		{"1500ms", 1500 * time.Millisecond},
		{"1m", time.Minute},
	} {
		t.Setenv("PORTCULLIS_EXECUTION_LOCK_TIMEOUT", accepted.envValue)
		if cfg, err := config.Load(); err != nil || cfg.ExecutionLockTimeout != accepted.want {
			t.Errorf("PORTCULLIS_EXECUTION_LOCK_TIMEOUT=%q gave %v, %v; want %v", accepted.envValue, cfg.ExecutionLockTimeout, err, accepted.want)
		}
	}
}

func TestShutdownInterruptionFitsInsideTheShutdownTimeout(t *testing.T) {
	for _, accepted := range []struct {
		name          string
		shutdown      string
		interrupt     string
		wantInterrupt time.Duration
		wantHTTPDrain time.Duration
	}{
		{name: "defaults", wantInterrupt: 5 * time.Second, wantHTTPDrain: 10 * time.Second},
		{name: "longer shutdown with the default interruption", shutdown: "25s", wantInterrupt: 5 * time.Second, wantHTTPDrain: 20 * time.Second},
		{name: "explicit interruption", shutdown: "20s", interrupt: "8s", wantInterrupt: 8 * time.Second, wantHTTPDrain: 12 * time.Second},
		{name: "one second of HTTP drain left", shutdown: "6s", interrupt: "5s", wantInterrupt: 5 * time.Second, wantHTTPDrain: time.Second},
	} {
		t.Run(accepted.name, func(t *testing.T) {
			if accepted.shutdown != "" {
				t.Setenv("PORTCULLIS_SHUTDOWN_TIMEOUT", accepted.shutdown)
			}
			if accepted.interrupt != "" {
				t.Setenv("PORTCULLIS_SHUTDOWN_INTERRUPT_TIMEOUT", accepted.interrupt)
			}
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ShutdownInterruptTimeout != accepted.wantInterrupt || cfg.HTTPDrainTimeout() != accepted.wantHTTPDrain {
				t.Fatalf("interrupt=%s httpDrain=%s, want %s and %s", cfg.ShutdownInterruptTimeout, cfg.HTTPDrainTimeout(), accepted.wantInterrupt, accepted.wantHTTPDrain)
			}
			// The whole shutdown, drain delay aside, never exceeds shutdown_timeout.
			if cfg.HTTPDrainTimeout()+cfg.ShutdownInterruptTimeout != cfg.ShutdownTimeout {
				t.Fatalf("budgets %s + %s exceed shutdown_timeout %s", cfg.HTTPDrainTimeout(), cfg.ShutdownInterruptTimeout, cfg.ShutdownTimeout)
			}
		})
	}
	for _, refused := range []struct {
		name      string
		shutdown  string
		interrupt string
	}{
		{name: "interruption equal to the shutdown timeout", shutdown: "10s", interrupt: "10s"},
		{name: "interruption longer than the shutdown timeout", shutdown: "10s", interrupt: "20s"},
		{name: "zero interruption", interrupt: "0s"},
		{name: "negative interruption", interrupt: "-1s"},
		{name: "default interruption above a short shutdown timeout", shutdown: "5s"},
	} {
		t.Run("refuses "+refused.name, func(t *testing.T) {
			if refused.shutdown != "" {
				t.Setenv("PORTCULLIS_SHUTDOWN_TIMEOUT", refused.shutdown)
			}
			if refused.interrupt != "" {
				t.Setenv("PORTCULLIS_SHUTDOWN_INTERRUPT_TIMEOUT", refused.interrupt)
			}
			if _, err := config.Load(); err == nil {
				t.Fatalf("shutdown=%q interrupt=%q loaded, want refusal", refused.shutdown, refused.interrupt)
			}
		})
	}
}
