// Package pgdialect is the PostgreSQL dialect adapter (ADR-0001, PRD §5.3): single-statement parsing and allow-list classification (ADR-0002), named parameter binding and token-rebuild redaction (ADR-0016), connection validation (ADR-0014), and governed execution. Raw parser and driver errors never leave this package (PRD §8.1).
package pgdialect

import (
	"time"

	"github.com/aportcullis/portcullis/internal/domain/setting"
)

// Options configures the dialect adapter.
type Options struct {
	// ValidateTimeout bounds one connection validation round-trip; zero picks the ADR-0014 default (the real default and bounds live in platform/config).
	ValidateTimeout time.Duration
	// LockTimeout bounds each lock wait of a governed execution; zero picks the ADR-0021 default from the settings registry.
	LockTimeout time.Duration
}

// Dialect implements the PostgreSQL side of the QueryDialect contract. Consumers declare their own narrow ports; *Dialect satisfies them structurally.
type Dialect struct {
	validateTimeout time.Duration
	lockTimeout     time.Duration
}

// New builds the dialect adapter.
func New(opts Options) *Dialect {
	timeout := opts.ValidateTimeout
	if timeout <= 0 {
		timeout = defaultTestTimeout
	}
	lockTimeout := opts.LockTimeout
	if lockTimeout <= 0 {
		lockTimeout = defaultExecutionLockTimeout()
	}
	return &Dialect{validateTimeout: timeout, lockTimeout: lockTimeout}
}

// defaultExecutionLockTimeout reads the compiled lock-wait default from the settings registry so the adapter and configuration share one source.
func defaultExecutionLockTimeout() time.Duration {
	descriptor, _ := setting.Lookup(setting.KeyExecutionLockTimeout)
	return descriptor.DefaultDuration()
}
