package config_test

import (
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/platform/config"
)

func TestDatabasePoolDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseMaxConns != 16 || cfg.DatabaseAcquireTimeout != 10*time.Second ||
		cfg.DatabaseStatementTimeout != 30*time.Second || cfg.DatabaseLockTimeout != 10*time.Second ||
		cfg.DatabaseIdleInTransactionTimeout != time.Minute {
		t.Errorf("pool defaults = max %d, acquire %s, statement %s, lock %s, idle-in-tx %s; want 16, 10s, 30s, 10s, 1m",
			cfg.DatabaseMaxConns, cfg.DatabaseAcquireTimeout, cfg.DatabaseStatementTimeout, cfg.DatabaseLockTimeout, cfg.DatabaseIdleInTransactionTimeout)
	}
}

func TestDatabasePoolAcceptsBoundaryValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		read func(config.Config) bool
	}{
		{"smallest pool and shortest bounds", map[string]string{
			"PORTCULLIS_DATABASE_MAX_CONNS": "2", "PORTCULLIS_DATABASE_ACQUIRE_TIMEOUT": "100ms",
			"PORTCULLIS_DATABASE_STATEMENT_TIMEOUT": "1s", "PORTCULLIS_DATABASE_LOCK_TIMEOUT": "100ms",
			"PORTCULLIS_DATABASE_IDLE_IN_TRANSACTION_TIMEOUT": "1s",
		}, func(cfg config.Config) bool {
			return cfg.DatabaseMaxConns == 2 && cfg.DatabaseAcquireTimeout == 100*time.Millisecond &&
				cfg.DatabaseStatementTimeout == time.Second && cfg.DatabaseLockTimeout == 100*time.Millisecond &&
				cfg.DatabaseIdleInTransactionTimeout == time.Second
		}},
		{"largest pool and longest bounds", map[string]string{
			"PORTCULLIS_DATABASE_MAX_CONNS": "200", "PORTCULLIS_DATABASE_ACQUIRE_TIMEOUT": "1m",
			"PORTCULLIS_DATABASE_STATEMENT_TIMEOUT": "10m", "PORTCULLIS_DATABASE_LOCK_TIMEOUT": "5m",
			"PORTCULLIS_DATABASE_IDLE_IN_TRANSACTION_TIMEOUT": "1h",
		}, func(cfg config.Config) bool {
			return cfg.DatabaseMaxConns == 200 && cfg.DatabaseAcquireTimeout == time.Minute &&
				cfg.DatabaseStatementTimeout == 10*time.Minute && cfg.DatabaseLockTimeout == 5*time.Minute &&
				cfg.DatabaseIdleInTransactionTimeout == time.Hour
		}},
		{"a mid-range pool", map[string]string{"PORTCULLIS_DATABASE_MAX_CONNS": "40"}, func(cfg config.Config) bool {
			return cfg.DatabaseMaxConns == 40
		}},
		{"a lock bound equal to the statement bound", map[string]string{
			"PORTCULLIS_DATABASE_STATEMENT_TIMEOUT": "20s", "PORTCULLIS_DATABASE_LOCK_TIMEOUT": "20s",
		}, func(cfg config.Config) bool {
			return cfg.DatabaseLockTimeout == 20*time.Second && cfg.DatabaseStatementTimeout == 20*time.Second
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !tc.read(cfg) {
				t.Errorf("pool settings = %+v", cfg)
			}
		})
	}
}

func TestDatabasePoolRejectsOutOfRangeValues(t *testing.T) {
	for _, tc := range []struct{ name, env, value string }{
		{"max conns zero", "PORTCULLIS_DATABASE_MAX_CONNS", "0"},
		{"max conns one", "PORTCULLIS_DATABASE_MAX_CONNS", "1"},
		{"max conns absurd", "PORTCULLIS_DATABASE_MAX_CONNS", "100000"},
		{"acquire timeout zero", "PORTCULLIS_DATABASE_ACQUIRE_TIMEOUT", "0s"},
		{"acquire timeout absurd", "PORTCULLIS_DATABASE_ACQUIRE_TIMEOUT", "2m"},
		{"statement timeout negative", "PORTCULLIS_DATABASE_STATEMENT_TIMEOUT", "-1s"},
		{"statement timeout absurd", "PORTCULLIS_DATABASE_STATEMENT_TIMEOUT", "11m"},
		{"lock timeout below floor", "PORTCULLIS_DATABASE_LOCK_TIMEOUT", "10ms"},
		{"lock timeout above the statement bound", "PORTCULLIS_DATABASE_LOCK_TIMEOUT", "45s"},
		{"idle in transaction zero", "PORTCULLIS_DATABASE_IDLE_IN_TRANSACTION_TIMEOUT", "0s"},
		{"idle in transaction absurd", "PORTCULLIS_DATABASE_IDLE_IN_TRANSACTION_TIMEOUT", "2h"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.env, tc.value)
			if _, err := config.Load(); err == nil {
				t.Errorf("%s=%q should be rejected", tc.env, tc.value)
			}
		})
	}
}
