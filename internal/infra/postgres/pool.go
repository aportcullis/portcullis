// Package postgres is the metadata-store infrastructure: connection pool and schema migrations. It implements (or backs) the domain repository ports.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolSettings bounds the metadata pool size, connection acquisition and every session's statement, lock and idle-in-transaction time (ADR-0010).
type PoolSettings struct {
	MaxConns                 int32
	AcquireTimeout           time.Duration
	StatementTimeout         time.Duration
	LockTimeout              time.Duration
	IdleInTransactionTimeout time.Duration
}

// validate refuses settings that would leave the pool or a session unbounded.
func (p PoolSettings) validate() error {
	if p.MaxConns < 1 || p.AcquireTimeout <= 0 || p.StatementTimeout <= 0 || p.LockTimeout <= 0 || p.IdleInTransactionTimeout <= 0 {
		return errors.New("metadata pool settings must all be positive")
	}
	return nil
}

// Open creates a bounded connection pool to the metadata database.
func Open(ctx context.Context, dsn string, settings PoolSettings) (*pgxpool.Pool, error) {
	if err := settings.validate(); err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = settings.MaxConns
	cfg.MinConns = min(cfg.MinConns, cfg.MaxConns)
	// Startup parameters apply to every session the pool opens, before any application statement.
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = postgresMilliseconds(settings.StatementTimeout)
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = postgresMilliseconds(settings.LockTimeout)
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = postgresMilliseconds(settings.IdleInTransactionTimeout)
	if cfg.ConnConfig.ConnectTimeout == 0 || cfg.ConnConfig.ConnectTimeout > settings.AcquireTimeout {
		cfg.ConnConfig.ConnectTimeout = settings.AcquireTimeout
	}
	if cfg.ConnConfig.Tracer != nil {
		return nil, fmt.Errorf("metadata pool DSN must not configure a tracer")
	}
	cfg.ConnConfig.Tracer = acquireDeadline{timeout: settings.AcquireTimeout}
	return pgxpool.NewWithConfig(ctx, cfg)
}

// acquireDeadline bounds each pool acquisition: pgxpool acquires with the context TraceAcquireStart returns.
type acquireDeadline struct{ timeout time.Duration }

// acquireCancelKey carries the acquisition deadline's cancel function to TraceAcquireEnd.
type acquireCancelKey struct{}

// TraceAcquireStart derives the bounded acquisition context.
func (a acquireDeadline) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	bounded, cancel := context.WithTimeout(ctx, a.timeout)
	return context.WithValue(bounded, acquireCancelKey{}, cancel)
}

// TraceAcquireEnd releases the acquisition deadline.
func (acquireDeadline) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireEndData) {
	if cancel, ok := ctx.Value(acquireCancelKey{}).(context.CancelFunc); ok {
		cancel()
	}
}

// TraceQueryStart leaves queries untraced; the tracer exists for acquisition only.
func (acquireDeadline) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

// TraceQueryEnd leaves queries untraced.
func (acquireDeadline) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
