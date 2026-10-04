package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func boundedPoolSettings() pg.PoolSettings {
	return pg.PoolSettings{
		MaxConns:                 3,
		AcquireTimeout:           300 * time.Millisecond,
		StatementTimeout:         2 * time.Second,
		LockTimeout:              250 * time.Millisecond,
		IdleInTransactionTimeout: time.Second,
	}
}

func openBoundedPool(t *testing.T, base *pgxpool.Pool, settings pg.PoolSettings) *pgxpool.Pool {
	t.Helper()
	pool, err := pg.Open(context.Background(), base.Config().ConnString(), settings)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestOpenAppliesPoolAndSessionBounds(t *testing.T) {
	base := dbtest.FreshPostgres(t)
	ctx := context.Background()
	pool := openBoundedPool(t, base, boundedPoolSettings())

	t.Run("pool size and session settings", func(t *testing.T) {
		if got := pool.Config().MaxConns; got != 3 {
			t.Errorf("MaxConns = %d, want 3", got)
		}
		var statementTimeout, lockTimeout, idleTimeout string
		if err := pool.QueryRow(ctx, `select current_setting('statement_timeout'), current_setting('lock_timeout'), current_setting('idle_in_transaction_session_timeout')`).Scan(&statementTimeout, &lockTimeout, &idleTimeout); err != nil {
			t.Fatal(err)
		}
		if statementTimeout != "2s" || lockTimeout != "250ms" || idleTimeout != "1s" {
			t.Errorf("session bounds = %s / %s / %s, want 2s / 250ms / 1s", statementTimeout, lockTimeout, idleTimeout)
		}
	})

	t.Run("a statement past the bound is cancelled", func(t *testing.T) {
		_, err := pool.Exec(ctx, `select pg_sleep(5)`)
		requirePoolSQLState(t, err, "57014", "a 5s statement")
	})

	t.Run("a lock wait past the bound fails", func(t *testing.T) {
		if _, err := base.Exec(ctx, `create table if not exists public.pool_lock_target (id int)`); err != nil {
			t.Fatal(err)
		}
		holder, err := base.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = holder.Rollback(ctx) }()
		if _, err := holder.Exec(ctx, `lock table public.pool_lock_target in access exclusive mode`); err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `select count(*) from public.pool_lock_target`)
		requirePoolSQLState(t, err, "55P03", "a blocked read")
	})

	t.Run("a session idle inside a transaction is terminated", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		time.Sleep(1500 * time.Millisecond)
		if _, err := tx.Exec(ctx, `select 1`); err == nil {
			t.Error("a transaction idle past the bound still ran a statement")
		}
	})

	t.Run("acquisition beyond the pool waits only the acquire bound", func(t *testing.T) {
		held := make([]*pgxpool.Conn, 0, 3)
		for range 3 {
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("Acquire within MaxConns: %v", err)
			}
			held = append(held, conn)
		}
		started := time.Now()
		_, err := pool.Acquire(ctx)
		waited := time.Since(started)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Acquire on an exhausted pool = %v, want context.DeadlineExceeded", err)
		}
		if waited > 2*time.Second {
			t.Errorf("exhausted acquire waited %s, want about 300ms", waited)
		}
		for _, conn := range held {
			conn.Release()
		}
		if err := pool.Ping(ctx); err != nil {
			t.Errorf("Ping after releasing = %v, want the pool usable again", err)
		}
	})
}

func TestOpenRefusesUnboundedSettings(t *testing.T) {
	base := dbtest.FreshPostgres(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		mutate func(*pg.PoolSettings)
	}{
		{"zero max conns", func(settings *pg.PoolSettings) { settings.MaxConns = 0 }},
		{"zero acquire timeout", func(settings *pg.PoolSettings) { settings.AcquireTimeout = 0 }},
		{"zero statement timeout", func(settings *pg.PoolSettings) { settings.StatementTimeout = 0 }},
		{"negative lock timeout", func(settings *pg.PoolSettings) { settings.LockTimeout = -time.Second }},
		{"zero idle-in-transaction timeout", func(settings *pg.PoolSettings) { settings.IdleInTransactionTimeout = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := boundedPoolSettings()
			tc.mutate(&settings)
			if pool, err := pg.Open(ctx, base.Config().ConnString(), settings); err == nil {
				pool.Close()
				t.Errorf("Open with %s succeeded", tc.name)
			}
		})
	}
	if _, err := pg.Open(ctx, "postgres://%zz", boundedPoolSettings()); err == nil {
		t.Error("Open with a malformed DSN succeeded")
	}
}

func requirePoolSQLState(t *testing.T, err error, code, attempt string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Errorf("%s = %v, want SQLSTATE %s", attempt, err, code)
	}
}
