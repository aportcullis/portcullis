// Package dbtest provides shared, container-backed databases for integration
// tests. Each engine starts once per test binary and is reused; the
// testcontainers reaper tears the containers down when the process exits.
//
// It is the harness for the cross-engine contract suite: PostgreSQL and MySQL
// run in containers, SQLite is file-based (added with the dialect adapters).
package dbtest

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/aportcullis/portcullis/internal/platform/logging"
)

var (
	pgOnce  sync.Once
	pgPool  *pgxpool.Pool
	pgDSN   string
	pgErr   error
	freshDB atomic.Int64
)

// Postgres returns a connection pool to a shared test Postgres. It uses
// PORTCULLIS_TEST_DATABASE_URL when set, otherwise a throwaway container. When
// neither Docker nor an external DB is available, the calling test is skipped.
func Postgres(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pgOnce.Do(startPostgres)
	if pgErr != nil {
		t.Skipf("postgres unavailable: %v", pgErr)
	}
	return pgPool
}

func startPostgres() {
	ctx := context.Background()

	dsn := os.Getenv("PORTCULLIS_TEST_DATABASE_URL")
	if dsn == "" {
		// Route testcontainers output through our standardized slog stream.
		tcLogger := logging.NewPrintfLogger(logging.New("debug", "json"), slog.LevelDebug, "testcontainers")
		// tag@digest pin (supply chain); same digest as compose.yaml and
		// web/e2e/server.sh — Renovate's regex manager bumps them together.
		container, err := tcpostgres.Run(ctx, "postgres:18.4-alpine3.24@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15",
			tcpostgres.WithDatabase("portcullis"),
			tcpostgres.WithUsername("portcullis"),
			tcpostgres.WithPassword("portcullis"),
			testcontainers.WithLogger(tcLogger),
			// Port-only readiness races both the postgres entrypoint (it starts a
			// temporary server during initdb, then restarts) and docker-proxy
			// (which listens before the container-side process does), yielding
			// "connection reset by peer" on slow CI. The module's canonical
			// strategy waits for the readiness log line twice, then the port.
			tcpostgres.BasicWaitStrategies(),
		)
		if err != nil {
			pgErr = err
			return
		}
		if dsn, err = container.ConnectionString(ctx, "sslmode=disable"); err != nil {
			pgErr = err
			return
		}
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		pgErr = err
		return
	}
	pgPool = pool
	pgDSN = dsn
}

// FreshPostgres creates a brand-new, empty database in the shared container and
// returns a pool to it (migrations NOT applied — the caller migrates). Use it
// for tests that need global isolation, e.g. first-run bootstrap which asserts
// on the whole users table. The database is dropped at test end.
func FreshPostgres(t testing.TB) *pgxpool.Pool {
	t.Helper()
	admin := Postgres(t) // ensures the container/DSN are up
	ctx := context.Background()

	name := fmt.Sprintf("pc_fresh_%d", freshDB.Add(1))
	if _, err := admin.Exec(ctx, "create database "+name); err != nil {
		// An external test DB (PORTCULLIS_TEST_DATABASE_URL) may connect as a
		// non-superuser without CREATEDB; skip rather than fail there.
		t.Skipf("FreshPostgres needs CREATEDB privilege: %v", err)
	}

	u, err := url.Parse(pgDSN)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatalf("connect fresh db: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		// DROP needs no active connections to the target database.
		_, _ = admin.Exec(context.Background(), "drop database if exists "+name+" with (force)")
	})
	return pool
}
