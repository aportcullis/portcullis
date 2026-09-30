// Package pgdialect is the PostgreSQL dialect adapter (ADR-0001, PRD §5.3): single-statement parsing and allow-list classification (ADR-0002), named parameter binding and token-rebuild redaction (ADR-0016), connection validation (ADR-0014), and governed execution. Raw parser and driver errors never leave this package (PRD §8.1).
package pgdialect

import (
	"time"
)

// Options configures the dialect adapter.
type Options struct {
	// ValidateTimeout bounds one connection validation round-trip; zero picks the ADR-0014 default (the real default and bounds live in platform/config).
	ValidateTimeout time.Duration
}

// Dialect implements the PostgreSQL side of the QueryDialect contract. Consumers declare their own narrow ports; *Dialect satisfies them structurally.
type Dialect struct {
	validateTimeout time.Duration
}

// New builds the dialect adapter.
func New(opts Options) *Dialect {
	timeout := opts.ValidateTimeout
	if timeout <= 0 {
		timeout = defaultTestTimeout
	}
	return &Dialect{validateTimeout: timeout}
}
