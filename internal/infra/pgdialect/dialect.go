// Package pgdialect is the PostgreSQL dialect adapter (ADR-0001, PRD §5.3): single-statement parsing and allow-list classification (ADR-0002), named parameter binding and token-rebuild redaction (ADR-0016), connection validation (ADR-0014), and governed execution. Raw parser and driver errors never leave this package (PRD §8.1).
package pgdialect

import (
	"context"
	"net"
	"net/netip"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/setting"
)

// DestinationResolver resolves a target host to the addresses the destination policy checks and the dialer then uses; *net.Resolver satisfies it.
type DestinationResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Options configures the dialect adapter.
type Options struct {
	// ValidateTimeout bounds one connection validation round-trip; zero picks the ADR-0014 default (the real default and bounds live in platform/config).
	ValidateTimeout time.Duration
	// LockTimeout bounds each lock wait of a governed execution; zero picks the ADR-0021 default from the settings registry.
	LockTimeout time.Duration
	// Destinations decides which resolved addresses connection tests and executions may dial (ADR-0051); the zero value is the default policy.
	Destinations connection.DestinationPolicy
	// Resolver resolves target hosts once per dial; nil uses net.DefaultResolver.
	Resolver DestinationResolver
}

// Dialect implements the PostgreSQL side of the QueryDialect contract. Consumers declare their own narrow ports; *Dialect satisfies them structurally.
type Dialect struct {
	validateTimeout time.Duration
	lockTimeout     time.Duration
	destinations    connection.DestinationPolicy
	resolver        DestinationResolver
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
	var resolver DestinationResolver = net.DefaultResolver
	if opts.Resolver != nil {
		resolver = opts.Resolver
	}
	return &Dialect{validateTimeout: timeout, lockTimeout: lockTimeout, destinations: opts.Destinations, resolver: resolver}
}

// defaultExecutionLockTimeout reads the compiled lock-wait default from the settings registry so the adapter and configuration share one source.
func defaultExecutionLockTimeout() time.Duration {
	descriptor, _ := setting.Lookup(setting.KeyExecutionLockTimeout)
	return descriptor.DefaultDuration()
}
