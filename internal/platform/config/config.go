// Package config loads runtime configuration from the environment via viper,
// binding env vars (prefixed PORTCULLIS_) onto a typed struct.
package config

import (
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// Config is the typed runtime configuration. Fields map to env vars as
// PORTCULLIS_<FIELD>, with "." in nested keys replaced by "_".
type Config struct {
	// Addr is the HTTP listen address. A bare port like "8080" is accepted and
	// normalized to ":8080" (all interfaces).
	Addr string `mapstructure:"addr"`
	// DatabaseURL is the metadata PostgreSQL DSN.
	DatabaseURL string `mapstructure:"database_url"`
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
}

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

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}

	// Accept a bare port ("8080") as well as "host:port" (":8080").
	if cfg.Addr != "" && !strings.Contains(cfg.Addr, ":") {
		cfg.Addr = ":" + cfg.Addr
	}
	return cfg, nil
}
