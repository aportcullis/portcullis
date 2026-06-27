package config_test

import (
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
